// Package redis implements the provider.Provider interface for Redis configuration backends.
// It polls Redis at a configurable interval to sync dynamic configuration values.
// All keys are fetched in a single MGET call per poll cycle for efficiency.
package redis

import (
	"context"
	"time"

	"github.com/PapaDanielVi/poya/provider"
	goredis "github.com/redis/go-redis/v9"
)

var _ provider.Provider = (*Provider)(nil)

// Config holds Redis-specific configuration.
type Config struct {
	Addr         string        // Redis address, e.g. "localhost:6379"
	Password     string        // Redis password (empty if no auth)
	DB           int           // Redis database number
	PollInterval time.Duration // how often to check for changes
}

// Provider implements the poya Provider interface using a polling strategy.
// All keys are fetched in a single MGET call per poll cycle.
type Provider struct {
	client       *goredis.Client
	pollInterval time.Duration
}

// New creates a new Redis provider connected to the given address.
func New(cfg Config) *Provider {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 5 * time.Second //nolint:mnd // default poll interval
	}
	return &Provider{
		client: goredis.NewClient(&goredis.Options{
			Addr:     cfg.Addr,
			Password: cfg.Password,
			DB:       cfg.DB,
		}),
		pollInterval: cfg.PollInterval,
	}
}

// Get retrieves the current value for a key from Redis.
func (p *Provider) Get(ctx context.Context, key string) (string, error) {
	return p.client.Get(ctx, key).Result()
}

// Watch polls all keys at the configured interval using a single MGET call.
// When any value changes, onChange is called with the key and new value.
func (p *Provider) Watch(ctx context.Context, keys []string, onChange func(key string, value string)) error {
	if len(keys) == 0 {
		<-ctx.Done()
		return nil
	}

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	lastValues := make(map[string]string, len(keys))

	// Initial fetch: get all keys in one MGET call
	vals, _ := p.client.MGet(ctx, keys...).Result()
	for i, v := range vals {
		if v != nil {
			if s, ok := v.(string); ok {
				lastValues[keys[i]] = s
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			pollVals, err := p.client.MGet(ctx, keys...).Result()
			if err != nil {
				continue
			}
			for i, v := range pollVals {
				key := keys[i]
				newVal := ""
				if v != nil {
					if s, ok := v.(string); ok {
						newVal = s
					}
				}
				if newVal != lastValues[key] {
					lastValues[key] = newVal
					onChange(key, newVal)
				}
			}
		}
	}
}

// Close disconnects from Redis.
func (p *Provider) Close() error {
	return p.client.Close()
}
