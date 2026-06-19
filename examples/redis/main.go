// Example: Redis provider.
//
// Models the runtime config of a checkout service: log level, request timeout,
// a rate limit, a maintenance feature flag, a database connection struct, and a
// list of allowed CORS origins. The Redis provider watches every key under
// "checkout/" with a single keyspace-notification subscription, so updates
// arrive as events rather than by polling.
//
// Run it end to end:
//
//	docker compose up -d
//	docker compose exec redis redis-cli SET checkout/log_level info
//	docker compose exec redis redis-cli SET checkout/request_timeout 30s
//	docker compose exec redis redis-cli SET checkout/rate_limit_rps 100
//	docker compose exec redis redis-cli SET checkout/maintenance_mode false
//	docker compose exec redis redis-cli SET checkout/database '{"host":"db.internal","port":5432,"max_open_conns":20}'
//	docker compose exec redis redis-cli SET checkout/allowed_origins '["https://app.example.com"]'
//	go run main.go
//
// Then change a value and watch the app log the event:
//
//	docker compose exec redis redis-cli SET checkout/rate_limit_rps 500
//	docker compose exec redis redis-cli SET checkout/maintenance_mode true
package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/PapaDanielVi/poya"
	"github.com/PapaDanielVi/poya/provider/redis"
)

const (
	reportInterval = 2 * time.Second
	// syncSettle gives the background watcher a moment to load current values
	// from the backend before we snapshot the initial config.
	syncSettle = time.Second
)

// DatabaseConfig is a struct-typed config value decoded from a JSON document.
type DatabaseConfig struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	MaxOpenConns int    `json:"max_open_conns"`
}

// CheckoutConfig is the full runtime config, registered in one shot via RegisterConfig.
type CheckoutConfig struct {
	LogLevel        poya.DcValue[string]         `poya:"key=log_level"`
	RequestTimeout  poya.DcValue[time.Duration]  `poya:"key=request_timeout"`
	RateLimitRPS    poya.DcValue[int]            `poya:"key=rate_limit_rps"`
	MaintenanceMode poya.DcValue[bool]           `poya:"key=maintenance_mode"`
	Database        poya.DcValue[DatabaseConfig] `poya:"key=database"`
	AllowedOrigins  poya.DcValue[[]string]       `poya:"key=allowed_origins"`
}

func newConfig() *CheckoutConfig {
	return &CheckoutConfig{
		LogLevel:        *poya.NewDcValue("info"),
		RequestTimeout:  *poya.NewDcValue(30 * time.Second),
		RateLimitRPS:    *poya.NewDcValue(100),
		MaintenanceMode: *poya.NewDcValue(false),
		Database:        *poya.NewDcValue(DatabaseConfig{Host: "localhost", Port: 5432, MaxOpenConns: 20}),
		AllowedOrigins:  *poya.NewDcValue([]string{"https://app.example.com"}),
	}
}

// describe renders the current config as one "key=value" line per field so the
// loop can diff successive snapshots and log only what changed.
func describe(cfg *CheckoutConfig) []string {
	db := cfg.Database.Get()
	return []string{
		"log_level=" + cfg.LogLevel.Get(),
		"request_timeout=" + cfg.RequestTimeout.Get().String(),
		fmt.Sprintf("rate_limit_rps=%d", cfg.RateLimitRPS.Get()),
		fmt.Sprintf("maintenance_mode=%v", cfg.MaintenanceMode.Get()),
		fmt.Sprintf("database=%s:%d(max_open_conns=%d)", db.Host, db.Port, db.MaxOpenConns),
		fmt.Sprintf("allowed_origins=%v", cfg.AllowedOrigins.Get()),
	}
}

// watchForChanges logs every field that changes between report intervals. The
// changes are how you confirm the app actually received a provider update.
func watchForChanges(log *slog.Logger, cfg *CheckoutConfig) {
	time.Sleep(syncSettle)
	prev := describe(cfg)
	log.Info("initial config", "values", prev)
	for {
		time.Sleep(reportInterval)
		cur := describe(cfg)
		for i := range cur {
			if cur[i] != prev[i] {
				log.Info("config changed", "value", cur[i])
			}
		}
		prev = cur
	}
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	prov := redis.New(redis.Config{
		Addr: "localhost:6379",
	})
	defer prov.Close() //nolint:errcheck,nolintlint

	sdk := poya.New(poya.Config{
		Provider: prov,
		Prefix:   "checkout/",
	})

	cfg := newConfig()
	poya.RegisterConfig(sdk, cfg)

	sdk.Start()
	defer sdk.Stop()

	log.Info("watching Redis under checkout/ — SET a key with redis-cli to see the event")
	watchForChanges(log, cfg)
}
