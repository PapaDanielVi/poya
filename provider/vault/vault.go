// Package vault implements the provider.Provider interface for HashiCorp Vault KV v2 backends.
// Every registered key is read as a field of a single KV secret located at the keys' common
// prefix, so one Vault read per poll cycle covers all keys instead of one read per key.
// Vault has no native watch API, so changes are discovered by polling at a configurable interval.
package vault

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PapaDanielVi/poya/provider"
	vault "github.com/hashicorp/vault/api"
)

var _ provider.Provider = (*Provider)(nil)

const (
	// defaultPollInterval is how often the provider polls Vault when the caller
	// does not set one.
	defaultPollInterval = 10 * time.Second
	// defaultMountPath is the KV v2 mount used when the caller does not set one.
	defaultMountPath = "secret"
)

// Config holds Vault provider behavior that is not part of the client itself.
type Config struct {
	PollInterval time.Duration // how often to check for changes. Default: 10s.
	MountPath    string        // KV secrets engine mount path. Default: "secret".
}

// Provider implements the poya Provider interface using Vault's KV v2 secrets engine.
// All keys are read from one secret per poll cycle.
type Provider struct {
	client       *vault.Client
	pollInterval time.Duration
	mountPath    string
}

// New creates a Vault provider from a fully-configured Vault client. The caller
// owns the client and configures every connection option (address, TLS,
// namespace, the auth token, etc.) on the vault.Client directly, including
// calling SetToken. PollInterval and MountPath default to 10s and "secret".
func New(client *vault.Client, cfg Config) (*Provider, error) {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = defaultPollInterval
	}
	if cfg.MountPath == "" {
		cfg.MountPath = defaultMountPath
	}

	return &Provider{
		client:       client,
		pollInterval: cfg.PollInterval,
		mountPath:    cfg.MountPath,
	}, nil
}

// splitKey splits a full key into the secret path and the field name within
// that secret. For "myapp/timeout" it returns ("myapp", "timeout"). A key with
// no slash is treated as a field at the mount root.
func splitKey(key string) (path, field string) {
	if idx := strings.LastIndexByte(key, '/'); idx >= 0 {
		return key[:idx], key[idx+1:]
	}
	return "", key
}

// readSecret reads the KV v2 secret at path and returns its fields as strings.
func (p *Provider) readSecret(ctx context.Context, path string) (map[string]string, error) {
	secret, err := p.client.KVv2(p.mountPath).Get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to read from vault: %w", err)
	}
	out := make(map[string]string)
	if secret == nil || secret.Data == nil {
		return out, nil
	}
	for k, v := range secret.Data {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out, nil
}

// Get retrieves the current value for a key from Vault. The key's parent path is
// the secret path and its last segment is the field name within that secret.
func (p *Provider) Get(ctx context.Context, key string) (string, error) {
	path, field := splitKey(key)
	fields, err := p.readSecret(ctx, path)
	if err != nil {
		return "", err
	}
	return fields[field], nil
}

// Watch polls the single secret at the keys' common prefix and reports changed
// fields. One Vault read per cycle covers every registered key.
func (p *Provider) Watch(ctx context.Context, keys []string, onChange func(key string, value string), onDelete func(key string)) error {
	if len(keys) == 0 {
		<-ctx.Done()
		return nil
	}

	prefix := provider.CommonPrefix(keys)
	secretPath := strings.TrimSuffix(prefix, "/")

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	lastValues := make(map[string]string, len(keys))
	p.poll(ctx, secretPath, prefix, keys, lastValues, onChange, onDelete)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			p.poll(ctx, secretPath, prefix, keys, lastValues, onChange, onDelete)
		}
	}
}

// poll reads the secret once and emits any key whose field value changed or was deleted.
func (p *Provider) poll(ctx context.Context, secretPath, prefix string, keys []string, lastValues map[string]string, onChange func(key string, value string), onDelete func(key string)) {
	fields, err := p.readSecret(ctx, secretPath)
	if err != nil {
		// Skip this cycle; the ticker retries on the next interval, so a transient
		// Vault outage self-heals without tearing down the watch.
		return
	}
	for _, key := range keys {
		field := strings.TrimPrefix(key, prefix)
		newVal, ok := fields[field]
		if !ok {
			if _, had := lastValues[key]; had {
				delete(lastValues, key)
				onDelete(key)
			}
			continue
		}
		if newVal != lastValues[key] {
			lastValues[key] = newVal
			onChange(key, newVal)
		}
	}
}

// Close cleans up Vault client resources.
func (p *Provider) Close() error {
	return nil
}
