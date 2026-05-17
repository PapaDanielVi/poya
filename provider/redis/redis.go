// Package redis provides a poya provider implementation using Redis.
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
	Addr        string        // Redis address, e.g. "localhost:6379"
	Password    string        // Redis password (empty if no auth)
	DB          int           // Redis database number
	PollInterval time.Duration // how often to check for changes
}

// Provider implements the poya Provider interface using a polling strategy.
// Redis has no native watch for arbitrary keys, so we poll at a configurable frequency.
type Provider struct {
	client       *goredis.Client
	pollInterval time.Duration
}

// New creates a new Redis provider connected to the given address.
func New(cfg Config) *Provider {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 5 * time.Second
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

// Watch polls the key at the configured interval.
// When the value changes, onChange is called with the new value.
func (p *Provider) Watch(ctx context.Context, key string, onChange func(key string, value string)) error {
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	var lastValue string

	// Do an initial fetch so we have a baseline
	val, err := p.client.Get(ctx, key).Result()
	if err == nil {
		lastValue = val
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			val, err := p.client.Get(ctx, key).Result()
			if err != nil {
				continue
			}
			if val != lastValue {
				lastValue = val
				onChange(key, val)
			}
		}
	}
}

// Close disconnects from Redis.
func (p *Provider) Close() error {
	return p.client.Close()
}
