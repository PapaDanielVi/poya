// Package redis implements the provider.Provider interface for Redis configuration backends.
// It watches every registered key with a single keyspace-notification subscription
// scoped to their common prefix, so changes are delivered as events instead of being
// discovered by polling. An optional resync interval re-reads all keys as a safety net.
package redis

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PapaDanielVi/poya/provider"
	goredis "github.com/redis/go-redis/v9"
)

var _ provider.Provider = (*Provider)(nil)

// keyspaceChannelFmt is the pub/sub channel pattern Redis publishes key events on
// when keyspace notifications are enabled. %d is the database number.
const keyspaceChannelFmt = "__keyspace@%d__:"

// Config holds Redis-specific configuration.
type Config struct {
	Addr     string // Redis address, e.g. "localhost:6379".
	Password string // Redis password (empty if no auth).
	DB       int    // Redis database number.

	// ResyncInterval, when greater than zero, re-reads every key on a timer as a
	// safety net against missed notifications (e.g. a dropped connection). Watching
	// itself is event-driven via keyspace notifications and does not require it.
	ResyncInterval time.Duration
}

// Provider implements the poya Provider interface using Redis keyspace notifications.
// A single pattern subscription covers every key under the registered common prefix.
type Provider struct {
	client         *goredis.Client
	db             int
	resyncInterval time.Duration
}

// New creates a new Redis provider connected to the given address.
func New(cfg Config) *Provider {
	return &Provider{
		client: goredis.NewClient(&goredis.Options{
			Addr:     cfg.Addr,
			Password: cfg.Password,
			DB:       cfg.DB,
		}),
		db:             cfg.DB,
		resyncInterval: cfg.ResyncInterval,
	}
}

// Get retrieves the current value for a key from Redis.
func (p *Provider) Get(ctx context.Context, key string) (string, error) {
	val, err := p.client.Get(ctx, key).Result()
	if err == goredis.Nil {
		return "", nil
	}
	return val, err
}

// Watch subscribes to keyspace notifications for the common prefix of all keys
// using a single pattern subscription. When Redis publishes a change for a watched
// key, onChange is called with the key and its new value. One subscription and one
// goroutine cover every key, regardless of how many are registered.
func (p *Provider) Watch(ctx context.Context, keys []string, onChange func(key string, value string)) error {
	if len(keys) == 0 {
		<-ctx.Done()
		return nil
	}

	// Keyspace notifications are off by default; enable key events for all commands.
	if err := p.client.ConfigSet(ctx, "notify-keyspace-events", "KEA").Err(); err != nil {
		return fmt.Errorf("redis: failed to enable keyspace notifications: %w", err)
	}

	watched := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		watched[k] = struct{}{}
	}

	channelPrefix := fmt.Sprintf(keyspaceChannelFmt, p.db)
	pattern := channelPrefix + provider.CommonPrefix(keys) + "*"
	pubsub := p.client.PSubscribe(ctx, pattern)
	defer pubsub.Close() //nolint:errcheck,nolintlint

	lastValues := p.seed(ctx, keys, onChange)

	var resync <-chan time.Time
	if p.resyncInterval > 0 {
		ticker := time.NewTicker(p.resyncInterval)
		defer ticker.Stop()
		resync = ticker.C
	}

	msgCh := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-msgCh:
			if !ok {
				return nil
			}
			key := strings.TrimPrefix(msg.Channel, channelPrefix)
			if _, watching := watched[key]; !watching {
				continue
			}
			val, _ := p.Get(ctx, key)
			if val != lastValues[key] {
				lastValues[key] = val
				onChange(key, val)
			}
		case <-resync:
			p.resyncAll(ctx, keys, lastValues, onChange)
		}
	}
}

// seed reads all keys once via a single MGET and emits their initial values.
func (p *Provider) seed(ctx context.Context, keys []string, onChange func(key string, value string)) map[string]string {
	lastValues := make(map[string]string, len(keys))
	vals, err := p.client.MGet(ctx, keys...).Result()
	if err != nil {
		return lastValues
	}
	for i, v := range vals {
		s, _ := v.(string)
		lastValues[keys[i]] = s
		if s != "" {
			onChange(keys[i], s)
		}
	}
	return lastValues
}

// resyncAll re-reads every key via a single MGET and emits any that changed.
func (p *Provider) resyncAll(ctx context.Context, keys []string, lastValues map[string]string, onChange func(key string, value string)) {
	vals, err := p.client.MGet(ctx, keys...).Result()
	if err != nil {
		return
	}
	for i, v := range vals {
		key := keys[i]
		newVal, _ := v.(string)
		if newVal != lastValues[key] {
			lastValues[key] = newVal
			onChange(key, newVal)
		}
	}
}

// Close disconnects from Redis.
func (p *Provider) Close() error {
	return p.client.Close()
}
