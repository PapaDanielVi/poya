// Example: PostgreSQL provider
//
// Prerequisites:
//   1. Start PostgreSQL:
//      docker run -d --name postgres -p 5432:5432 -e POSTGRES_PASSWORD=root -e POSTGRES_DB=configdb postgres:16
//   2. Create the config table and seed data:
//      docker exec -i postgres psql -U postgres -d configdb <<'SQL'
//      CREATE TABLE config (
//          config_key   VARCHAR(255) PRIMARY KEY,
//          config_value TEXT
//      );
//      INSERT INTO config VALUES ('myapp/db/host', 'localhost');
//      INSERT INTO config VALUES ('myapp/db/port', '5432');
//      INSERT INTO config VALUES ('myapp/verbose', 'true');
//      INSERT INTO config VALUES ('myapp/timeout', '30s');
//      SQL
//   3. Run: go run examples/postgresql/main.go
//   4. In another terminal, update a value:
//      docker exec -i postgres psql -U postgres -d configdb -c "UPDATE config SET config_value='3306' WHERE config_key='myapp/db/port'"
//      Watch the output — the update is reflected within the poll interval.
package main

import (
	"database/sql"
	"log/slog"
	"os"
	"time"

	"github.com/PapaDanielVi/poya"
	"github.com/PapaDanielVi/poya/provider/postgresql"
	_ "github.com/lib/pq"
)

const (
	pollInterval     = 5 * time.Second
	defaultDBPort    = 5432
	defaultDBHost    = "localhost"
	defaultDBVerbose = false
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	db, err := sql.Open("postgres", "postgres://postgres:root@localhost:5432/configdb?sslmode=disable")
	if err != nil {
		log.Error("failed to open postgres connection", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	provider, err := postgresql.New(postgresql.Config{
		DB:           db,
		TableName:    "config",
		KeyColumn:    "config_key",
		ValueColumn:  "config_value",
		PollInterval: pollInterval,
	})
	if err != nil {
		log.Error("failed to create postgresql provider", "error", err)
		os.Exit(1)
	}

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

	log.Info("Polling PostgreSQL every 5s — UPDATE config values to see changes reflected.")
	for {
		log.Info("current config",
			"timeout", timeout.Get(),
			"verbose", verbose.Get(),
			"db.host", cfg.Host.Get(),
			"db.port", cfg.Port.Get())
		time.Sleep(pollInterval)
	}
}
