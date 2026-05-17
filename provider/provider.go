// Package provider defines the Provider interface for configuration backends.
package provider

import "context"

// Provider is the interface that all config backends must implement.
// Each provider decides its own strategy for watching changes:
//   - etcd: native Watch API (event-driven)
//   - Redis: polling loop at configurable frequency
//   - Vault: polling via Vault SDK
type Provider interface {
	// Get retrieves the current raw value for a key from the backend.
	Get(ctx context.Context, key string) (string, error)

	// Watch monitors a key for changes. When a change is detected,
	// onChange is called with the key and the new raw string value.
	// The implementation must block until the context is cancelled or an error occurs.
	Watch(ctx context.Context, key string, onChange func(key string, value string)) error

	// Close releases any resources held by the provider.
	Close() error
}
