package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/LooLookProject/foodbox/backend/internal/clova"
	"github.com/LooLookProject/foodbox/backend/internal/config"
	"github.com/LooLookProject/foodbox/backend/internal/crawler"
	"github.com/LooLookProject/foodbox/backend/internal/domain"
	"github.com/LooLookProject/foodbox/backend/internal/httpapi"
	"github.com/LooLookProject/foodbox/backend/internal/ocr"
	"github.com/LooLookProject/foodbox/backend/internal/scheduler"
	"github.com/LooLookProject/foodbox/backend/internal/service"
	"github.com/LooLookProject/foodbox/backend/internal/slack"
	"github.com/LooLookProject/foodbox/backend/internal/store"
)

const shutdownTimeout = 10 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("foodbox stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	menus, notifications, err := buildServices(cfg)
	if err != nil {
		return err
	}
	handler, err := httpapi.NewHandler(httpapi.Config{
		AdminToken: cfg.AdminToken.Value(),
		StaticDir:  cfg.StaticDir,
		Location:   cfg.Location,
		Logger:     logger,
	}, menus, notifications)
	if err != nil {
		return fmt.Errorf("build HTTP handler: %w", err)
	}

	runner, err := scheduler.NewRunner(scheduler.Options{
		Location: cfg.Location,
		Startup:  menus.RefreshIfStale,
		Daily:    notifications.NotifyToday,
		OnError: func(err error) {
			logger.Error("scheduled job failed", "error", err)
		},
	})
	if err != nil {
		return fmt.Errorf("build scheduler: %w", err)
	}

	listener, err := net.Listen("tcp", ":"+strconv.Itoa(cfg.Port))
	if err != nil {
		return fmt.Errorf("listen on port %d: %w", cfg.Port, err)
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ready := make(chan struct{})
	schedulerDone := make(chan error, 1)
	go func() {
		schedulerDone <- runner.Run(ctx, ready)
	}()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Serve(listener)
	}()
	close(ready)
	logger.Info("foodbox started", "port", cfg.Port)

	var resultErr error
	schedulerStopped := false
	select {
	case <-ctx.Done():
		logger.Info("foodbox shutting down")
	case err := <-serverDone:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			resultErr = fmt.Errorf("serve HTTP: %w", err)
		}
	case err := <-schedulerDone:
		schedulerStopped = true
		if err != nil {
			resultErr = fmt.Errorf("run scheduler: %w", err)
		} else if ctx.Err() == nil {
			resultErr = errors.New("scheduler stopped unexpectedly")
		}
	}
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("shut down HTTP server: %w", err))
		if closeErr := server.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close HTTP server: %w", closeErr))
		}
	}
	if !schedulerStopped {
		select {
		case err := <-schedulerDone:
			if err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("stop scheduler: %w", err))
			}
		case <-shutdownCtx.Done():
			resultErr = errors.Join(resultErr, errors.New("scheduler shutdown timed out"))
		}
	}
	return resultErr
}

func buildServices(cfg *config.Config) (*service.MenuService, *service.NotificationService, error) {
	repository, err := store.NewFileStore(cfg.DBFileDir)
	if err != nil {
		return nil, nil, fmt.Errorf("open menu store: %w", err)
	}
	hashes, err := store.NewHashStore(cfg.DBFileDir)
	if err != nil {
		return nil, nil, fmt.Errorf("open image hash store: %w", err)
	}
	crawlClient, err := crawler.NewClient(cfg.CrawlURL)
	if err != nil {
		return nil, nil, fmt.Errorf("build crawler client: %w", err)
	}
	clovaClient, err := clova.NewClient(cfg.ClovaURL, cfg.ClovaSecret.Value())
	if err != nil {
		return nil, nil, fmt.Errorf("build Clova client: %w", err)
	}
	slackClient, err := slack.NewClient(cfg.SlackURL, cfg.SlackToken.Value())
	if err != nil {
		return nil, nil, fmt.Errorf("build Slack client: %w", err)
	}

	menus := service.NewMenuService(
		repository,
		crawlClient,
		clovaAdapter{client: clovaClient},
		service.ParserFunc(ocr.Parse),
		hashes,
		service.WithDateClock(func() domain.LocalDate {
			return domain.FromTime(time.Now().In(cfg.Location))
		}),
	)

	notifications := service.NewNotificationService(
		menus,
		slackAdapter{client: slackClient},
		service.NotificationConfig{
			Channel:   cfg.SlackChannel,
			Username:  cfg.SlackUsername,
			IconEmoji: ":bento:",
		},
		service.WithNotificationDateClock(func() domain.LocalDate {
			return domain.FromTime(time.Now().In(cfg.Location))
		}),
	)
	return menus, notifications, nil
}
