package main

import (
	"bytes"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/apourchet/httpwrap"
	cfy "github.com/geraud22/config-from-yaml"
	"github.com/geraud22/sensordata-alerts/app"
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
	app, err := app.New(opts)
	if err != nil {
		log.Fatalf("init app: %v", err)
	}
	httpWrapper := httpwrap.NewStandardWrapper()
	http.Handle("/notify", httpWrapper.Wrap(app.RespondToAlert))
	go func() {
		if err := http.ListenAndServe("localhost:3030", nil); err != nil {
			log.Printf("server startup: %v", err)
		}
	}()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	log.Println("Server Started...")
	<-sigChan
	log.Println("Exiting...")
}
