// Package etcd implements the provider.Provider interface for etcd configuration backends.
// It uses etcd's native Watch API with prefix-based watching for efficient event-driven
// dynamic configuration updates. A single watch call monitors all keys under a common prefix.
package etcd

import (
	"context"
	"strings"
	"time"

	"github.com/PapaDanielVi/poya/provider"
	"go.etcd.io/etcd/client/v3"
)

var _ provider.Provider = (*Provider)(nil)

// Config holds etcd-specific configuration.
type Config struct {
	Endpoints   []string      // etcd endpoints, e.g. []string{"localhost:2379"}
	DialTimeout time.Duration // timeout for establishing connection
}

// Provider implements the poya Provider interface using etcd's native Watch API.
type Provider struct {
	client *clientv3.Client
}

// New creates a new etcd provider connected to the given endpoints.
func New(cfg Config) (*Provider, error) {
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = 5 * time.Second //nolint:mnd // default dial timeout
	}
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: cfg.DialTimeout,
	})
	if err != nil {
		return nil, err
	}
	return &Provider{client: cli}, nil
}

// Get retrieves the current value for a key from etcd.
func (p *Provider) Get(ctx context.Context, key string) (string, error) {
	resp, err := p.client.Get(ctx, key)
	if err != nil {
		return "", err
	}
	if len(resp.Kvs) == 0 {
		return "", nil
	}
	return string(resp.Kvs[0].Value), nil
}

// Watch monitors all keys under a common prefix using a single etcd Watch call.
// It extracts the longest common prefix from the provided keys, watches that prefix,
// and calls onChange for each key that changes.
func (p *Provider) Watch(ctx context.Context, keys []string, onChange func(key string, value string)) error {
	if len(keys) == 0 {
		<-ctx.Done()
		return nil
	}

	prefix := provider.CommonPrefix(keys)
	if prefix == "" {
		prefix = "/"
	}
	// Ensure prefix ends with "/" for proper range watching
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	// Load the current values before watching so registered keys reflect the
	// backend immediately instead of staying at their defaults until the next
	// change. Watch resumes from the revision returned by the initial read so no
	// event between the two calls is missed.
	resp, err := p.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return err
	}
	for _, kv := range resp.Kvs {
		onChange(string(kv.Key), string(kv.Value))
	}

	watchCh := p.client.Watch(ctx, prefix, clientv3.WithPrefix(), clientv3.WithRev(resp.Header.Revision+1))
	for {
		select {
		case <-ctx.Done():
			return nil
		case watchResp, ok := <-watchCh:
			if !ok {
				return nil
			}
			if watchResp.Err() != nil {
				return watchResp.Err()
			}
			for _, event := range watchResp.Events {
				if event.Type == clientv3.EventTypePut {
					onChange(string(event.Kv.Key), string(event.Kv.Value))
				}
			}
		}
	}
}

// Close disconnects from etcd.
func (p *Provider) Close() error {
	return p.client.Close()
}
