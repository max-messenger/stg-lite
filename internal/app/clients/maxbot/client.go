//nolint:revive
package maxbot

import (
	"context"
	"fmt"
	"net/http"

	maxbot "github.com/max-messenger/max-bot-api-client-go/v2"
	"go.uber.org/zap"
)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

const (
	subscribeTypeWebhook = "webhook"
	subscribeTypePoll    = "poll"
)

type Client struct {
	logger *zap.Logger
	config Config

	client *maxbot.Api

	bgCtx  context.Context
	cancel context.CancelFunc

	handler maxbot.UpdateHandler
}

func New(logger *zap.Logger, config Config, cli HTTPClient) (*Client, error) {
	opts := []maxbot.Opt{
		maxbot.WithHTTPClient(cli),
	}
	if config.ApiUrl != "" {
		opts = append(opts, maxbot.WithBaseURL(config.ApiUrl))

	}

	maxCli, err := maxbot.NewApi(config.Token, opts...)
	if err != nil {
		return nil, fmt.Errorf("create max client: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Client{
		client: maxCli,

		logger: logger,

		bgCtx:  ctx,
		cancel: cancel,

		config: config,
	}, nil
}

func (c *Client) Start(ctx context.Context) error {
	info, err := c.client.Bots.GetMyInfo(ctx)
	if err != nil {
		return fmt.Errorf("get bot info: %w", err)
	}

	c.logger.Info("bot", zap.Any("info", info))

	err = c.unsubscribe(ctx)
	if err != nil {
		return fmt.Errorf("unsubscribe: %w", err)
	}

	c.logger.Info("subscribe", zap.String("type", c.config.SubscriptionType))

	err = c.subscribe(ctx)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	return nil
}

func (c *Client) Stop(_ context.Context) error {
	c.cancel()

	return nil
}
