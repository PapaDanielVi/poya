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
	"fmt"
	"time"

	"github.com/PapaDanielVi/poya"
	"github.com/PapaDanielVi/poya/provider/redis"
)

func main() {
	provider := redis.New(redis.Config{
		Addr:         "localhost:6379",
		PollInterval: 5 * time.Second,
	})
	defer provider.Close()

	sdk := poya.New(poya.Config{
		Provider: provider,
		Prefix:   "myapp/",
	})

	timeout := poya.NewDcValue("30s")
	verbose := poya.NewDcValue(false)
	poya.Register(sdk, "timeout", timeout)
	poya.Register(sdk, "verbose", verbose)

	type DBConfig struct {
		Host poya.DcValue[string] `poya:"key=host"`
		Port poya.DcValue[int]    `poya:"key=port"`
	}
	cfg := DBConfig{
		Host: *poya.NewDcValue("localhost"),
		Port: *poya.NewDcValue(5432),
	}
	poya.RegisterConfig(sdk, &cfg)

	sdk.Start()
	defer sdk.Stop()

	fmt.Println("Polling Redis every 5s — change values with redis-cli to see updates.")
	for {
		fmt.Printf("  timeout=%s  verbose=%v  db.host=%s  db.port=%d\n",
			timeout.Get(), verbose.Get(), cfg.Host.Get(), cfg.Port.Get())
		time.Sleep(5 * time.Second)
	}
}
