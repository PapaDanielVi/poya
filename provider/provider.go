// Package provider defines the Provider interface for configuration backends.
package provider

import "context"

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
