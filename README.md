# poya

[![CI](https://github.com/PapaDanielVi/poya/actions/workflows/ci.yml/badge.svg)](https://github.com/PapaDanielVi/poya/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/report/github.com/PapaDanielVi/poya)](https://goreportcard.com/report/github.com/PapaDanielVi/poya)
[![Go Reference](https://pkg.go.dev/badge/github.com/PapaDanielVi/poya.svg)](https://pkg.go.dev/github.com/PapaDanielVi/poya)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**poya** is a Go SDK for dynamic runtime configuration and configuration management. Register typed config values, connect a backend provider (etcd, Redis, HashiCorp Vault, MySQL, or PostgreSQL), and the SDK keeps everything in sync in the background. Your application only calls `Get()` — no polling, no refresh logic. Supports use cases including feature flags, A/B testing, service discovery, and runtime parameter tuning.

## Features

- **Type-safe generics** — `DcValue[string]`, `DcValue[int]`, `DcValue[YourConfig]`, any type you need
- **Scalar and struct values** — a single `DcValue[T]` type handles both; scalars are parsed via type switch, structs are JSON-decoded automatically
- **Declarative config structs** — define your entire config layout in a single struct with tags; poya discovers and registers all fields via reflection
- **Multiple providers** — etcd (watch API), Redis (polling), HashiCorp Vault (KV v2 polling), MySQL (polling), PostgreSQL (polling)
- **Lock-free reads** — `Get()` uses `atomic.Value` for zero-contention reads on the hot path
- **Pluggable metrics** — Prometheus (default), OpenTelemetry, expvar, or inject your own implementation
- **Structured logging** — inject any logger; defaults to stderr via `log/slog`
- **Prefix & nesting** — hierarchical key management with automatic prefix accumulation for nested structs
- **Graceful shutdown** — context-based cancellation cleans up all background goroutines

## Supported Scalar Types

`DcValue[T]` supports these scalar types for automatic string parsing:

| Category          | Types                                         |
| ----------------- | --------------------------------------------- |
| Signed integers   | `int`, `int8`, `int16`, `int32`, `int64`      |
| Unsigned integers | `uint`, `uint8`, `uint16`, `uint32`, `uint64` |
| Floating point    | `float32`, `float64`                          |
| Other             | `string`, `bool`, `time.Duration`, `[]byte`   |

Any other type falls through to raw string return.

## Installation

```bash
go get github.com/PapaDanielVi/poya
```

Requires Go 1.26+.

## Quick Start

```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/PapaDanielVi/poya"
	"github.com/PapaDanielVi/poya/provider/redis"
)

func main() {
	// 1. Create a provider
	rdb := redis.New(redis.Config{
		Addr:         "localhost:6379",
		PollInterval: 5 * time.Second,
	})

	// 2. Create the SDK
	sdk := poya.New(poya.Config{
		Provider:      rdb,
		Prefix:        "myapp/",
		EnableMetrics: true,
	})

	// 3. Register values individually
	timeout := poya.NewDcValue("30s")
	poya.Register(sdk, "timeout", timeout)

	// 4. Start background sync
	sdk.Start()
	defer sdk.Stop()

	// 5. Read values anywhere in your application
	fmt.Println(timeout.Get()) // always the latest value from Redis
}
```

## API Reference

### Values — `DcValue[T]`

A single generic type handles both scalars and structs. The SDK determines which at registration time via reflection.

**Scalar values** (`string`, `int`, `bool`, `float64`, etc.):

```go
val := poya.NewDcValue("default_value")
poya.Register(sdk, "my_key", val)

current := val.Get() // returns string
```

**Struct values** — the provider stores a JSON blob; poya decodes it into your struct:

```go
type DatabaseConfig struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	MaxConn int    `json:"max_conn"`
}

dbDefault := DatabaseConfig{Host: "localhost", Port: 5432, MaxConn: 10}
dbVal := poya.NewDcValue(dbDefault)
poya.Register(sdk, "database", dbVal)

cfg := dbVal.Get() // returns DatabaseConfig
fmt.Println(cfg.Host)
```

### Declarative Config Structs — `RegisterConfig`

Define your entire configuration in a single struct. poya uses struct tags to discover fields:

```go
type AppConfig struct {
	Timeout  poya.DcValue[string]        `poya:"key=timeout"`
	Verbose  poya.DcValue[bool]          `poya:"key=verbose"`
	DBConfig poya.DcValue[DatabaseConfig] `poya:"key=db_config"`
	DB       DBConfig                     `poya:"prefix=db"`
}

type DBConfig struct {
	Host poya.DcValue[string] `poya:"key=host"`
	Port poya.DcValue[int]    `poya:"key=port"`
}

cfg := AppConfig{
	Timeout:  *poya.NewDcValue("30s"),
	Verbose:  *poya.NewDcValue(false),
	DBConfig: *poya.NewDcValue(DatabaseConfig{Host: "localhost", Port: 5432}),
	DB: DBConfig{
		Host: *poya.NewDcValue("localhost"),
		Port: *poya.NewDcValue(5432),
	},
}

sdk.RegisterConfig(&cfg)
// Registers: myapp/timeout, myapp/verbose, myapp/db_config, myapp/db/host, myapp/db/port
```

#### Tag Format

| Tag                         | Meaning                                                 |
| --------------------------- | ------------------------------------------------------- |
| `poya:"key=timeout"`        | This field is a config value watched at key `timeout`   |
| `poya:"prefix=db"`          | This nested struct contributes `db/` to child key paths |
| `poya:"key=host,prefix=db"` | Both a value and a prefix for deeper nesting            |

Fields without a tag use their lowercased field name as the key.

### Prefix Handling

Prefixes accumulate hierarchically:

```
Full key = SDK Prefix + Parent Prefixes + Field Key

Example with Prefix="myapp/":
  Timeout field (key=timeout) → "myapp/timeout"
  DB.Host field (key=host, parent prefix="db/") → "myapp/db/host"
```

### Metrics

poya supports multiple metrics backends. Inject any via `Config.Metrics`:

**Prometheus** (default when `EnableMetrics: true`):

```go
sdk := poya.New(poya.Config{
	Provider:      rdb,
	Prefix:        "myapp/",
	EnableMetrics: true,
})
```

**OpenTelemetry**:

```go
meter := otel.Meter("github.com/PapaDanielVi/poya")
otelMetrics, _ := otel.New(meter)
sdk := poya.New(poya.Config{Provider: rdb, Metrics: otelMetrics})
```

**expvar** (zero external dependencies):

```go
sdk := poya.New(poya.Config{Provider: rdb, Metrics: expvar.New()})
```

**Custom implementation**:

```go
sdk := poya.New(poya.Config{Provider: rdb, Metrics: myCustomMetrics})
```

All backends implement the same interface:

```go
type Metrics interface {
	IncWatchEvents(key string)
	IncWatchErrors(key string)
	ObserveUpdateLatency(key string, d time.Duration)
	SetRegisteredKeys(n int)
}
```

When metrics are disabled, a no-op stub is used — no if-checks in hot paths.

**Prometheus metrics:**

| Metric                             | Type      | Description                                  |
| ---------------------------------- | --------- | -------------------------------------------- |
| `poya_watch_events_total`          | Counter   | Total watch events received (labeled by key) |
| `poya_watch_errors_total`          | Counter   | Total watch errors (labeled by key)          |
| `poya_sync_update_latency_seconds` | Histogram | Value update latency (labeled by key)        |
| `poya_registered_keys`             | Gauge     | Number of registered config keys             |

Each SDK instance uses its own Prometheus registry, so multiple instances won't conflict.

### Logging

poya uses a simple structured-logger interface. Inject any logger via `Config.Logger`:

```go
sdk := poya.New(poya.Config{
	Provider: rdb,
	Logger:   myCustomLogger,
})
```

The default logger writes to stderr via `log/slog`. The interface:

```go
type Logger interface {
	Debug(msg string, keysAndValues ...any)
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}
```

## Provider Setup

### etcd

Uses etcd's native Watch API for event-driven updates (no polling):

```go
etcdProvider, err := etcd.New(etcd.Config{
	Endpoints:   []string{"localhost:2379"},
	DialTimeout: 5 * time.Second,
})
if err != nil {
	log.Fatal(err)
}
defer etcdProvider.Close()

sdk := poya.New(poya.Config{Provider: etcdProvider, Prefix: "myapp/"})
```

### Redis

Polls at a configurable interval. Best for simple setups without etcd:

```go
rdb := redis.New(redis.Config{
	Addr:         "localhost:6379",
	Password:     "",       // no auth
	DB:           0,
	PollInterval: 5 * time.Second,
})
defer rdb.Close()

sdk := poya.New(poya.Config{Provider: rdb, Prefix: "myapp/"})
```

### HashiCorp Vault

Polls the KV v2 secrets engine. The key is the secret path within the mount:

```go
v, err := vault.New(vault.Config{
	Address:      "http://localhost:8200",
	Token:        "root-token",
	MountPath:    "secret",
	PollInterval: 10 * time.Second,
})
if err != nil {
	log.Fatal(err)
}

sdk := poya.New(poya.Config{Provider: v, Prefix: "myapp/"})
```

### MySQL

Polls a database table at a configurable interval. Accepts an existing `*sql.DB` connection (you manage the lifecycle):

**Using the default repository** (simple key-value table):

```go
import (
	"database/sql"
	"github.com/PapaDanielVi/poya/provider/mysql"
	_ "github.com/go-sql-driver/mysql"
)

db, _ := sql.Open("mysql", "user:pass@tcp(localhost:3306)/configdb")
provider, _ := mysql.New(mysql.Config{
	DB:           db,
	TableName:    "config",
	KeyColumn:   "config_key",
	ValueColumn: "config_value",
	PollInterval: 5 * time.Second,
})

sdk := poya.New(poya.Config{Provider: provider, Prefix: "myapp/"})
```

**Using a custom repository** (any table schema):

```go
type MyRepository struct {
	db *sql.DB
}

func (r *MyRepository) Get(ctx context.Context, key string) (string, error) {
	// Custom query logic for your schema
	var value string
	err := r.db.QueryRowContext(ctx, "SELECT value FROM my_table WHERE name = ?", key).Scan(&value)
	return value, err
}

provider, _ := mysql.New(mysql.Config{
	Repository:   &MyRepository{db: db},
	PollInterval: 5 * time.Second,
})
```

### PostgreSQL

Same interface as MySQL, with PostgreSQL-specific placeholder syntax:

```go
import (
	"database/sql"
	"github.com/PapaDanielVi/poya/provider/postgresql"
	_ "github.com/lib/pq"
)

db, _ := sql.Open("postgres", "postgres://user:pass@localhost/configdb?sslmode=disable")
provider, _ := postgresql.New(postgresql.Config{
	DB:           db,
	TableName:    "config",
	KeyColumn:   "config_key",
	ValueColumn: "config_value",
	PollInterval: 5 * time.Second,
})

sdk := poya.New(poya.Config{Provider: provider, Prefix: "myapp/"})
```

Custom repositories work identically to MySQL:

```go
provider, _ := postgresql.New(postgresql.Config{
	Repository:   &MyRepository{db: db},
	PollInterval: 5 * time.Second,
})
```

## Use Cases

- **Feature flags** — toggle features at runtime without redeployment
- **Database credentials** — rotate connection strings dynamically
- **Service discovery** — update endpoint lists as services scale
- **Rate limits & thresholds** — adjust operational parameters in real time
- **A/B testing** — change experiment parameters on the fly
- **Multi-tenant config** — per-tenant settings with hierarchical key prefixes

## Project Structure

```
poya/
├── poya.go                    # SDK: New, Start, Stop, Register, RegisterConfig
├── dcvalue.go                 # DcValue[T] — unified scalar + struct config value
├── metrics/
│   ├── metrics.go             # Metrics interface + NoopMetrics stub
│   ├── prometheus/            # Prometheus implementation
│   ├── otel/                  # OpenTelemetry implementation
│   └── expvar/                # expvar implementation (zero dependencies)
├── logger/
│   └── logger.go              # Logger interface + slog default + noop stub
├── provider/
│   ├── provider.go            # Provider interface
│   ├── etcd/                  # etcd provider (native Watch API)
│   ├── redis/                 # Redis provider (polling)
│   ├── vault/                 # HashiCorp Vault provider (KV v2 polling)
│   ├── mysql/                 # MySQL provider (polling, Repository interface)
│   └── postgresql/            # PostgreSQL provider (polling, Repository interface)
└── ...
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on adding providers, value types, and submitting pull requests.

## Keywords

Go, Golang, SDK, dynamic config, runtime configuration, configuration management, feature flags, A/B testing, service discovery, etcd, Redis, HashiCorp Vault, MySQL, PostgreSQL, type-safe config, generic config, Go SDK

## License

[MIT](LICENSE)
