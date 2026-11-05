package maxbot

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	maxbot "github.com/max-messenger/max-bot-api-client-go/v2"
	"github.com/max-messenger/max-bot-api-client-go/v2/model"
	"go.uber.org/zap"
)

func (c *Client) UpdateHandler(handler maxbot.UpdateHandler) (string, http.HandlerFunc) {
	c.handler = handler
	maxHandler := c.client.GetHandler(handler, c.config.Secret)

	return c.config.Path, maxHandler
}

func (c *Client) unsubscribe(ctx context.Context) error {
	result, rErr := c.client.Subscriptions.GetSubscriptions(ctx)
	if rErr != nil {
		return fmt.Errorf("cannot get subscriptions: %w", rErr)
	}

	for _, s := range result.Subscriptions {
		c.logger.Info("unsubscribe", zap.Any("subscription", s))
		res, err := c.client.Subscriptions.Unsubscribe(ctx, s.URL)
		if err != nil {
			c.logger.Error("cannot unsubscribe", zap.Error(err))

			continue
		}
		c.logger.Info("unsubscribe", zap.Any("response", res))
	}

	return nil
}

func (c *Client) subscribe(ctx context.Context) error {
	if c.config.SubscriptionType != subscribeTypeWebhook {
		go c.poolingUpdates()

		return nil
	}
	// subscribe only for webhook
	webhookURL, err := url.JoinPath(c.config.Url, c.config.Path)
	if err != nil {
		return fmt.Errorf("join webhook url: %w", err)
	}

	subscriptionResp, err := c.client.Subscriptions.Subscribe(
		ctx,
		webhookURL,
		c.config.Secret,
		c.config.Types,
		c.config.Version,
	)
	if err != nil {
		return fmt.Errorf("cannot subscribe: %w", err)
	}

	c.logger.Info("subscribe", zap.Any("response", subscriptionResp))

	return nil
}

func (c *Client) poolingUpdates() {
	var marker int64
	c.logger.Info("start pooling updates")

	for {
		select {
		case <-c.bgCtx.Done():
		default:
			var err error
			var updates []model.Update
			updates, marker, err = c.client.Subscriptions.GetUpdates(c.bgCtx, marker)
			if err != nil {
				c.logger.Error("cannot get updates", zap.Error(err))

				continue
			}

			for _, update := range updates {
				c.handler(c.bgCtx, update)
			}
		}
	}
}
