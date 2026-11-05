//nolint:revive
package maxbot

type Config struct {
	ApiUrl           string   `yaml:"api_url"`
	Token            string   `yaml:"token"`
	Url              string   `yaml:"url"`
	Path             string   `yaml:"path"`
	Secret           string   `yaml:"secret"` // nolint:gosec
	SubscriptionType string   `yaml:"subscription_type"`
	Types            []string `yaml:"types"`
	Version          string   `yaml:"version"`
}

func (c Config) IsWebhook() bool {
	return c.SubscriptionType == subscribeTypeWebhook
}
