package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
	"github.com/LooLookProject/foodbox/backend/internal/httpapi"
	"github.com/LooLookProject/foodbox/backend/internal/service"
)

type unwritableRepository struct{}

func (unwritableRepository) FindAll(context.Context) ([]domain.Menu, error) {
	return []domain.Menu{}, nil
}

func (unwritableRepository) FindByDate(context.Context, domain.LocalDate) (domain.Menu, bool, error) {
	return domain.Menu{}, false, nil
}

func (unwritableRepository) SaveAll(context.Context, []domain.Menu) error {
	return nil
}

func (unwritableRepository) CheckWritable(context.Context) error {
	return errors.New("data directory is read-only")
}

func TestHealthReturnsUnavailableWhenRepositoryIsNotWritable(t *testing.T) {
	menus := service.NewMenuService(unwritableRepository{}, nil, nil, nil, nil)
	notifications := service.NewNotificationService(nil, nil, service.NotificationConfig{})
	handler, err := httpapi.NewHandler(httpapi.Config{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, menus, notifications)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"errorCode":"NOT_READY"`) {
		t.Fatalf("response does not contain NOT_READY: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "read-only") {
		t.Fatalf("response exposed internal storage error: %s", response.Body.String())
	}
}
