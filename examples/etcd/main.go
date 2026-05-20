// Example: etcd provider
//
// Prerequisites:
//   1. Start etcd: docker run -d --name etcd -p 2379:2379 quay.io/coreos/etcd:v3.5.0 etcd --advertise-client-urls http://0.0.0.0:2379 --listen-client-urls http://0.0.0.0:2379
//   2. Seed keys:
//      etcdctl put myapp/db/host localhost
//      etcdctl put myapp/db/port 5432
//      etcdctl put myapp/verbose true
//      etcdctl put myapp/timeout 30s
//   3. Run: go run examples/etcd/main.go
//   4. In another terminal, change a value: etcdctl put myapp/db/port 3306
//      Watch the output — the update is reflected within seconds.
package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/PapaDanielVi/poya"
	"github.com/PapaDanielVi/poya/provider/etcd"
)

const (
	pollInterval     = 5 * time.Second
	dialTimeout      = 5 * time.Second
	defaultDBPort    = 5432
	defaultDBHost    = "localhost"
	defaultDBVerbose = false
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	provider, err := etcd.New(etcd.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: dialTimeout,
	})
	if err != nil {
		log.Error("failed to create etcd provider", "error", err)
		os.Exit(1)
	}
	defer provider.Close() //nolint:errcheck,nolintlint

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

	log.Info("Watching etcd keys under myapp/ — change values with etcdctl to see updates.")
	for {
		log.Info("current config",
			"timeout", timeout.Get(),
			"verbose", verbose.Get(),
			"db.host", cfg.Host.Get(),
			"db.port", cfg.Port.Get())
		time.Sleep(pollInterval)
	}
}
