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
	"fmt"
	"log"
	"time"

	"github.com/PapaDanielVi/poya"
	"github.com/PapaDanielVi/poya/provider/etcd"
)

func main() {
	provider, err := etcd.New(etcd.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer provider.Close()

	sdk := poya.New(poya.Config{
		Provider: provider,
		Prefix:   "myapp/",
	})

	// Register individual values
	timeout := poya.NewDcValue("30s")
	verbose := poya.NewDcValue(false)
	poya.Register(sdk, "timeout", timeout)
	poya.Register(sdk, "verbose", verbose)

	// Register via config struct
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

	fmt.Println("Watching etcd keys under myapp/ — change values with etcdctl to see updates.")
	for {
		fmt.Printf("  timeout=%s  verbose=%v  db.host=%s  db.port=%d\n",
			timeout.Get(), verbose.Get(), cfg.Host.Get(), cfg.Port.Get())
		time.Sleep(5 * time.Second)
	}
}
