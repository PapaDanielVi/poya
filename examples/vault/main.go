// Example: HashiCorp Vault provider
//
// Prerequisites:
//   1. Start Vault in dev mode:
//      docker run -d --name vault -p 8200:8200 --cap-add=IPC_LOCK vault:1.13 server -dev -dev-root-token-id=root
//   2. Seed secrets (the key name is the secret path within the KV mount):
//      VAULT_ADDR=http://localhost:8200 VAULT_TOKEN=root vault kv put secret/myapp/db host=localhost port=5432
//      VAULT_ADDR=http://localhost:8200 VAULT_TOKEN=root vault kv put secret/myapp/verbose value=true
//      VAULT_ADDR=http://localhost:8200 VAULT_TOKEN=root vault kv put secret/myapp/timeout value=30s
//   3. Run: go run examples/vault/main.go
//   4. In another terminal, update a secret:
//      VAULT_ADDR=http://localhost:8200 VAULT_TOKEN=root vault kv put secret/myapp/db host=remote port=3306
//      Watch the output — the update is reflected within the poll interval.
package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/PapaDanielVi/poya"
	"github.com/PapaDanielVi/poya/provider/vault"
)

const (
	pollInterval    = 10 * time.Second
	displayInterval = 5 * time.Second
	defaultDBPort   = 5432
	defaultDBHost   = "localhost"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	provider, err := vault.New(vault.Config{
		Address:      "http://localhost:8200",
		Token:        "root",
		MountPath:    "secret",
		PollInterval: pollInterval,
	})
	if err != nil {
		log.Error("failed to create vault provider", "error", err)
		os.Exit(1)
	}

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
		Host: *poya.NewDcValue(defaultDBHost),
		Port: *poya.NewDcValue(defaultDBPort),
	}
	poya.RegisterConfig(sdk, &cfg)

	sdk.Start()
	defer sdk.Stop()

	log.Info("Polling Vault every 10s — update secrets to see changes reflected.")
	for {
		log.Info("current config",
			"timeout", timeout.Get(),
			"verbose", verbose.Get(),
			"db.host", cfg.Host.Get(),
			"db.port", cfg.Port.Get())
		time.Sleep(displayInterval)
	}
}
