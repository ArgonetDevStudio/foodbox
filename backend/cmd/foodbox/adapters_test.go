package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/LooLookProject/foodbox/backend/internal/clova"
	"github.com/LooLookProject/foodbox/backend/internal/service"
	"github.com/LooLookProject/foodbox/backend/internal/slack"
)

type fakeClovaClient struct {
	raw []byte
	err error
}

func (client fakeClovaClient) Recognize(context.Context, []byte) (*clova.Response, []byte, error) {
	return &clova.Response{}, client.raw, client.err
}

func TestClovaAdapterReturnsRawResponse(t *testing.T) {
	want := []byte(`{"images":[]}`)
	got, err := (clovaAdapter{client: fakeClovaClient{raw: want}}).Recognize(context.Background(), []byte("image"))
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Recognize raw response = %q, want %q", got, want)
	}
}

func TestClovaAdapterPreservesError(t *testing.T) {
	want := errors.New("recognition failed")
	_, got := (clovaAdapter{client: fakeClovaClient{err: want}}).Recognize(context.Background(), []byte("image"))
	if !errors.Is(got, want) {
		t.Fatalf("Recognize error = %v, want %v", got, want)
	}
}

type fakeSlackClient struct {
	message slack.Message
	err     error
}

func (client *fakeSlackClient) Send(_ context.Context, message slack.Message) error {
	client.message = message
	return client.err
}

func TestSlackAdapterMapsMessage(t *testing.T) {
	client := &fakeSlackClient{}
	want := service.SlackMessage{
		Channel:   "#lunch",
		Username:  "foodbox",
		Text:      "menu",
		IconEmoji: ":bento:",
	}
	if err := (slackAdapter{client: client}).Send(context.Background(), want); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got := service.SlackMessage{
		Channel:   client.message.Channel,
		Username:  client.message.Username,
		Text:      client.message.Text,
		IconEmoji: client.message.IconEmoji,
	}
	if got != want {
		t.Fatalf("Send message = %#v, want %#v", got, want)
	}
}

func TestSlackAdapterPreservesError(t *testing.T) {
	want := errors.New("send failed")
	client := &fakeSlackClient{err: want}
	got := (slackAdapter{client: client}).Send(context.Background(), service.SlackMessage{})
	if !errors.Is(got, want) {
		t.Fatalf("Send error = %v, want %v", got, want)
	}
}
