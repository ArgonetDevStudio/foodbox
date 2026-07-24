package service

import (
	"context"
	"errors"
	"testing"

	"github.com/LooLookProject/foodbox/backend/internal/domain"
)

type menuProviderFunc func(context.Context, domain.LocalDate) (domain.Menu, error)

func (f menuProviderFunc) Today(ctx context.Context, date domain.LocalDate) (domain.Menu, error) {
	return f(ctx, date)
}

type recordingSlackSender struct {
	messages []SlackMessage
	err      error
}

func (s *recordingSlackSender) Send(_ context.Context, message SlackMessage) error {
	s.messages = append(s.messages, message)
	return s.err
}

func TestNotifyWeekendCallsNeitherMenuProviderNorSlack(t *testing.T) {
	providerCalls := 0
	provider := menuProviderFunc(func(context.Context, domain.LocalDate) (domain.Menu, error) {
		providerCalls++
		return domain.Menu{}, nil
	})
	sender := &recordingSlackSender{}
	service := NewNotificationService(provider, sender, NotificationConfig{}, WithNotificationDateClock(fixedDate(2026, 7, 25)))

	result, err := service.Notify(context.Background(), NotifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != NotificationSkippedWeekend || providerCalls != 0 || len(sender.messages) != 0 {
		t.Fatalf("result=%+v provider calls=%d slack calls=%d", result, providerCalls, len(sender.messages))
	}
}

func TestNotifyInvalidMenuSkipsBeforeWednesdayReplacement(t *testing.T) {
	today := domain.LocalDate{Year: 2025, Month: 5, Day: 7}
	provider := menuProviderFunc(func(context.Context, domain.LocalDate) (domain.Menu, error) {
		return domain.Menu{Date: today, Menus: []string{"holiday"}, Valid: false}, nil
	})
	sender := &recordingSlackSender{}
	service := NewNotificationService(provider, sender, NotificationConfig{}, WithNotificationDateClock(func() domain.LocalDate { return today }))

	result, err := service.Notify(context.Background(), NotifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != NotificationSkippedInvalid || len(sender.messages) != 0 {
		t.Fatalf("result=%+v messages=%+v", result, sender.messages)
	}
}

func TestNotifyCreatesExactRegularSlackMessage(t *testing.T) {
	today := domain.LocalDate{Year: 2025, Month: 3, Day: 31}
	menuDate := domain.LocalDate{Year: 2024, Month: 11, Day: 8}
	provider := menuProviderFunc(func(_ context.Context, got domain.LocalDate) (domain.Menu, error) {
		if got != today {
			t.Fatalf("date = %+v", got)
		}
		return domain.Menu{Date: menuDate, Menus: []string{"김치찌개", "된장찌개", "제육볶음"}, Valid: true}, nil
	})
	sender := &recordingSlackSender{}
	service := NewNotificationService(provider, sender, NotificationConfig{
		Channel:  "#foodbox",
		Username: "점심봇",
	}, WithNotificationDateClock(func() domain.LocalDate { return today }))

	result, err := service.Notify(context.Background(), NotifyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := "<2024-11-08 금요일> 점심 메뉴\n• 김치찌개\n• 된장찌개\n• 제육볶음"
	if result.Status != NotificationSent || len(sender.messages) != 1 || sender.messages[0].Text != want {
		t.Fatalf("result=%+v messages=%+v", result, sender.messages)
	}
	if result.Message.IconEmoji != ":bento:" || result.Message.Channel != "#foodbox" || result.Message.Username != "점심봇" {
		t.Fatalf("payload = %+v", result.Message)
	}
}

func TestNotifyWednesdayMessages(t *testing.T) {
	tests := []struct {
		name string
		date domain.LocalDate
		text string
	}{
		{name: "non-last Wednesday is salad day", date: domain.LocalDate{Year: 2025, Month: 4, Day: 23}, text: "<2025-04-23 수요일> 점심 메뉴\n• 데니스델리 🥗"},
		{name: "last Wednesday is eating-out day", date: domain.LocalDate{Year: 2025, Month: 4, Day: 30}, text: "<2025-04-30 수요일> 점심 메뉴\n• 외식 🍽"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := menuProviderFunc(func(context.Context, domain.LocalDate) (domain.Menu, error) {
				return domain.Menu{Date: test.date, Menus: []string{"a", "b", "c"}, Valid: true}, nil
			})
			sender := &recordingSlackSender{}
			service := NewNotificationService(provider, sender, NotificationConfig{}, WithNotificationDateClock(func() domain.LocalDate { return test.date }))

			if err := service.NotifyToday(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(sender.messages) != 1 || sender.messages[0].Text != test.text {
				t.Fatalf("messages = %+v", sender.messages)
			}
		})
	}
}

func TestNotifyDryRunReturnsPayloadWithoutSending(t *testing.T) {
	today := domain.LocalDate{Year: 2025, Month: 3, Day: 31}
	provider := menuProviderFunc(func(context.Context, domain.LocalDate) (domain.Menu, error) {
		return domain.Menu{Date: today, Menus: []string{"a", "b", "c"}, Valid: true}, nil
	})
	sender := &recordingSlackSender{}
	service := NewNotificationService(provider, sender, NotificationConfig{}, WithNotificationDateClock(func() domain.LocalDate { return today }))

	result, err := service.Notify(context.Background(), NotifyOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != NotificationDryRun || result.Message.Text == "" || len(sender.messages) != 0 {
		t.Fatalf("result=%+v messages=%+v", result, sender.messages)
	}
}

func TestNotifyPropagatesProviderAndSenderErrors(t *testing.T) {
	today := domain.LocalDate{Year: 2025, Month: 3, Day: 31}
	providerError := errors.New("repository down")
	provider := menuProviderFunc(func(context.Context, domain.LocalDate) (domain.Menu, error) {
		return domain.Menu{}, providerError
	})
	service := NewNotificationService(provider, nil, NotificationConfig{}, WithNotificationDateClock(func() domain.LocalDate { return today }))
	if _, err := service.Notify(context.Background(), NotifyOptions{}); !errors.Is(err, providerError) {
		t.Fatalf("provider error = %v", err)
	}

	senderError := errors.New("slack down")
	provider = menuProviderFunc(func(context.Context, domain.LocalDate) (domain.Menu, error) {
		return domain.Menu{Date: today, Menus: []string{"a", "b", "c"}, Valid: true}, nil
	})
	sender := &recordingSlackSender{err: senderError}
	service = NewNotificationService(provider, sender, NotificationConfig{}, WithNotificationDateClock(func() domain.LocalDate { return today }))
	if _, err := service.Notify(context.Background(), NotifyOptions{}); !errors.Is(err, senderError) {
		t.Fatalf("sender error = %v", err)
	}
}

func TestWednesdayClassificationAcrossMonthLengths(t *testing.T) {
	tests := []struct {
		date domain.LocalDate
		last bool
	}{
		{date: domain.LocalDate{Year: 2026, Month: 2, Day: 18}, last: false},
		{date: domain.LocalDate{Year: 2026, Month: 2, Day: 25}, last: true},
		{date: domain.LocalDate{Year: 2024, Month: 2, Day: 28}, last: true},
		{date: domain.LocalDate{Year: 2025, Month: 4, Day: 23}, last: false},
		{date: domain.LocalDate{Year: 2025, Month: 4, Day: 30}, last: true},
	}
	for _, test := range tests {
		if got := isLastWednesday(test.date); got != test.last {
			t.Errorf("isLastWednesday(%s) = %v, want %v", formatDate(test.date), got, test.last)
		}
	}
}
