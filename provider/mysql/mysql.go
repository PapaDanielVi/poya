// Package mysql implements the provider.Provider interface for MySQL configuration backends.
// It polls a database table at a configurable interval to sync dynamic configuration values.
// Supports custom Repository implementations for non-standard table schemas.
// All keys are fetched in a single query per poll cycle for efficiency.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/PapaDanielVi/poya/provider"
)

var _ provider.Provider = (*Provider)(nil)

// Repository defines how config values are read from MySQL.
// Users can implement this interface for custom table schemas, or use
// the DefaultRepository for a simple key-value table.
type Repository interface {
	Get(ctx context.Context, key string) (string, error)
	GetAll(ctx context.Context, keys []string) (map[string]string, error)
}

// DefaultRepository provides a simple key-value query against a MySQL table.
type DefaultRepository struct {
	db          *sql.DB
	tableName   string
	keyColumn   string
	valueColumn string
}

// NewDefaultRepository creates the default repository that queries a key-value table.
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

// GetAll retrieves values for multiple keys in a single query.
func (r *DefaultRepository) GetAll(ctx context.Context, keys []string) (map[string]string, error) {
	if len(keys) == 0 {
		return make(map[string]string), nil
	}
	placeholders := make([]string, len(keys))
	args := make([]interface{}, len(keys))
	for i, k := range keys {
		placeholders[i] = "?"
		args[i] = k
	}
	query := fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s IN (%s)",
		r.keyColumn, r.valueColumn, r.tableName, r.keyColumn, strings.Join(placeholders, ","))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("mysql repository get all: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string, len(keys))
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		result[key] = value
	}
	return result, rows.Err()
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
// All keys are fetched in a single query per poll cycle.
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
//	    DB:           db,
//	    TableName:    "config",
//	    KeyColumn:    "config_key",
//	    ValueColumn:  "config_value",
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

// Watch polls all keys at the configured interval using a single query.
// When any value changes, onChange is called with the key and new value.
func (p *Provider) Watch(ctx context.Context, keys []string, onChange func(key string, value string)) error {
	if len(keys) == 0 {
		<-ctx.Done()
		return nil
	}

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	lastValues := make(map[string]string, len(keys))

	// Initial fetch
	vals, _ := p.repo.GetAll(ctx, keys)
	for k, v := range vals {
		lastValues[k] = v
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			vals, err := p.repo.GetAll(ctx, keys)
			if err != nil {
				continue
			}
			for _, key := range keys {
				newVal := vals[key]
				if newVal != lastValues[key] {
					lastValues[key] = newVal
					onChange(key, newVal)
				}
			}
		}
	}
}

// Close releases MySQL provider resources.
// Note: the *sql.DB is managed by the caller and is not closed here.
func (p *Provider) Close() error {
	return nil
}
