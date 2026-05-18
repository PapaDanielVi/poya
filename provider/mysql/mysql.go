// Package mysql implements the provider.Provider interface for MySQL configuration backends.
// It polls a database table at a configurable interval to sync dynamic configuration values.
// Supports custom Repository implementations for non-standard table schemas.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/PapaDanielVi/poya/provider"
)

var _ provider.Provider = (*Provider)(nil)

// Repository defines how config values are read from MySQL.
// Users can implement this interface for custom table schemas, or use
// the DefaultRepository for a simple key-value table.
type Repository interface {
	Get(ctx context.Context, key string) (string, error)
}

// DefaultRepository provides a simple key-value query against a MySQL table.
type DefaultRepository struct {
	db          *sql.DB
	tableName   string
	keyColumn   string
	valueColumn string
}

// NewDefaultRepository creates a default repository that queries a key-value table.
// The table is expected to have at least a key column and a value column.
// Example schema:
//
//	CREATE TABLE config (
//	    config_key   VARCHAR(255) PRIMARY KEY,
//	    config_value TEXT
//	);
func NewDefaultRepository(db *sql.DB, table, keyColumn, valueColumn string) *DefaultRepository {
	return &DefaultRepository{
		db:          db,
		tableName:   table,
		keyColumn:   keyColumn,
		valueColumn: valueColumn,
	}
}

// Get retrieves the value for a key from the MySQL table.
func (r *DefaultRepository) Get(ctx context.Context, key string) (string, error) {
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?", r.valueColumn, r.tableName, r.keyColumn)
	var value string
	err := r.db.QueryRowContext(ctx, query, key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("mysql repository get: %w", err)
	}
	return value, nil
}

// Config holds MySQL-specific configuration.
type Config struct {
	// Repository is the data access layer. If nil, a DefaultRepository
	// must be provided via the DB/Table/KeyColumn/ValueColumn fields.
	Repository Repository

	// DB is an existing *sql.DB connection. Users manage the lifecycle
	// (open, close, connection pooling) themselves.
	// Only used when Repository is nil.
	DB *sql.DB

	// TableName is the table to query. Default: "config".
	// Only used when Repository is nil.
	TableName string

	// KeyColumn is the column name for config keys. Default: "config_key".
	// Only used when Repository is nil.
	KeyColumn string

	// ValueColumn is the column name for config values. Default: "config_value".
	// Only used when Repository is nil.
	ValueColumn string

	// PollInterval is how often to check for changes. Default: 5s.
	PollInterval time.Duration
}

// Provider implements the poya Provider interface using polling against MySQL.
// MySQL has no native watch mechanism, so we poll at a configurable frequency.
type Provider struct {
	repo         Repository
	pollInterval time.Duration
}

// New creates a new MySQL provider.
// Pass a Config with either a custom Repository or a pre-configured *sql.DB.
//
// Using the default repository:
//
//	provider, err := mysql.New(mysql.Config{
//	    DB:          db,
//	    TableName:   "config",
//	    KeyColumn:   "config_key",
//	    ValueColumn: "config_value",
//	    PollInterval: 5 * time.Second,
//	})
//
// Using a custom repository:
//
//	provider, err := mysql.New(mysql.Config{
//	    Repository:   myCustomRepo,
//	    PollInterval: 5 * time.Second,
//	})
func New(cfg Config) (*Provider, error) {
	repo := cfg.Repository
	if repo == nil {
		if cfg.DB == nil {
			return nil, fmt.Errorf("mysql provider: either Repository or DB must be provided")
		}
		tableName := cfg.TableName
		if tableName == "" {
			tableName = "config"
		}
		keyColumn := cfg.KeyColumn
		if keyColumn == "" {
			keyColumn = "config_key"
		}
		valueColumn := cfg.ValueColumn
		if valueColumn == "" {
			valueColumn = "config_value"
		}
		repo = NewDefaultRepository(cfg.DB, tableName, keyColumn, valueColumn)
	}

	pollInterval := cfg.PollInterval
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}

	return &Provider{
		repo:         repo,
		pollInterval: pollInterval,
	}, nil
}

// Get retrieves the current value for a key from MySQL.
func (p *Provider) Get(ctx context.Context, key string) (string, error) {
	return p.repo.Get(ctx, key)
}

// Watch polls the key at the configured interval.
// When the value changes, onChange is called with the new value.
func (p *Provider) Watch(ctx context.Context, key string, onChange func(key string, value string)) error {
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	var lastValue string

	// Initial fetch for baseline
	val, _ := p.repo.Get(ctx, key)
	lastValue = val

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			val, err := p.repo.Get(ctx, key)
			if err != nil {
				continue
			}
			if val != lastValue {
				lastValue = val
				onChange(key, val)
			}
		}
	}
}

// Close releases MySQL provider resources.
// Note: the *sql.DB is managed by the caller and is not closed here.
func (p *Provider) Close() error {
	return nil
}
