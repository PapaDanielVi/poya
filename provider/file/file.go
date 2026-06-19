// Package file implements provider.Provider for local file-based configuration.
// It watches a file for changes using fsnotify (fsevents on macOS, inotify on Linux)
// and re-reads the file on every change, calling compare-and-swap on dynamic values.
// Supports JSON and YAML flat key:value formats (not nested).
package file

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PapaDanielVi/poya/provider"
	"github.com/fsnotify/fsnotify"
	"go.yaml.in/yaml/v2"
)

var _ provider.Provider = (*Provider)(nil)

// Format specifies the file format.
type Format int

const (
	// FormatJSON parses the file as flat JSON key:value pairs.
	FormatJSON Format = iota
	// FormatYAML parses the file as flat YAML key:value pairs.
	FormatYAML
	// FormatAuto detects the format from the file extension (.json, .yaml, .yml).
	FormatAuto
)

// Config holds file-specific configuration.
type Config struct {
	// Path is the path to the configuration file.
	Path string

	// Format specifies how to parse the file. Default: FormatAuto.
	Format Format
}

// Provider implements the poya Provider interface using file watching.
type Provider struct {
	path   string
	format Format
	mu     sync.Mutex
	values map[string]string
}

// New creates a new file provider that watches the given file.
func New(cfg Config) (*Provider, error) {
	if cfg.Path == "" {
		return nil, errors.New("file provider: path is required")
	}

	format := cfg.Format
	if format == FormatAuto {
		ext := strings.ToLower(filepath.Ext(cfg.Path))
		switch ext {
		case ".json":
			format = FormatJSON
		case ".yaml", ".yml":
			format = FormatYAML
		default:
			return nil, fmt.Errorf("file provider: cannot detect format from extension %q, specify explicitly", ext)
		}
	}

	p := &Provider{
		path:   cfg.Path,
		format: format,
		values: make(map[string]string),
	}

	if err := p.load(); err != nil {
		return nil, fmt.Errorf("file provider: initial load failed: %w", err)
	}

	return p, nil
}

// Get retrieves the current value for a key from the file.
func (p *Provider) Get(_ context.Context, key string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.values[key], nil
}

// Watch monitors the file for changes using fsnotify.
// On each change, it re-reads the file and calls onChange for any key whose value changed.
func (p *Provider) Watch(ctx context.Context, keys []string, onChange func(key string, value string)) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("file provider: failed to create watcher: %w", err)
	}
	defer watcher.Close() //nolint:errcheck,nolintlint

	if addErr := watcher.Add(p.path); addErr != nil {
		return fmt.Errorf("file provider: failed to watch file: %w", addErr)
	}

	// Also watch the directory to handle file renames/recreates (common with atomic writes).
	dir := filepath.Dir(p.path)
	if addErr := watcher.Add(dir); addErr != nil {
		return fmt.Errorf("file provider: failed to watch directory: %w", addErr)
	}

	// Emit the values already loaded from the file so registered keys reflect it
	// immediately instead of staying at their defaults until the first edit.
	p.emitAll(keys, onChange)

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			// Only react to writes, creates, or renames affecting our file.
			if event.Name != p.path && event.Name != "" {
				continue
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				// Small delay to let the writer finish.
				time.Sleep(50 * time.Millisecond) //nolint:mnd // delay for atomic write completion
				p.detectChanges(keys, onChange)
			}
		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			// Silently continue on watch errors; the file may be temporarily unavailable.
			_ = watchErr
		}
	}
}

// detectChanges reloads the file and calls onChange for every key.
func (p *Provider) detectChanges(keys []string, onChange func(key string, value string)) {
	if err := p.load(); err != nil {
		return
	}
	p.emitAll(keys, onChange)
}

// emitAll calls onChange with the currently loaded value of every key.
func (p *Provider) emitAll(keys []string, onChange func(key string, value string)) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, key := range keys {
		onChange(key, p.values[key])
	}
}

// load reads and parses the file into the values map.
func (p *Provider) load() error {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	var raw map[string]any
	switch p.format {
	case FormatJSON:
		if jsonErr := json.Unmarshal(data, &raw); jsonErr != nil {
			return fmt.Errorf("failed to parse JSON: %w", jsonErr)
		}
	case FormatYAML:
		if yamlErr := yaml.Unmarshal(data, &raw); yamlErr != nil {
			return fmt.Errorf("failed to parse YAML: %w", yamlErr)
		}
	case FormatAuto:
		return errors.New("format should have been resolved before load")
	default:
		return fmt.Errorf("unknown format: %v", p.format)
	}

	// Convert each value to a string. Scalars become their plain textual form;
	// objects and arrays are re-encoded as JSON so struct and array DcValues
	// decode them correctly.
	newValues := make(map[string]string, len(raw))
	for k, v := range raw {
		newValues[k] = stringifyValue(v)
	}

	p.mu.Lock()
	p.values = newValues
	p.mu.Unlock()

	return nil
}

// stringifyValue renders a decoded JSON/YAML value as the raw string the SDK
// expects. Scalars use their plain textual form; objects and arrays are
// re-encoded as JSON so struct and array DcValues unmarshal them correctly.
func stringifyValue(v any) string {
	switch v.(type) {
	case map[string]any, []any, map[any]any:
		if b, err := json.Marshal(normalizeYAML(v)); err == nil {
			return string(b)
		}
	}
	return fmt.Sprintf("%v", v)
}

// normalizeYAML converts map[any]any values produced by the YAML decoder into
// map[string]any so they can be JSON-encoded. It recurses through nested maps
// and slices.
func normalizeYAML(v any) any {
	switch val := v.(type) {
	case map[any]any:
		m := make(map[string]any, len(val))
		for k, item := range val {
			m[fmt.Sprintf("%v", k)] = normalizeYAML(item)
		}
		return m
	case map[string]any:
		m := make(map[string]any, len(val))
		for k, item := range val {
			m[k] = normalizeYAML(item)
		}
		return m
	case []any:
		s := make([]any, len(val))
		for i, item := range val {
			s[i] = normalizeYAML(item)
		}
		return s
	default:
		return v
	}
}

// Close releases file provider resources.
func (p *Provider) Close() error {
	return nil
}
