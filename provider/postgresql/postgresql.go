// Package postgresql implements the provider.Provider interface for PostgreSQL configuration backends.
// It polls a database table at a configurable interval to sync dynamic configuration values.
// Supports custom Repository implementations for non-standard table schemas.
// All keys are fetched in a single query per poll cycle for efficiency.
package postgresql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PapaDanielVi/poya/provider"
)

var _ provider.Provider = (*Provider)(nil)

// Repository defines how config values are read from PostgreSQL.
// Users can implement this interface for custom table schemas, or use
// the DefaultRepository for a simple key-value table.
type Repository interface {
	Get(ctx context.Context, key string) (string, error)
	GetAll(ctx context.Context, keys []string) (map[string]string, error)
}

// DefaultRepository provides a simple key-value query against a PostgreSQL table.
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

// Get retrieves the value for a key from the PostgreSQL table.
func (r *DefaultRepository) Get(ctx context.Context, key string) (string, error) {
	//nolint:gosec // G201: table/column names are config-driven, not user input
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1", r.valueColumn, r.tableName, r.keyColumn)
	var value string
	err := r.db.QueryRowContext(ctx, query, key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("postgresql repository get: %w", err)
	}
	return value, nil
}

// GetAll retrieves values for multiple keys in a single query using $N placeholders.
func (r *DefaultRepository) GetAll(ctx context.Context, keys []string) (map[string]string, error) {
	if len(keys) == 0 {
		return make(map[string]string), nil
	}
	placeholders := make([]string, len(keys))
	args := make([]any, len(keys))
	for i, k := range keys {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = k
	}
	//nolint:gosec // G201: table/column names are config-driven, not user input
	query := fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s IN (%s)",
		r.keyColumn, r.valueColumn, r.tableName, r.keyColumn, strings.Join(placeholders, ","))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgresql repository get all: %w", err)
	}
	defer rows.Close() //nolint:errcheck,nolintlint

	result := make(map[string]string, len(keys))
	for rows.Next() {
		var rowKey, rowValue string
		if scanErr := rows.Scan(&rowKey, &rowValue); scanErr != nil {
			continue
		}
		result[rowKey] = rowValue
	}
	return result, rows.Err()
}

// Config holds PostgreSQL-specific configuration.
type Config struct {
	// Repository is the data access layer. If nil, a DefaultRepository
	// must be provided via the DB/Table/KeyColumn/ValueColumn fields.
	Repository Repository

	// DB is an existing [*sql.DB] connection. Users manage the lifecycle
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

// Provider implements the poya Provider interface using polling against PostgreSQL.
// All keys are fetched in a single query per poll cycle.
type Provider struct {
	repo         Repository
	pollInterval time.Duration
}

// New creates a new PostgreSQL provider.
// Pass a Config with either a custom Repository or a pre-configured [*sql.DB].
//
// Using the default repository:
//
//	provider, err := postgresql.New(postgresql.Config{
//	    DB:           db,
//	    TableName:    "config",
//	    KeyColumn:    "config_key",
//	    ValueColumn:  "config_value",
//	    PollInterval: 5 * time.Second,
//	})
//
// Using a custom repository:
//
//	provider, err := postgresql.New(postgresql.Config{
//	    Repository:   myCustomRepo,
//	    PollInterval: 5 * time.Second,
//	})
func New(cfg Config) (*Provider, error) {
	repo := cfg.Repository
	if repo == nil {
		if cfg.DB == nil {
			return nil, errors.New("postgresql provider: either Repository or DB must be provided")
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
		pollInterval = 5 * time.Second //nolint:mnd // default poll interval
	}

	return &Provider{
		repo:         repo,
		pollInterval: pollInterval,
	}, nil
}

// Get retrieves the current value for a key from PostgreSQL.
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

	// lastValues starts empty so the first poll emits every current value,
	// loading the backend state instead of leaving keys at their defaults.
	lastValues := make(map[string]string, len(keys))
	p.poll(ctx, keys, lastValues, onChange)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			p.poll(ctx, keys, lastValues, onChange)
		}
	}
}

// poll fetches all keys in one query and emits any whose value changed since the
// last cycle. An empty lastValues makes it emit every current value.
func (p *Provider) poll(ctx context.Context, keys []string, lastValues map[string]string, onChange func(key string, value string)) {
	pollVals, err := p.repo.GetAll(ctx, keys)
	if err != nil {
		return
	}
	for _, key := range keys {
		newVal := pollVals[key]
		if newVal != lastValues[key] {
			lastValues[key] = newVal
			onChange(key, newVal)
		}
	}
}

// Close releases PostgreSQL provider resources.
// Note: the [*sql.DB] is managed by the caller and is not closed here.
func (p *Provider) Close() error {
	return nil
}
