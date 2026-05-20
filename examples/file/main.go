// Example: File provider
//
// Prerequisites:
//   1. Create a config file (JSON or YAML) in the current directory:
//
//      echo '{"timeout":"30s","verbose":true,"max_conn":100}' > /tmp/config.json
//
//      // or for YAML:
//      cat > /tmp/config.yaml <<'YAML'
//      timeout: 30s
//      verbose: true
//      max_conn: 100
//      YAML
//
//   2. Update the path below to point to your file.
//   3. Run: go run examples/file/main.go
//   4. In another terminal, edit the file:
//      echo '{"timeout":"60s","verbose":false,"max_conn":200}' > /tmp/config.json
//      Watch the output — the update is reflected almost instantly.
package main

import (
	"log"
	"time"

	"github.com/PapaDanielVi/poya"
	"github.com/PapaDanielVi/poya/provider/file"
)

const pollInterval = 5 * time.Second

func main() {
	provider, err := file.New(file.Config{
		Path: "/tmp/config.json",
		// Format: file.FormatAuto, // auto-detects from .json / .yaml / .yml
	})
	if err != nil {
		log.Fatal(err)
	}

	sdk := poya.New(poya.Config{
		Provider: provider,
	})

	timeout := poya.NewDcValue("30s")
	verbose := poya.NewDcValue(false)
	maxConn := poya.NewDcValue(0)
	poya.Register(sdk, "timeout", timeout)
	poya.Register(sdk, "verbose", verbose)
	poya.Register(sdk, "max_conn", maxConn)

	sdk.Start()
	defer sdk.Stop()

	log.Println("Watching /tmp/config.json for changes — edit the file to see updates.")
	for {
		log.Printf("  timeout=%s  verbose=%v  max_conn=%d\n",
			timeout.Get(), verbose.Get(), maxConn.Get())
		time.Sleep(pollInterval)
	}
}
