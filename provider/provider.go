// Package provider defines the Provider interface for configuration backends.
package provider

import (
	"context"
	"strings"
)

// Provider is the interface that all config backends must implement.
// Each provider decides its own strategy for watching changes:
//   - etcd: native Watch API with prefix watching (event-driven)
//   - Redis/MySQL/PostgreSQL/Vault: batch polling of all keys
//   - File: filesystem event notification
type Provider interface {
	// Get retrieves the current raw value for a key from the backend.
	Get(ctx context.Context, key string) (string, error)

	// Watch monitors multiple keys for changes. The provider is free to
	// implement this as a single prefix watch, a batch poll, or any other
	// efficient strategy. When a change is detected for a key, onChange is
	// called with the key and the new raw string value.
	// The implementation must block until the context is cancelled or an error occurs.
	Watch(ctx context.Context, keys []string, onChange func(key string, value string)) error

	// Close releases any resources held by the provider.
	Close() error
}

// CommonPrefix returns the longest common prefix shared by all the given keys.
// Providers use it to watch every registered key with a single prefix-scoped
// operation (an etcd prefix watch, a Redis keyspace subscription, a SQL LIKE
// query) instead of one operation per key. It returns an empty string when the
// keys share no common prefix.
func CommonPrefix(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	prefix := keys[0]
	for _, k := range keys[1:] {
		for !strings.HasPrefix(k, prefix) {
			if prefix == "" {
				return ""
			}
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}
