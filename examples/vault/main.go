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
	"fmt"
	"log"
	"time"

	"github.com/PapaDanielVi/poya"
	"github.com/PapaDanielVi/poya/provider/vault"
)

func main() {
	provider, err := vault.New(vault.Config{
		Address:      "http://localhost:8200",
		Token:        "root",
		MountPath:    "secret",
		PollInterval: 10 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
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
		Host: *poya.NewDcValue("localhost"),
		Port: *poya.NewDcValue(5432),
	}
	poya.RegisterConfig(sdk, &cfg)

	sdk.Start()
	defer sdk.Stop()

	fmt.Println("Polling Vault every 10s — update secrets to see changes reflected.")
	for {
		fmt.Printf("  timeout=%s  verbose=%v  db.host=%s  db.port=%d\n",
			timeout.Get(), verbose.Get(), cfg.Host.Get(), cfg.Port.Get())
		time.Sleep(5 * time.Second)
	}
}
