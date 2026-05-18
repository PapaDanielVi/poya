// Package etcd implements the provider.Provider interface for etcd configuration backends.
// It uses etcd's native Watch API for event-driven dynamic configuration updates.
package etcd

import (
	"context"
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
		cfg.DialTimeout = 5 * time.Second
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

// Watch monitors a key for changes using etcd's native Watch API.
// When a change is detected, onChange is called with the new value.
func (p *Provider) Watch(ctx context.Context, key string, onChange func(key string, value string)) error {
	watchCh := p.client.Watch(ctx, key)
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
