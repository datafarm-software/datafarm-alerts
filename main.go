package main

import (
	"log"
	"net/http"

	"github.com/apourchet/httpwrap"
	"github.com/geraud22/sensordata-alerts/app"
)

func main() {
	app := &app.App{}
	httpWrapper := httpwrap.NewStandardWrapper()
	http.Handle("/notify", httpWrapper.Wrap(app.RespondToAlert))
	if err := http.ListenAndServe("localhost:3030", nil); err != nil {
		log.Printf("while shutting down server: %v", err)
	}
}
