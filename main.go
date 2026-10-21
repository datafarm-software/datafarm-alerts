package main

import (
	"bytes"
	"log"
	"os"

	"github.com/datafarm-software/datafarm-alerts/alerts"
	cfy "github.com/geraud22/config-from-yaml"
)

func main() {
	config, err := os.ReadFile("config.yml")
	if err != nil {
		log.Fatalf("error reading config file: %v", err)
	}
	opts, err := cfy.LoadConfig[alerts.Opts](bytes.NewReader(config), "yaml", nil)
	if err != nil {
		log.Fatalf("error loading config: %v", err)
	}
	if err := alerts.Start(opts); err != nil {
		log.Fatalf("init app: %v", err)
	}
}
