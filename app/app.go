package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"reflect"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/danielgtaylor/huma/v2/humacli"
	"github.com/geraud22/sensordata-alerts/alertsource"
	"github.com/geraud22/sensordata-alerts/alertstore"
	"github.com/geraud22/sensordata-alerts/notifier"
)

type HttpOpts struct {
	Address string `mapstructure:"address"`
	Port    int    `mapstructure:"port" validate:"required"`
}

type Opts struct {
	Http HttpOpts `mapstructure:"http" validate:"required"`
}

type App struct {
	AlertSource alertsource.AlertSource
	AlertStore  alertstore.AlertStore
	Notifier    notifier.Notifier
}

func Start(opts Opts) error {
	app := &App{}
	app.Notifier = notifier.NewMockNotifier(nil)
	app.AlertStore = alertstore.NewMockAlertStore(nil)
	app.AlertSource = alertsource.NewMockAlertSource(nil)
	cli := humacli.New(func(hooks humacli.Hooks, options *Opts) {
		router := http.NewServeMux()
		config := huma.DefaultConfig("SensorData Alerts API", "1.0.0")
		api := humago.New(router, config)
		app.RegisterHumaOperations(api)
		server := &http.Server{
			Addr:    fmt.Sprintf("%s:%d", opts.Http.Address, opts.Http.Port),
			Handler: router,
		}
		hooks.OnStart(func() {
			log.Println("Server started on ", server.Addr)
			if err := server.ListenAndServe(); err != nil {
				if !errors.Is(err, http.ErrServerClosed) {
					log.Fatalf("HTTP server error: %v", err)
				}
			}
		})
		hooks.OnStop(func() {
			server.Shutdown(context.Background())
		})
	})
	cli.Run()
	return nil
}

type HumaError struct {
	Schema string `json:"$schema" doc:"Link to Object Model."`
	Title  string `json:"title" doc:"Name associated with error code."`
	Status int    `json:"status" doc:"Http Status Code."`
	Detail string `json:"detail" doc:"Human Readable explanation of what went wrong."`
}

type ErrorResponse struct {
	huma.StatusError `json:"statusError" doc:"A breakdown of the error that occurred."`
	AlertId          string `json:"alertId" readOnly:"true" required:"false" doc:"In the event of a conflict, the existing alertId will be returned."`
}

func (a *App) RegisterHumaOperations(api huma.API) {
	operation := huma.Operation{
		Method:  "POST",
		Path:    "/alert/notify",
		Tags:    []string{"Post Alerts"},
		Summary: "Post an alert.",
		Hidden:  true,
	}
	huma.Register(api, operation, a.RespondToAlert)
	registry := huma.NewMapRegistry("#/errors", huma.DefaultSchemaNamer)
	operation = huma.Operation{
		Method:      "POST",
		Path:        "/alert/create",
		Tags:        []string{"Create"},
		Summary:     "Create an alert rule.",
		Description: "Clients can use this route to setup a new sensordata check, which will alert them according to the provided AlertRule request body.",
		RequestBody: &huma.RequestBody{},
		Responses: map[string]*huma.Response{
			"500": {
				Description: "Internal Server Error",
				Content: map[string]*huma.MediaType{
					"application/json": {
						Schema: huma.SchemaFromType(registry, reflect.TypeOf(HumaError{})),
						Example: HumaError{
							Schema: "http://localhost:3030/schemas/ErrorModel.json",
							Title:  "Bad Request",
							Status: 400,
							Detail: "Internal error while registering the alert.",
						},
					}}},
			"400": {
				Description: "Bad Request",
				Content: map[string]*huma.MediaType{
					"application/json": {
						Schema: huma.SchemaFromType(registry, reflect.TypeOf(HumaError{})),
						Example: HumaError{
							Schema: "http://localhost:3030/schemas/ErrorModel.json",
							Title:  "Bad Request",
							Status: 400,
							Detail: "Need 2 range values for range alert.",
						},
					},
				},
			},
			"409": {
				Description: "Resource Conflict",
				Content: map[string]*huma.MediaType{
					"application/json": {
						Schema: huma.SchemaFromType(registry, reflect.TypeOf(ErrorResponse{})),
						Example: ErrorResponse{
							StatusError: huma.Error409Conflict(
								"Alert with identical parameters already exists."),
							AlertId: "alert1",
						},
					},
				},
			},
		},
	}
	huma.Register(api, operation, a.CreateAlertRule)
}

func (a *App) RespondToAlert(
	ctx context.Context,
	in *struct{ Body alertsource.InfluxAlert }) (*struct{}, error) {
	alert := in.Body.GetAlert()
	previous, err := a.AlertStore.GetPreviousAlert(alert.DeviceId, alert.QueryField)
	if err != nil {
		log.Printf(
			"getting previous alert for %s: %s, error: %v",
			alert.DeviceId, alert.QueryField, err,
		)
		return nil, huma.Error500InternalServerError("no alert configured")
	}
	alert.Duration = previous.Duration
	if !a.ShouldNotifyCrit(alert, previous) {
		return nil, nil
	}
	users, err := a.AlertStore.GetAlertRecipients(alert.Id)
	if err != nil {
		log.Printf("GetAlertRecipients: %v", err)
		return nil, nil
	}
	if err = a.Notifier.Notify(users, alert.Message); err != nil {
		log.Printf("notifying users: %v", err)
		return nil, nil
	}
	if err = a.AlertStore.Update(alert); err != nil {
		log.Printf("updating alertstore: %v", err)
		return nil, nil
	}
	return nil, nil
}
func (a *App) ShouldNotifyCrit(alert *alertsource.Alert, previous *alertsource.Alert) bool {
	if alert.Level == previous.Level {
		if alert.Level != alertsource.Crit {
			return false
		}
		if alert.Time.Sub(previous.Time) > alert.Duration {
			return true
		}
	}
	if alert.Level != alertsource.Crit {
		return false
	}
	if alert.Level > previous.Level {
		return true
	}
	if alert.Time.Sub(previous.Time) > alert.Duration {
		return true
	}
	return false
}

func (a *App) CreateAlertRule(
	ctx context.Context,
	in *struct {
		Body alertsource.AlertRule
	}) (*struct{ Body alertsource.RuleCreated }, error) {
	if in.Body.AlertRange < alertsource.Range && len(in.Body.RangeValues) > 1 {
		log.Printf("can't have more than one range value on greater or less than alert.")
		return nil,
			huma.Error400BadRequest("More than one range value on less than or greater alert.")
	}
	if in.Body.AlertRange == alertsource.Range && len(in.Body.RangeValues) != 2 {
		log.Printf("Need 2 range values for range alert.")
		return nil, huma.Error400BadRequest("Need 2 range values for range alert.")
	}
	if existingAlertId, err := a.AlertStore.CheckAlertExists(in.Body); err != nil {
		if !errors.Is(err, alertstore.AlertExists) {
			log.Printf("checking alert exists: %v", err)
			return nil,
				huma.Error500InternalServerError("Internal error checking if alert exists.")
		}
		return nil, ErrorResponse{
			StatusError: huma.Error409Conflict(
				"Alert with identical parameters already exists."),
			AlertId: existingAlertId,
		}
	}
	alertId, err := a.AlertSource.RegisterAlert(in.Body)
	if err != nil {
		log.Printf("registering alert: %v", err)
		return nil, huma.Error500InternalServerError("Internal error while registering the alert.")
	}
	in.Body.Id = alertId
	if err := a.AlertStore.StoreAlert(in.Body); err != nil {
		log.Printf("storing alert: %v", err)
		return nil, huma.Error500InternalServerError("internal error storing the alert rule")
	}
	if err := a.AlertStore.AddUsersToNotifyList(in.Body.Id, in.Body.Usernames); err != nil {
		log.Printf("add usernames to alert: %s notify list: %v", in.Body.Id, err)
		return nil, huma.Error500InternalServerError("internal error adding user to notify list.")
	}
	return &struct{ Body alertsource.RuleCreated }{
		Body: alertsource.RuleCreated{
			AlertId: alertId,
		},
	}, nil
}
