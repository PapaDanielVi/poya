// Package vault implements the provider.Provider interface for HashiCorp Vault KV v2 backends.
// It polls Vault secrets at a configurable interval to sync dynamic configuration and feature flags.
// All keys are fetched in a single goroutine per poll cycle for efficiency.
package vault

import (
	"context"
	"fmt"
	"time"

	"github.com/PapaDanielVi/poya/provider"
	vault "github.com/hashicorp/vault/api"
)

var _ provider.Provider = (*Provider)(nil)

// Config holds Vault-specific configuration.
type Config struct {
	Address      string        // Vault address, e.g. "http://localhost:8200"
	Token        string        // Vault token for authentication
	PollInterval time.Duration // how often to check for changes
	MountPath    string        // KV secrets engine mount path, e.g. "secret"
}

// Provider implements the poya Provider interface using Vault's KV secrets engine.
// All keys are fetched in a single goroutine per poll cycle.
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

// Get retrieves the current value for a key from Vault.
// The key is treated as a path within the configured KV mount.
// For KV v2, the path is prefixed with "data/" automatically.
func (p *Provider) Get(ctx context.Context, key string) (string, error) {
	secret, err := p.client.KVv2(p.mountPath).Get(ctx, key)
	if err != nil {
		return "", fmt.Errorf("failed to read from vault: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return "", nil
	}
	for _, v := range secret.Data {
		if s, ok := v.(string); ok {
			return s, nil
		}
	}
	return "", nil
}

// Watch polls all keys at the configured interval in a single goroutine.
// When any value changes, onChange is called with the key and new value.
func (p *Provider) Watch(ctx context.Context, keys []string, onChange func(key string, value string)) error {
	if len(keys) == 0 {
		<-ctx.Done()
		return nil
	}

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	lastValues := make(map[string]string, len(keys))

	// Initial fetch for all keys
	for _, key := range keys {
		val, _ := p.Get(ctx, key)
		lastValues[key] = val
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			for _, key := range keys {
				val, err := p.Get(ctx, key)
				if err != nil {
					continue
				}
				if val != lastValues[key] {
					lastValues[key] = val
					onChange(key, val)
				}
			}
		}
	}
}

// Close cleans up Vault client resources.
func (p *Provider) Close() error {
	return nil
}
