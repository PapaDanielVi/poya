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

// Config holds Vault-specific configuration.
type Config struct {
	Address      string        // Vault address, e.g. "http://localhost:8200".
	Token        string        // Vault token for authentication.
	PollInterval time.Duration // how often to check for changes.
	MountPath    string        // KV secrets engine mount path, e.g. "secret".
}

// Provider implements the poya Provider interface using Vault's KV v2 secrets engine.
// All keys are read from one secret per poll cycle.
type Provider struct {
	client       *vault.Client
	pollInterval time.Duration
	mountPath    string
}

// New creates a new Vault provider connected to the given address.
func New(cfg Config) (*Provider, error) {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 10 * time.Second //nolint:mnd // default poll interval
	}
	if cfg.MountPath == "" {
		cfg.MountPath = "secret"
	}

	vaultConfig := vault.DefaultConfig()
	vaultConfig.Address = cfg.Address

	client, err := vault.NewClient(vaultConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create vault client: %w", err)
	}

	client.SetToken(cfg.Token)

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
func (p *Provider) Watch(ctx context.Context, keys []string, onChange func(key string, value string)) error {
	if len(keys) == 0 {
		<-ctx.Done()
		return nil
	}

	prefix := provider.CommonPrefix(keys)
	secretPath := strings.TrimSuffix(prefix, "/")

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	lastValues := make(map[string]string, len(keys))
	p.poll(ctx, secretPath, prefix, keys, lastValues, onChange)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			p.poll(ctx, secretPath, prefix, keys, lastValues, onChange)
		}
	}
}

// poll reads the secret once and emits any key whose field value changed.
func (p *Provider) poll(ctx context.Context, secretPath, prefix string, keys []string, lastValues map[string]string, onChange func(key string, value string)) {
	fields, err := p.readSecret(ctx, secretPath)
	if err != nil {
		return
	}
	for _, key := range keys {
		field := strings.TrimPrefix(key, prefix)
		newVal := fields[field]
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
