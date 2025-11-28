package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/apourchet/httpwrap"
	"github.com/geraud22/sensordata-alerts/app"
)

func main() {
	app := &app.App{}
	httpWrapper := httpwrap.NewStandardWrapper()
	http.Handle("/notify", httpWrapper.Wrap(app.RespondToAlert))
	go func() {
		if err := http.ListenAndServe("localhost:3030", nil); err != nil {
			log.Printf("server startup: %v", err)
		}
	}()
	log.Println("Server Started...")
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	<-sigChan
	log.Println("Exiting...")
}
