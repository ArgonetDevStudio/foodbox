package main

import (
	"context"

	"github.com/LooLookProject/foodbox/backend/internal/clova"
	"github.com/LooLookProject/foodbox/backend/internal/service"
	"github.com/LooLookProject/foodbox/backend/internal/slack"
)

type clovaClient interface {
	Recognize(context.Context, []byte) (*clova.Response, []byte, error)
}

type clovaAdapter struct {
	client clovaClient
}

func (adapter clovaAdapter) Recognize(ctx context.Context, image []byte) ([]byte, error) {
	_, raw, err := adapter.client.Recognize(ctx, image)
	return raw, err
}

type slackClient interface {
	Send(context.Context, slack.Message) error
}

type slackAdapter struct {
	client slackClient
}

func (adapter slackAdapter) Send(ctx context.Context, message service.SlackMessage) error {
	return adapter.client.Send(ctx, slack.Message{
		Channel:   message.Channel,
		Username:  message.Username,
		Text:      message.Text,
		IconEmoji: message.IconEmoji,
	})
}
