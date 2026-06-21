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

// Config holds Redis provider behavior that is not part of the client itself.
type Config struct {
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

// New creates a Redis provider from a fully-configured go-redis client. The
// caller owns the client and configures every connection option (address, auth,
// TLS, pool sizing, the database number, etc.) on goredis.Options directly. The
// database number used for the keyspace channel is read from client.Options().
func New(client *goredis.Client, cfg Config) *Provider {
	return &Provider{
		client:         client,
		db:             client.Options().DB,
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
// key, onChange is called with the key and its new value. When a key disappears
// (DEL, TTL expiry, or eviction), onDelete is called. One subscription and one
// goroutine cover every key, regardless of how many are registered. If the
// subscription drops (a broken connection, the channel closing) it re-subscribes
// with exponential backoff and re-reads all keys, returning only when the context
// is cancelled.
func (p *Provider) Watch(ctx context.Context, keys []string, onChange func(key string, value string), onDelete func(key string)) error {
	if len(keys) == 0 {
		<-ctx.Done()
		return nil
	}

	watched := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		watched[k] = struct{}{}
	}
	channelPrefix := fmt.Sprintf(keyspaceChannelFmt, p.db)
	pattern := channelPrefix + provider.CommonPrefix(keys) + "*"

	// lastValues persists across reconnects so a re-subscribe only re-emits keys
	// whose value actually changed while the subscription was down.
	lastValues := make(map[string]string, len(keys))

	attempt := 0
	for {
		if ctx.Err() != nil {
			return nil
		}
		if p.subscribeOnce(ctx, keys, watched, channelPrefix, pattern, lastValues, onChange, onDelete) {
			return nil
		}
		if !provider.SleepBackoff(ctx, attempt) {
			return nil
		}
		attempt++
	}
}

// getExists retrieves the value for a key and reports whether it exists in Redis.
// A missing key returns ("", false). A transient error returns ("", true) so the
// SDK does not spuriously revert to the default on a network hiccup.
func (p *Provider) getExists(ctx context.Context, key string) (string, bool) {
	val, err := p.client.Get(ctx, key).Result()
	if err == goredis.Nil {
		return "", false
	}
	if err != nil {
		return "", true
	}
	return val, true
}

// subscribeOnce enables keyspace notifications, subscribes, and consumes events
// until the context is cancelled (returns true) or the subscription drops
// (returns false), signalling the caller to re-subscribe.
func (p *Provider) subscribeOnce(ctx context.Context, keys []string, watched map[string]struct{}, channelPrefix, pattern string, lastValues map[string]string, onChange func(key string, value string), onDelete func(key string)) bool {
	// Keyspace notifications are off by default; enable key events for all commands.
	if err := p.client.ConfigSet(ctx, "notify-keyspace-events", "KEA").Err(); err != nil {
		return false
	}

	pubsub := p.client.PSubscribe(ctx, pattern)
	defer pubsub.Close() //nolint:errcheck,nolintlint

	// Re-read all keys on every (re)connect to load current state and catch any
	// change missed while disconnected.
	p.resyncAll(ctx, keys, lastValues, onChange, onDelete)

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
			return true
		case msg, ok := <-msgCh:
			if !ok {
				return false
			}
			key := strings.TrimPrefix(msg.Channel, channelPrefix)
			if _, watching := watched[key]; !watching {
				continue
			}
			val, exists := p.getExists(ctx, key)
			if !exists {
				if _, had := lastValues[key]; had {
					delete(lastValues, key)
					onDelete(key)
				}
				continue
			}
			if val != lastValues[key] {
				lastValues[key] = val
				onChange(key, val)
			}
		case <-resync:
			p.resyncAll(ctx, keys, lastValues, onChange, onDelete)
		}
	}
}

// resyncAll re-reads every key via a single MGET and emits any that changed or were deleted.
func (p *Provider) resyncAll(ctx context.Context, keys []string, lastValues map[string]string, onChange func(key string, value string), onDelete func(key string)) {
	vals, err := p.client.MGet(ctx, keys...).Result()
	if err != nil {
		return
	}
	for i, v := range vals {
		key := keys[i]
		if v == nil {
			if _, had := lastValues[key]; had {
				delete(lastValues, key)
				onDelete(key)
			}
			continue
		}
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
