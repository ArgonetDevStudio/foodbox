package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
)

type MenuProvider interface {
	Today(ctx context.Context, date domain.LocalDate) (domain.Menu, error)
}

type SlackSender interface {
	Send(ctx context.Context, message SlackMessage) error
}

type SlackSenderFunc func(context.Context, SlackMessage) error

func (f SlackSenderFunc) Send(ctx context.Context, message SlackMessage) error {
	return f(ctx, message)
}

type SlackMessage struct {
	Channel   string
	Username  string
	Text      string
	IconEmoji string
}

type NotificationConfig struct {
	Channel   string
	Username  string
	IconEmoji string
}

type NotifyOptions struct {
	DryRun bool
}

type NotificationStatus string

const (
	NotificationSent           NotificationStatus = "sent"
	NotificationDryRun         NotificationStatus = "dry_run"
	NotificationSkippedWeekend NotificationStatus = "skipped_weekend"
	NotificationSkippedInvalid NotificationStatus = "skipped_invalid"
)

type NotificationResult struct {
	Status  NotificationStatus
	Message SlackMessage
}

type NotificationService struct {
	menus  MenuProvider
	sender SlackSender
	config NotificationConfig
	now    func() domain.LocalDate
}

type NotificationOption func(*NotificationService)

func WithNotificationDateClock(now func() domain.LocalDate) NotificationOption {
	return func(service *NotificationService) {
		if now != nil {
			service.now = now
		}
	}
}

func NewNotificationService(
	menus MenuProvider,
	sender SlackSender,
	config NotificationConfig,
	options ...NotificationOption,
) *NotificationService {
	if config.IconEmoji == "" {
		config.IconEmoji = ":bento:"
	}
	service := &NotificationService{
		menus:  menus,
		sender: sender,
		config: config,
		now:    todayInSeoul,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *NotificationService) NotifyToday(ctx context.Context) error {
	_, err := s.Notify(ctx, NotifyOptions{})
	return err
}

func (s *NotificationService) Notify(ctx context.Context, options NotifyOptions) (NotificationResult, error) {
	today := s.now()
	if isWeekend(today) {
		return NotificationResult{Status: NotificationSkippedWeekend}, nil
	}
	if s.menus == nil {
		return NotificationResult{}, fmt.Errorf("menu provider: %w", ErrNotConfigured)
	}

	menu, err := s.menus.Today(ctx, today)
	if err != nil {
		return NotificationResult{}, fmt.Errorf("get today's menu: %w", err)
	}
	if !menu.Valid {
		return NotificationResult{Status: NotificationSkippedInvalid}, nil
	}

	messageDate := menu.Date
	menuItems := menu.Menus
	if asTime(today).Weekday() == time.Wednesday {
		messageDate = today
		if isLastWednesday(today) {
			menuItems = []string{"외식 🍽"}
		} else {
			menuItems = []string{"데니스델리 🥗"}
		}
	}

	message := SlackMessage{
		Channel:   s.config.Channel,
		Username:  s.config.Username,
		Text:      createSlackText(messageDate, menuItems),
		IconEmoji: s.config.IconEmoji,
	}
	if options.DryRun {
		return NotificationResult{Status: NotificationDryRun, Message: message}, nil
	}
	if s.sender == nil {
		return NotificationResult{}, fmt.Errorf("slack sender: %w", ErrNotConfigured)
	}
	if err := s.sender.Send(ctx, message); err != nil {
		return NotificationResult{}, fmt.Errorf("send Slack message: %w", err)
	}
	return NotificationResult{Status: NotificationSent, Message: message}, nil
}

func createSlackText(date domain.LocalDate, menus []string) string {
	bullets := make([]string, len(menus))
	for i, menu := range menus {
		bullets[i] = "• " + menu
	}
	return fmt.Sprintf("<%s %s> 점심 메뉴\n%s", formatDate(date), koreanWeekday(date), strings.Join(bullets, "\n"))
}
