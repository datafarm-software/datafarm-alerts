package main

import (
	"bytes"
	"log"
	"os"

	cfy "github.com/geraud22/config-from-yaml"
	"github.com/geraud22/datafarm-alerts/app"
)

func main() {
	config, err := os.ReadFile("config.yml")
	if err != nil {
		log.Fatalf("error reading config file: %v", err)
	}
	opts, err := cfy.LoadConfig[app.Opts](bytes.NewReader(config), "yaml", nil)
	if err != nil {
		log.Fatalf("error loading config: %v", err)
	}
	if err := app.Start(opts); err != nil {
		log.Fatalf("init app: %v", err)
	}
}
