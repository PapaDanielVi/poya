// Example: File provider.
//
// Models the runtime config of a checkout service in a local JSON file: log
// level, request timeout, a rate limit, a maintenance feature flag, a database
// connection struct, and a list of allowed CORS origins. The file provider
// watches the file with fsnotify and re-reads it on every change. Nested objects
// and arrays are decoded into struct and array config values.
//
// Run it end to end:
//
//	docker compose up -d   # writes /tmp/checkout-config.json
//	go run main.go
//
// Then edit the file and watch the app log the event:
//
//	cat > /tmp/checkout-config.json <<'JSON'
//	{
//	  "log_level": "debug",
//	  "request_timeout": "45s",
//	  "rate_limit_rps": 500,
//	  "maintenance_mode": true,
//	  "database": {"host": "db.prod", "port": 6543, "max_open_conns": 50},
//	  "allowed_origins": ["https://app.example.com", "https://admin.example.com"]
//	}
//	JSON
package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/PapaDanielVi/poya"
	"github.com/PapaDanielVi/poya/provider/file"
)

const (
	reportInterval = 2 * time.Second
	configPath     = "/tmp/checkout-config.json"
	// syncSettle gives the background watcher a moment to load current values
	// from the backend before we snapshot the initial config.
	syncSettle = time.Second
)

// DatabaseConfig is a struct-typed config value decoded from a JSON object.
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
// changes are how you confirm the app actually received a file update.
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

	prov, err := file.New(file.Config{
		Path: configPath, // format auto-detected from the .json extension
	})
	if err != nil {
		log.Error("failed to create file provider", "error", err)
		os.Exit(1)
	}
	defer prov.Close() //nolint:errcheck,nolintlint

	sdk := poya.New(poya.Config{
		Provider: prov,
	})

	cfg := newConfig()
	poya.RegisterConfig(sdk, cfg)

	sdk.Start()
	defer sdk.Stop()

	log.Info("watching " + configPath + " — edit the file to see the event")
	watchForChanges(log, cfg)
}
