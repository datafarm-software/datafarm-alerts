package main

import (
	"log"
	"os"
	"regexp"

	"github.com/danielgtaylor/huma/v2/adapters/humamux"
	"github.com/datafarm-software/datafarm-alerts/alerts"
	localhuma "github.com/datafarm-software/datafarm-api/api/huma"
	"github.com/datafarm-software/telemetry"
	"github.com/datafarm-software/telemetry/logging"
	"github.com/datafarm-software/telemetry/metering"
	"github.com/datafarm-software/telemetry/tracing"
	"github.com/gorilla/mux"
	"gopkg.in/yaml.v3"
)

var FileNameRegex = regexp.MustCompile(`^[\w-_/.]+\.{1,2}[\w]{1,5}$`)

func main() {
	args := os.Args
	filename := "alerts/openapi.yaml"
	if len(args) > 1 {
		if len(args) != 3 || args[1] != "--filename" {
			log.Fatalf("Usage: go run main.go --filename someName.yml")
		}
		filename = args[2]
		if !FileNameRegex.MatchString(filename) {
			log.Fatalf("Filename failed regex: %s", FileNameRegex)
		}
	}
	router := mux.NewRouter()
	config := localhuma.Config(localhuma.Production)
	humaApi := humamux.New(router, config)
	a := &alerts.App{Telemetry: &telemetry.Telemetry{
		Meter:  metering.MockMeter(),
		Tracer: tracing.MockTracer(),
		Logger: logging.MockLogger(),
	}}
	a.RegisterHumaOperations(humaApi)
	doc := humaApi.OpenAPI()
	out, err := yaml.Marshal(doc)
	if err != nil {
		log.Fatalf("marshalling yaml: %v", err)
	}
	if err = os.WriteFile(filename, out, 0o644); err != nil {
		log.Fatalf("writing yaml: %v", err)
	}
}
