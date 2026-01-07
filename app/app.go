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
	registry := huma.NewMapRegistry("#/errors", huma.DefaultSchemaNamer)
	operation := huma.Operation{
		Method:  "POST",
		Path:    "/alert/notify",
		Tags:    []string{"Post Alerts"},
		Summary: "Post an alert.",
		Hidden:  true,
	}
	huma.Register(api, operation, a.RespondToAlert)
	operation = huma.Operation{
		Method:      "POST",
		Path:        "/alerts",
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
							Title:  "Internal Server Error",
							Status: 500,
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
	operation = huma.Operation{
		Method: "GET",
		Path:   "/alerts/{alertId}",
	}
	huma.Register(api, operation, a.GetAlertRule)
	operation = huma.Operation{
		Method: "PUT",
		Path:   "/alerts/{alertId}",
		Parameters: []*huma.Param{
			{
				Name:            "alertId",
				In:              "path",
				Description:     "Alert Id to be updated.",
				Required:        true,
				AllowEmptyValue: false,
				Schema: &huma.Schema{
					Type:    "string",
					Pattern: "^[a-zA-Z0-9]+$",
				},
			},
		},
		Tags:        []string{"Update"},
		Summary:     "Update an alert rule.",
		Description: "Clients can use this route to update an existing sensordata check, which will alert them according to the provided AlertRule request body.",
		RequestBody: &huma.RequestBody{},
		Responses: map[string]*huma.Response{
			"500": {
				Description: "Internal Server Error",
				Content: map[string]*huma.MediaType{
					"application/json": {
						Schema: huma.SchemaFromType(registry, reflect.TypeOf(HumaError{})),
						Example: HumaError{
							Schema: "http://localhost:3030/schemas/ErrorModel.json",
							Title:  "Internal Server Error",
							Status: 500,
							Detail: "Internal error while retrieving the alert.",
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
							Detail: "Something went wrong.",
						},
					},
				},
			},
		},
	}
	huma.Register(api, operation, a.UpdateAlertRule)
}

func (a *App) RespondToAlert(
	ctx context.Context,
	in *struct{ Body alertsource.InfluxAlert }) (*struct{}, error) {
	source := alertsource.AlertSource(&in.Body)
	alert := source.GetAlertState()
	previousAlert, err := a.AlertStore.GetAlertState(alert.Id)
	if err != nil {
		log.Printf("getting previous alert for %s: %v", alert.Id, err)
		return nil, huma.Error500InternalServerError("no alert configured")
	}
	alertRule, err := a.AlertStore.GetAlertRule(alert.Id)
	if err != nil {
		log.Printf("getting alert rule for %s: %v", alert.Id, err)
		return nil, huma.Error500InternalServerError("Internal error getting the alert rule.")
	}
	alert.Duration, err = alertstore.ParseDuration(alertRule.Duration)
	if err != nil {
		log.Printf("parsing duration for alert: %s, %s, err: %v", alert.Id, alertRule.Duration, err)
		return nil, huma.Error500InternalServerError("Internal error parsing the duration of the alert.")
	}
	if !a.ShouldNotifyCrit(alert, previousAlert) {
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
	return nil, nil
}
func (a *App) ShouldNotifyCrit(alert *alertstore.AlertState, previous *alertstore.AlertState) bool {
	if alert.Level != alertstore.Crit {
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
		Body alertstore.AlertRule
	}) (*struct{ Body alertstore.CreatedRule }, error) {
	if in.Body.AlertRange < alertstore.Range && len(in.Body.RangeValues) > 1 {
		log.Printf("can't have more than one range value on greater or less than alert.")
		return nil,
			huma.Error400BadRequest("More than one range value on less than or greater alert.")
	}
	if in.Body.AlertRange == alertstore.Range && len(in.Body.RangeValues) != 2 {
		log.Printf("Need 2 range values for range alert.")
		return nil, huma.Error400BadRequest("Need 2 range values for range alert.")
	}
	existingAlertId, err := a.AlertStore.CheckAlertRuleExists(in.Body)
	if err != nil {
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
	alertId, err := a.AlertSource.RegisterAlertRule(in.Body)
	if err != nil {
		log.Printf("registering alert: %v", err)
		return nil, huma.Error500InternalServerError("Internal error while registering the alert.")
	}
	alertRecipients := make([]alertstore.AlertRecipient, 0, len(in.Body.EmailAddresses))
	var ar alertstore.AlertRecipient
	for _, email := range in.Body.EmailAddresses {
		ar = alertstore.AlertRecipient{
			AlertId: alertId,
			Email:   email,
		}
		alertRecipients = append(alertRecipients, ar)
	}
	dbRecord := alertstore.Schema{
		DeviceAlerts: []alertstore.DeviceAlertRule{
			{
				AlertId:    alertId,
				DeviceId:   in.Body.DeviceId,
				QueryField: in.Body.QueryField,
			},
		},
		AlertRecipients: alertRecipients,
		AlertRules: []alertstore.StoredAlertRule{
			{
				AlertId:     alertId,
				RangeValues: in.Body.RangeValues,
				Duration:    in.Body.Duration,
				AlertRange:  in.Body.AlertRange,
			},
		},
	}
	if err := a.AlertStore.StoreAlertRule(alertId, dbRecord); err != nil {
		log.Printf("storing alert: %v", err)
		return nil, huma.Error500InternalServerError("internal error storing the alert rule")
	}
	return &struct{ Body alertstore.CreatedRule }{
		Body: alertstore.CreatedRule{
			AlertId: alertId,
		},
	}, nil
}

func (a *App) UpdateAlertRule(
	ctx context.Context,
	in *struct {
		AlertId string `path:"alertId"`
		Body    alertstore.AlertRule
	},
) (*struct{ Body alertstore.AlertRule }, error) {
	if in.Body.AlertRange < alertstore.Range && len(in.Body.RangeValues) > 1 {
		log.Printf("can't have more than one range value on greater or less than alert.")
		return nil,
			huma.Error400BadRequest("More than one range value on less than or greater alert.")
	}
	if in.Body.AlertRange == alertstore.Range && len(in.Body.RangeValues) != 2 {
		log.Printf("Need 2 range values for range alert.")
		return nil, huma.Error400BadRequest("Need 2 range values for range alert.")
	}
	in.Body.AlertId = in.AlertId
	existingAlertId, err := a.AlertStore.CheckAlertRuleExists(in.Body)
	if existingAlertId == "" && err == nil {
		log.Printf("tried to update an alert that does not exist.")
		return nil,
			huma.Error404NotFound("Tried to update an alert that does not exist.")
	}
	if err != nil && !errors.Is(err, alertstore.AlertExists) {
		log.Printf("checking alert exists: %v", err)
		return nil,
			huma.Error500InternalServerError("Internal error checking if alert exists.")
	}
	recipients := make([]alertstore.AlertRecipient, 0, len(in.Body.EmailAddresses))
	for _, email := range in.Body.EmailAddresses {
		recipients = append(recipients, alertstore.AlertRecipient{
			AlertId: in.AlertId,
			Email:   email,
		})
	}
	dbRecord := alertstore.Schema{
		DeviceAlerts: []alertstore.DeviceAlertRule{
			{
				DeviceId:   in.Body.DeviceId,
				QueryField: in.Body.QueryField,
				AlertId:    in.AlertId,
			},
		},
		AlertRecipients: recipients,
		AlertRules: []alertstore.StoredAlertRule{
			{
				AlertId:     in.AlertId,
				RangeValues: in.Body.RangeValues,
				Duration:    in.Body.Duration,
				AlertRange:  in.Body.AlertRange,
			},
		},
	}
	alertRule, err := a.AlertStore.StoreAlertRule(existingAlertId, dbRecord)
	if err != nil {
		log.Printf("updating alert rule for id: %s, error: %v", in.AlertId, err)
		return nil, huma.Error500InternalServerError(
			"Internal error while storing the updated alert rule.")
	}
	return &struct{ Body alertstore.AlertRule }{alertRule}, nil
}

func (a *App) GetAlertRule(
	ctx context.Context,
	in *struct {
		AlertId string `path:"alertId"`
	},
) (*struct {
	Body alertstore.AlertRule
}, error) {
	alert, err := a.AlertStore.GetAlertRule(in.AlertId)
	if err != nil {
		if !errors.Is(err, alertstore.NoAlert) {
			log.Printf("GetAlertRule(%s): %v", in.AlertId, err)
			return nil, huma.Error500InternalServerError(
				"Unexpected area retrieving alert.")
		}
		return nil, huma.Error404NotFound("No alert rule found.")
	}
	return &struct{ Body alertstore.AlertRule }{alert}, nil
}
