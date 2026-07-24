package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
	slackapi "github.com/LooLookProject/foodbox/backend/internal/slack"
)

type legacyMenuProviderFunc func(context.Context, domain.LocalDate) (domain.Menu, error)

func (f legacyMenuProviderFunc) Today(ctx context.Context, date domain.LocalDate) (domain.Menu, error) {
	return f(ctx, date)
}

type legacyRecordingSender struct {
	messages []SlackMessage
}

func (sender *legacyRecordingSender) Send(_ context.Context, message SlackMessage) error {
	sender.messages = append(sender.messages, message)
	return nil
}

type legacySlackClientAdapter struct {
	client *slackapi.Client
}

func (adapter legacySlackClientAdapter) Send(ctx context.Context, message SlackMessage) error {
	return adapter.client.Send(ctx, slackapi.Message{
		Channel:   message.Channel,
		Username:  message.Username,
		Text:      message.Text,
		IconEmoji: message.IconEmoji,
	})
}

func legacyDate(year, month, day int) domain.LocalDate {
	return domain.LocalDate{Year: year, Month: month, Day: day}
}

func assertLegacyBytes(t *testing.T, got, want string) {
	t.Helper()
	if !bytes.Equal([]byte(got), []byte(want)) {
		t.Fatalf("bytes = %q, want %q", []byte(got), []byte(want))
	}
}

func TestLegacySlackContractKoreanWeekdaysByteForByte(t *testing.T) {
	tests := []struct {
		name string
		date domain.LocalDate
		want string
	}{
		{name: "Sunday", date: legacyDate(2025, 3, 30), want: "<2025-03-30 일요일> 점심 메뉴\n• 메뉴"},
		{name: "Monday", date: legacyDate(2025, 3, 31), want: "<2025-03-31 월요일> 점심 메뉴\n• 메뉴"},
		{name: "Tuesday", date: legacyDate(2025, 4, 1), want: "<2025-04-01 화요일> 점심 메뉴\n• 메뉴"},
		{name: "Wednesday", date: legacyDate(2025, 4, 2), want: "<2025-04-02 수요일> 점심 메뉴\n• 메뉴"},
		{name: "Thursday", date: legacyDate(2025, 4, 3), want: "<2025-04-03 목요일> 점심 메뉴\n• 메뉴"},
		{name: "Friday", date: legacyDate(2025, 4, 4), want: "<2025-04-04 금요일> 점심 메뉴\n• 메뉴"},
		{name: "Saturday", date: legacyDate(2025, 4, 5), want: "<2025-04-05 토요일> 점심 메뉴\n• 메뉴"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertLegacyBytes(t, createSlackText(test.date, []string{"메뉴"}), test.want)
		})
	}
}

func TestLegacySlackContractRegularWeekdayNotifications(t *testing.T) {
	tests := []struct {
		name string
		date domain.LocalDate
		want string
	}{
		{name: "Monday", date: legacyDate(2025, 3, 24), want: "<2025-03-24 월요일> 점심 메뉴\n• 김치찌개\n• 된장찌개\n• 제육볶음"},
		{name: "Tuesday", date: legacyDate(2025, 3, 25), want: "<2025-03-25 화요일> 점심 메뉴\n• 김치찌개\n• 된장찌개\n• 제육볶음"},
		{name: "Thursday", date: legacyDate(2025, 3, 27), want: "<2025-03-27 목요일> 점심 메뉴\n• 김치찌개\n• 된장찌개\n• 제육볶음"},
		{name: "Friday", date: legacyDate(2025, 3, 28), want: "<2025-03-28 금요일> 점심 메뉴\n• 김치찌개\n• 된장찌개\n• 제육볶음"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			providerCalls := 0
			provider := legacyMenuProviderFunc(func(_ context.Context, got domain.LocalDate) (domain.Menu, error) {
				providerCalls++
				if got != test.date {
					t.Fatalf("menu date argument = %s, want %s", got, test.date)
				}
				return domain.Menu{Date: test.date, Menus: []string{"김치찌개", "된장찌개", "제육볶음"}, Valid: true}, nil
			})
			sender := &legacyRecordingSender{}
			notifier := NewNotificationService(provider, sender, NotificationConfig{
				Channel:  "#foodbox",
				Username: "점심봇",
			}, WithNotificationDateClock(func() domain.LocalDate { return test.date }))

			result, err := notifier.Notify(context.Background(), NotifyOptions{})
			if err != nil {
				t.Fatal(err)
			}
			wantMessage := SlackMessage{Channel: "#foodbox", Username: "점심봇", Text: test.want, IconEmoji: ":bento:"}
			if providerCalls != 1 || len(sender.messages) != 1 {
				t.Fatalf("provider calls = %d, Slack calls = %d", providerCalls, len(sender.messages))
			}
			if result.Status != NotificationSent || result.Message != wantMessage || sender.messages[0] != wantMessage {
				t.Fatalf("result = %+v, sent = %+v, want = %+v", result, sender.messages[0], wantMessage)
			}
			assertLegacyBytes(t, sender.messages[0].Text, test.want)
		})
	}
}

func TestLegacySlackContractEveryWednesdayAndMonthEndBoundary(t *testing.T) {
	for year := 2024; year <= 2026; year++ {
		for month := 1; month <= 12; month++ {
			lastDay := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, seoul).Day()
			for day := 1; day <= lastDay; day++ {
				date := legacyDate(year, month, day)
				if asTime(date).Weekday() != time.Wednesday {
					continue
				}
				remaining := lastDay - day
				t.Run(formatDate(date), func(t *testing.T) {
					provider := legacyMenuProviderFunc(func(_ context.Context, got domain.LocalDate) (domain.Menu, error) {
						if got != date {
							t.Fatalf("menu date argument = %s, want %s", got, date)
						}
						return domain.Menu{
							Date:  legacyDate(1999, 1, 1),
							Menus: []string{"legacy one", "legacy two", "legacy three"},
							Valid: true,
						}, nil
					})
					sender := &legacyRecordingSender{}
					notifier := NewNotificationService(provider, sender, NotificationConfig{}, WithNotificationDateClock(func() domain.LocalDate { return date }))

					if err := notifier.NotifyToday(context.Background()); err != nil {
						t.Fatal(err)
					}
					if len(sender.messages) != 1 {
						t.Fatalf("Slack calls = %d, want 1", len(sender.messages))
					}
					menu := "데니스델리 🥗"
					if remaining < 7 {
						menu = "외식 🍽"
					}
					want := fmt.Sprintf("<%s 수요일> 점심 메뉴\n• %s", formatDate(date), menu)
					assertLegacyBytes(t, sender.messages[0].Text, want)
				})
			}
		}
	}
}

func TestLegacySlackContractInvalidWednesdayAndWeekendsMakeNoCalls(t *testing.T) {
	tests := []struct {
		name              string
		date              domain.LocalDate
		wantStatus        NotificationStatus
		wantProviderCalls int
	}{
		{name: "invalid non-last Wednesday", date: legacyDate(2025, 5, 7), wantStatus: NotificationSkippedInvalid, wantProviderCalls: 1},
		{name: "invalid last Wednesday", date: legacyDate(2025, 4, 30), wantStatus: NotificationSkippedInvalid, wantProviderCalls: 1},
		{name: "Saturday", date: legacyDate(2025, 3, 29), wantStatus: NotificationSkippedWeekend, wantProviderCalls: 0},
		{name: "Sunday", date: legacyDate(2025, 3, 30), wantStatus: NotificationSkippedWeekend, wantProviderCalls: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			providerCalls := 0
			provider := legacyMenuProviderFunc(func(context.Context, domain.LocalDate) (domain.Menu, error) {
				providerCalls++
				return domain.Menu{Date: test.date, Menus: []string{"one", "two", "three"}, Valid: false}, nil
			})
			sender := &legacyRecordingSender{}
			notifier := NewNotificationService(provider, sender, NotificationConfig{}, WithNotificationDateClock(func() domain.LocalDate { return test.date }))

			result, err := notifier.Notify(context.Background(), NotifyOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != test.wantStatus || providerCalls != test.wantProviderCalls || len(sender.messages) != 0 {
				t.Fatalf("result = %+v, provider calls = %d, Slack calls = %d", result, providerCalls, len(sender.messages))
			}
		})
	}
}

func TestLegacySlackContractTrustsStoredValidFlag(t *testing.T) {
	tests := []struct {
		name       string
		menus      []string
		valid      bool
		wantStatus NotificationStatus
		wantCalls  int
		wantText   string
	}{
		{
			name: "stored true sends even with one line", menus: []string{"oneMenu"}, valid: true,
			wantStatus: NotificationSent, wantCalls: 1,
			wantText: "<2025-03-31 월요일> 점심 메뉴\n• oneMenu",
		},
		{
			name: "stored false skips even with three lines", menus: []string{"oneMenu", "twoMenu", "threeMenu"}, valid: false,
			wantStatus: NotificationSkippedInvalid, wantCalls: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			date := legacyDate(2025, 3, 31)
			provider := legacyMenuProviderFunc(func(context.Context, domain.LocalDate) (domain.Menu, error) {
				return domain.Menu{Date: date, Menus: test.menus, Valid: test.valid}, nil
			})
			sender := &legacyRecordingSender{}
			notifier := NewNotificationService(provider, sender, NotificationConfig{}, WithNotificationDateClock(func() domain.LocalDate { return date }))

			result, err := notifier.Notify(context.Background(), NotifyOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != test.wantStatus || len(sender.messages) != test.wantCalls {
				t.Fatalf("result = %+v, Slack calls = %d", result, len(sender.messages))
			}
			if test.wantCalls == 1 {
				assertLegacyBytes(t, sender.messages[0].Text, test.wantText)
			}
		})
	}
}

func TestLegacySlackContractExternalMockReceivesExactJSON(t *testing.T) {
	tests := []struct {
		name    string
		channel string
	}{
		{name: "channel without hash", channel: "foodbox"},
		{name: "channel with hash", channel: "#foodbox"},
		{name: "channel with spaces", channel: " foodbox "},
	}

	type capturedRequest struct {
		method      string
		path        string
		contentType string
		body        []byte
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := make(chan capturedRequest, 2)
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Errorf("read request body: %v", err)
				}
				requests <- capturedRequest{
					method: request.Method, path: request.URL.Path,
					contentType: request.Header.Get("Content-Type"), body: body,
				}
				_, _ = writer.Write([]byte("ok"))
			}))
			defer server.Close()

			client, err := slackapi.NewClient(server.URL+"/services/", "/T000/B000/legacy-token", slackapi.WithHTTPClient(server.Client()))
			if err != nil {
				t.Fatal(err)
			}
			date := legacyDate(2025, 3, 31)
			provider := legacyMenuProviderFunc(func(context.Context, domain.LocalDate) (domain.Menu, error) {
				return domain.Menu{Date: date, Menus: []string{"김치찌개", "된장찌개", "제육볶음"}, Valid: true}, nil
			})
			notifier := NewNotificationService(provider, legacySlackClientAdapter{client: client}, NotificationConfig{
				Channel: test.channel, Username: "점심봇", IconEmoji: ":bento:",
			}, WithNotificationDateClock(func() domain.LocalDate { return date }))

			if err := notifier.NotifyToday(context.Background()); err != nil {
				t.Fatal(err)
			}
			got := <-requests
			wantBody := []byte(`{"channel":"#foodbox","username":"점심봇","text":"\u003c2025-03-31 월요일\u003e 점심 메뉴\n• 김치찌개\n• 된장찌개\n• 제육볶음","icon_emoji":":bento:"}`)
			if got.method != http.MethodPost || got.path != "/services/T000/B000/legacy-token" || got.contentType != "application/json" {
				t.Fatalf("method = %q, path = %q, content type = %q", got.method, got.path, got.contentType)
			}
			if !bytes.Equal(got.body, wantBody) {
				t.Fatalf("request JSON bytes = %s, want %s", got.body, wantBody)
			}
			select {
			case extra := <-requests:
				t.Fatalf("unexpected extra request: %+v", extra)
			default:
			}
		})
	}
}
