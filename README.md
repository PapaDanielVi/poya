<img src="docs/icon.png" alt="poay" style="border-radius: 50%; width: 300px; height: 300px; object-fit: cover;"/>

# Poya

[![CI](https://github.com/PapaDanielVi/poya/actions/workflows/ci.yml/badge.svg)](https://github.com/PapaDanielVi/poya/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/report/github.com/PapaDanielVi/poya)](https://goreportcard.com/report/github.com/PapaDanielVi/poya)
[![Go Reference](https://pkg.go.dev/badge/github.com/PapaDanielVi/poya.svg)](https://pkg.go.dev/github.com/PapaDanielVi/poya)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**poya** is a Go SDK for dynamic runtime configuration and configuration management. Register typed config values, connect a backend provider (etcd, Redis, HashiCorp Vault, MySQL, PostgreSQL, or local file), and the SDK keeps everything in sync in the background. Your application only calls `Get()` — no polling, no refresh logic. Supports use cases including feature flags, A/B testing, service discovery, and runtime parameter tuning.

## Features

- **Type-safe generics** — `DcValue[string]`, `DcValue[int]`, `DcValue[YourConfig]`, any type you need
- **Scalar, struct, and array values** — a single `DcValue[T]` type handles all three; scalars are parsed via type switch, structs and arrays are JSON-decoded automatically
- **Declarative config structs** — define your entire config layout in a single struct with tags; poya discovers and registers all fields via reflection
- **Multiple providers** — etcd (prefix watch API), Redis (keyspace notifications), HashiCorp Vault (single KV v2 secret), MySQL (batch polling), PostgreSQL (batch polling), File (fsnotify / fsevents)
- **Efficient watching** — every provider watches all keys with one prefix-scoped operation and one goroutine, never per key: etcd uses a single prefix watch, Redis a single keyspace subscription, SQL a single batched query per cycle. Current values load on start, so reads never sit at their defaults.
- **Bring your own client** — each provider takes a fully-configured backend client (etcd, go-redis, Vault, `*sql.DB`), so you control every connection option: TLS, auth, pool sizing, timeouts
- **Resilient watching** — providers reconnect after network failures with exponential backoff and re-read current values, so a dropped connection self-heals without restarting your app
- **Switchable off** — set `Config.Disabled` to skip all connections and watching; every value stays at its default, so you can ship one binary with dynamic config on or off
- **Lock-free reads** — `Get()` uses `atomic.Value` for zero-contention reads on the hot path
- **Pluggable metrics** — Prometheus (default), OpenTelemetry, or inject your own implementation, plus a ready-made Grafana dashboard
- **Structured logging** — defaults to stderr via `log/slog`, with ready-made adapters for zap, logrus, and zerolog, or inject your own
- **Config-loader hooks** — decode hooks seed `DcValue` defaults from your existing config files; works with viper (both mapstructure forks) and koanf v2
- **Wide type support** — every Go scalar kind (including named types like `type Level int`), `time.Duration`, `time.Time`, `[]byte`, and `encoding.TextUnmarshaler` types parse from provider strings; structs and slices decode from JSON
- **Prefix & nesting** — hierarchical key management with automatic prefix accumulation for nested structs
- **Graceful shutdown** — context-based cancellation cleans up all background goroutines

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
	goredis "github.com/redis/go-redis/v9"
)

func main() {
	// 1. Build and configure the backend client yourself, then hand it to a provider.
	client := goredis.NewClient(&goredis.Options{Addr: "localhost:6379"})
	rdb := redis.New(client, redis.Config{})

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

## Examples

Runnable examples for every provider live in the [`examples/`](examples/) directory. Each example includes setup instructions (Docker commands to start the backend, seed data) and demonstrates both individual `Register` and struct-based `RegisterConfig` patterns.

| Example    | Provider        | Watch Strategy                           | File                                                         |
| ---------- | --------------- | ---------------------------------------- | ------------------------------------------------------------ |
| etcd       | etcd            | Single prefix watch (event-driven)       | [`examples/etcd/main.go`](examples/etcd/main.go)             |
| Redis      | Redis           | Batch `MGET` polling                     | [`examples/redis/main.go`](examples/redis/main.go)           |
| Vault      | HashiCorp Vault | Sequential poll (KV v2)                  | [`examples/vault/main.go`](examples/vault/main.go)           |
| MySQL      | MySQL           | Batch `SELECT ... WHERE IN` polling      | [`examples/mysql/main.go`](examples/mysql/main.go)           |
| PostgreSQL | PostgreSQL      | Batch `SELECT ... WHERE IN ($N)` polling | [`examples/postgresql/main.go`](examples/postgresql/main.go) |
| File       | Local file      | fsnotify / fsevents (JSON + YAML)        | [`examples/file/main.go`](examples/file/main.go)             |

**Running an example:**

```bash
# Start the backend (see the example file for Docker commands)
docker run -d --name redis -p 6379:6379 redis:8.2.6

# Run the example
go run examples/redis/main.go
```

All examples use the same config keys (`timeout`, `verbose`, `db/host`, `db/port`) so you can swap providers and keep the same application code.

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


**Array values** — the provider stores a JSON array; poya decodes it into your slice:

```go
tags := poya.NewDcValue([]string{"alpha", "beta"})
poya.Register(sdk, "tags", tags)

s := tags.Get() // returns []string
fmt.Println(s[0]) // "alpha"

// Works with any element type:
ports := poya.NewDcValue([]int{8080, 9090})
poya.Register(sdk, "ports", ports)

p := ports.Get() // returns []int
```

The provider value must be a JSON array (e.g. `["alpha","beta"]` or `[8080,9090]`). Any slice element type that `encoding/json` supports works.

**Duration values** — `time.Duration` is supported as a scalar type, parsed from strings like `"30s"`, `"1m"`, `"500ms"`:

```go
timeout := poya.NewDcValue(time.Duration(30 * time.Second))
poya.Register(sdk, "timeout", timeout)

// Provider value "1m30s" will be parsed to 90s
t := timeout.Get() // returns time.Duration
fmt.Println(t) // 30s (default)
```

The provider value must be a valid `time.Duration` string (supports standard Go duration formats).

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

A ready-made Grafana dashboard for these metrics lives at [`docs/grafana/poya-dashboard.json`](docs/grafana/poya-dashboard.json). Import it in Grafana (Dashboards → Import → Upload JSON) and pick your Prometheus data source. It charts watch event and error rates per key, update latency quantiles (p50/p95/p99), and the registered-key count.

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

Ready-made adapters wrap the popular logging libraries. Each `New` takes a fully-configured logger that you own:

```go
import (
	poyazap "github.com/PapaDanielVi/poya/logger/zap"
	poyalogrus "github.com/PapaDanielVi/poya/logger/logrus"
	poyazerolog "github.com/PapaDanielVi/poya/logger/zerolog"
)

// zap
zl, _ := zap.NewProduction()
sdk := poya.New(poya.Config{Provider: rdb, Logger: poyazap.New(zl)})

// logrus
sdk := poya.New(poya.Config{Provider: rdb, Logger: poyalogrus.New(logrus.New())})

// zerolog
sdk := poya.New(poya.Config{Provider: rdb, Logger: poyazerolog.New(zerolog.New(os.Stderr))})
```

To silence poya entirely, pass `logger.NewNoop()`.

### Disabling dynamic config

Set `Config.Disabled` to turn poya off without removing it from your code. `Start()` becomes a no-op: the SDK never connects to a provider, never watches anything, and every registered `DcValue` keeps its default. A provider isn't required in this mode, so you can wire the flag from an environment variable or config file and ship the same binary with dynamic config on or off.

```go
sdk := poya.New(poya.Config{
	Provider: rdb,            // ignored while disabled; may be nil
	Disabled: os.Getenv("DYNAMIC_CONFIG") != "on",
})
poya.Register(sdk, "timeout", timeout)
sdk.Start() // no-op when disabled; timeout.Get() returns its default
```

### Loading initial values from config files (Viper, koanf, ...)

A common pattern is to seed `DcValue` defaults from a static config file (YAML, TOML, env) at startup, then let a provider take over for live updates. Most Go config loaders decode into structs through [mapstructure](https://github.com/go-viper/mapstructure), and poya ships decode hooks that turn decoded values into properly-typed `DcValue[T]` instances.

There are two mapstructure forks in the wild, so there are two hook packages with the same API:

| Package | mapstructure import | Loaders |
| --- | --- | --- |
| `github.com/PapaDanielVi/poya/hooks` | `github.com/mitchellh/mapstructure` (v1) | viper < 1.18, anything on the original mapstructure |
| `github.com/PapaDanielVi/poya/hooks/mapstructurev2` | `github.com/go-viper/mapstructure/v2` | koanf v2, viper >= 1.18, OpenTelemetry Collector |

Both packages handle every `DcValue` kind: scalars (parsed from strings for env/flat config), `time.Duration`, `time.Time` (RFC3339), any type whose pointer implements `encoding.TextUnmarshaler`, structs (from nested maps), and slices.

**koanf v2:**

```go
import (
	"github.com/PapaDanielVi/poya"
	"github.com/PapaDanielVi/poya/hooks/mapstructurev2"
	"github.com/knadh/koanf/v2"
	"github.com/go-viper/mapstructure/v2"
)

type AppConfig struct {
	Host    *poya.DcValue[string]        `koanf:"host" poya:"key=host"`
	Port    *poya.DcValue[int]           `koanf:"port" poya:"key=port"`
	Timeout *poya.DcValue[time.Duration] `koanf:"timeout" poya:"key=timeout"`
}

var cfg AppConfig
err := k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{
	Tag: "koanf",
	DecoderConfig: &mapstructure.DecoderConfig{
		DecodeHook: mapstructurev2.HookFunc(),
		Result:     &cfg,
	},
})
// cfg now holds the file values as DcValue defaults.
poya.RegisterConfig(sdk, &cfg)
sdk.Start() // provider values override the file defaults as they change.
```

**viper >= 1.18:**

```go
import "github.com/PapaDanielVi/poya/hooks/mapstructurev2"

err := viper.Unmarshal(&cfg, viper.DecodeHook(mapstructurev2.HookFunc()))
poya.RegisterConfig(sdk, &cfg)
```

**viper < 1.18 (or any tool on mitchellh/mapstructure):**

```go
import "github.com/PapaDanielVi/poya/hooks"

decoder, _ := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
	DecodeHook: hooks.MapstructureHookFunc(),
	Result:     &cfg,
})
err := decoder.Decode(viper.AllSettings())
```

Both packages also expose `StringToDcValueHookFunc()` (string sources only, for env-style config) and `JSONStringHookFunc()` (a struct or slice stored as a single JSON string), which compose with other hooks via `mapstructure.ComposeDecodeHookFunc`.

Tag-based loaders that write struct fields directly without a decode hook (for example `caarlos0/env`) have no place to plug a hook in, so they aren't supported through this mechanism. Use one of the mapstructure-based loaders above to seed `DcValue` defaults.

## Provider Setup

Every network provider takes a fully-configured client that you build and own. The provider never creates or hides the client, so you control endpoints, TLS, auth, pooling, and timeouts directly through each library's own config. The provider reconnects automatically with exponential backoff if the watch is dropped.

### etcd

Uses etcd's native Watch API for event-driven updates (no polling). Pass a configured `*clientv3.Client`:

```go
import clientv3 "go.etcd.io/etcd/client/v3"

cli, err := clientv3.New(clientv3.Config{
	Endpoints:   []string{"localhost:2379"},
	DialTimeout: 5 * time.Second,
})
if err != nil {
	log.Fatal(err)
}
etcdProvider := etcd.New(cli)
defer etcdProvider.Close()

sdk := poya.New(poya.Config{Provider: etcdProvider, Prefix: "myapp/"})
```

### Redis

Watches every key under their common prefix with a single keyspace-notification
subscription, so changes arrive as events. The provider enables keyspace
notifications on the server itself and reads the database number from the client.
Set `ResyncInterval` to also re-read all keys on a timer as a safety net against
missed notifications. Pass a configured `*redis.Client`:

```go
import goredis "github.com/redis/go-redis/v9"

client := goredis.NewClient(&goredis.Options{
	Addr:     "localhost:6379",
	Password: "", // no auth
	DB:       0,
})
rdb := redis.New(client, redis.Config{
	ResyncInterval: 0, // optional; 0 means event-driven only
})
defer rdb.Close()

sdk := poya.New(poya.Config{Provider: rdb, Prefix: "myapp/"})
```

### HashiCorp Vault

Stores the whole config in one KV v2 secret at the keys' common prefix, one field
per key, so a single read per poll cycle covers every key. With `Prefix: "myapp/"`
the secret lives at `<mount>/myapp` with fields named after each key. Pass a
configured `*vault.Client` (set the token on the client yourself):

```go
import vaultapi "github.com/hashicorp/vault/api"

vaultCfg := vaultapi.DefaultConfig()
vaultCfg.Address = "http://localhost:8200"
client, err := vaultapi.NewClient(vaultCfg)
if err != nil {
	log.Fatal(err)
}
client.SetToken("root-token")

v, err := vault.New(client, vault.Config{
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

### File

Watches a local JSON or YAML file for changes using fsnotify (fsevents on macOS, inotify on Linux). On every change the file is re-read and all registered values are updated via compare-and-swap. Supports flat `key: value` format (not nested):

```go
fp, err := file.New(file.Config{
	Path: "/etc/myapp/config.json",
	// Format: file.FormatAuto, // auto-detects from extension
})
if err != nil {
	log.Fatal(err)
}

sdk := poya.New(poya.Config{Provider: fp, Prefix: "myapp/"})
```

**JSON file format** (`config.json`):
```json
{
	"timeout": "30s",
	"verbose": true,
	"max_conn": 100
}
```

**YAML file format** (`config.yaml`):
```yaml
timeout: 30s
verbose: true
max_conn: 100
```

Format is auto-detected from the file extension (`.json`, `.yaml`, `.yml`) or can be set explicitly via `Config.Format`.

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
├── hooks/
│   ├── hooks.go               # mapstructure v1 decode hooks (viper < 1.18)
│   ├── internal/dchook/       # shared, mapstructure-agnostic decode logic
│   └── mapstructurev2/        # go-viper/mapstructure/v2 hooks (koanf, viper >= 1.18)
├── docs/grafana/              # Grafana dashboard JSON for the Prometheus metrics
├── metrics/
│   ├── metrics.go             # Metrics interface + NoopMetrics stub
│   ├── prometheus/            # Prometheus implementation
│   └── otel/                  # OpenTelemetry implementation
├── logger/
│   ├── logger.go              # Logger interface + slog default + noop stub
│   ├── zap/                   # zap adapter
│   ├── logrus/                # logrus adapter
│   └── zerolog/               # zerolog adapter
├── provider/
│   ├── provider.go            # Provider interface
│   ├── backoff.go             # Shared exponential backoff for reconnects
│   ├── etcd/                  # etcd provider (prefix watch API)
│   ├── redis/                 # Redis provider (keyspace notifications)
│   ├── vault/                 # HashiCorp Vault provider (KV v2 polling)
│   ├── mysql/                 # MySQL provider (batch polling, Repository interface)
│   ├── postgresql/            # PostgreSQL provider (batch polling, Repository interface)
│   └── file/                  # File provider (fsnotify / fsevents, JSON + YAML)
└── ...
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on adding providers, value types, and submitting pull requests.

## Keywords

Go, Golang, SDK, dynamic config, runtime configuration, configuration management, feature flags, A/B testing, service discovery, etcd, Redis, HashiCorp Vault, MySQL, PostgreSQL, file config, fsnotify, fsevents, type-safe config, generic config, viper, koanf, mapstructure, decode hook, Go SDK

## License

[MIT](LICENSE)
