// Example: Redis provider
//
// Prerequisites:
//   1. Start Redis: docker run -d --name redis -p 6379:6379 redis:7
//   2. Seed keys:
//      redis-cli SET myapp/db/host localhost
//      redis-cli SET myapp/db/port 5432
//      redis-cli SET myapp/verbose true
//      redis-cli SET myapp/timeout 30s
//   3. Run: go run examples/redis/main.go
//   4. In another terminal, change a value: redis-cli SET myapp/db/port 3306
//      Watch the output — the update is reflected within the poll interval.
package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/PapaDanielVi/poya"
	"github.com/PapaDanielVi/poya/provider/redis"
)

const (
	pollInterval     = 5 * time.Second
	defaultDBPort    = 5432
	defaultDBHost    = "localhost"
	defaultDBVerbose = false
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	provider := redis.New(redis.Config{
		Addr:         "localhost:6379",
		PollInterval: pollInterval,
	})
	defer provider.Close()

	sdk := poya.New(poya.Config{
		Provider: provider,
		Prefix:   "myapp/",
	})

	timeout := poya.NewDcValue("30s")
	verbose := poya.NewDcValue(defaultDBVerbose)
	poya.Register(sdk, "timeout", timeout)
	poya.Register(sdk, "verbose", verbose)

	type DBConfig struct {
		Host poya.DcValue[string] `poya:"key=host"`
		Port poya.DcValue[int]    `poya:"key=port"`
	}
	cfg := DBConfig{
		Host: *poya.NewDcValue(defaultDBHost),
		Port: *poya.NewDcValue(defaultDBPort),
	}
	poya.RegisterConfig(sdk, &cfg)

	sdk.Start()
	defer sdk.Stop()

	log.Info("Polling Redis every 5s — change values with redis-cli to see updates.")
	for {
		log.Info("current config",
			"timeout", timeout.Get(),
			"verbose", verbose.Get(),
			"db.host", cfg.Host.Get(),
			"db.port", cfg.Port.Get())
		time.Sleep(pollInterval)
	}
}
