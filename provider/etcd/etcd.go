// Package etcd implements the provider.Provider interface for etcd configuration backends.
// It uses etcd's native Watch API with prefix-based watching for efficient event-driven
// dynamic configuration updates. A single watch call monitors all keys under a common prefix.
package etcd

import (
	"context"
	"strings"

	"github.com/PapaDanielVi/poya/provider"
	"go.etcd.io/etcd/client/v3"
)

var _ provider.Provider = (*Provider)(nil)

// Provider implements the poya Provider interface using etcd's native Watch API.
type Provider struct {
	client *clientv3.Client
}

// New creates an etcd provider from a fully-configured etcd client. The caller
// owns the client and configures every connection option (endpoints, TLS, auth,
// dial timeout, etc.) directly on clientv3. The provider does not close the
// client; call clientv3.Client.Close yourself when done, or use Provider.Close
// which delegates to it.
func New(client *clientv3.Client) *Provider {
	return &Provider{client: client}
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
// It extracts the longest common prefix from the provided keys, watches that
// prefix, and calls onChange for each key that changes. If the watch is dropped
// (compaction, a broken connection, the channel closing) it re-reads the current
// values and re-establishes the watch with exponential backoff, returning only
// when the context is cancelled.
func (p *Provider) Watch(ctx context.Context, keys []string, onChange func(key string, value string)) error {
	if len(keys) == 0 {
		<-ctx.Done()
		return nil
	}

	prefix := provider.CommonPrefix(keys)
	if prefix == "" {
		prefix = "/"
	}
	// Ensure prefix ends with "/" for proper range watching.
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	attempt := 0
	for {
		if ctx.Err() != nil {
			return nil
		}

		// Load the current values before watching so registered keys reflect the
		// backend immediately instead of staying at their defaults until the next
		// change. On a reconnect this also catches any change missed while the
		// watch was down. The watch resumes from the read's revision so no event
		// between the two calls is missed.
		resp, err := p.client.Get(ctx, prefix, clientv3.WithPrefix())
		if err != nil {
			if !provider.SleepBackoff(ctx, attempt) {
				return nil
			}
			attempt++
			continue
		}
		for _, kv := range resp.Kvs {
			onChange(string(kv.Key), string(kv.Value))
		}
		// A successful (re)connect resets the backoff.
		attempt = 0

		watchCh := p.client.Watch(ctx, prefix, clientv3.WithPrefix(), clientv3.WithRev(resp.Header.Revision+1))
		if p.drain(ctx, watchCh, onChange) {
			return nil
		}

		// The watch ended without the context being cancelled; back off and
		// re-establish it.
		if !provider.SleepBackoff(ctx, attempt) {
			return nil
		}
		attempt++
	}
}

// drain consumes watch events until the context is cancelled (returns true) or
// the watch channel closes or reports an error (returns false), signalling the
// caller to re-establish the watch.
func (p *Provider) drain(ctx context.Context, watchCh clientv3.WatchChan, onChange func(key string, value string)) bool {
	for {
		select {
		case <-ctx.Done():
			return true
		case watchResp, ok := <-watchCh:
			if !ok || watchResp.Err() != nil {
				return false
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
