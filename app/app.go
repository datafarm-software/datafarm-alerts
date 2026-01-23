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
	Http   HttpOpts               `mapstructure:"http" validate:"required"`
	Influx alertsource.InfluxOpts `mapstructure:"influx" validate:"required"`
	Gomail notifier.GomailOpts    `mapstructure:"gomail" validate:"required"`
}

type App struct {
	AlertSource alertsource.AlertSource
	AlertStore  alertstore.AlertStore
	Notifier    notifier.Notifier
}

func Start(opts Opts) error {
	var err error
	app := &App{}
	app.Notifier, err = notifier.NewGomail(opts.Gomail)
	if err != nil {
		return err
	}
	app.AlertStore, err = alertstore.NewGorm(alertstore.Production, "prod")
	if err != nil {
		return err
	}
	if err = app.AlertStore.PrepareDb(alertstore.Schema{}); err != nil {
		return err
	}
	app.AlertSource, err = alertsource.NewInflux(opts.Influx)
	if err != nil {
		return err
	}
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
		Path:        "/alert",
		Tags:        []string{"Create"},
		Summary:     "Create an alert rule.",
		Description: "Clients can use this route to setup a new sensordata check, which will alert them according to the provided AlertRule request body.",
		RequestBody: &huma.RequestBody{},
		Responses: map[string]*huma.Response{
			"500": {
				Description: "Internal Server Error",
				Content: map[string]*huma.MediaType{
					"application/json": {
						Schema: huma.SchemaFromType(registry, reflect.TypeFor[HumaError]()),
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
						Schema: huma.SchemaFromType(registry, reflect.TypeFor[HumaError]()),
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
						Schema: huma.SchemaFromType(registry, reflect.TypeFor[ErrorResponse]()),
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
		Path:   "/alert/{alertId}",
		Parameters: []*huma.Param{
			{
				Name:            "alertId",
				In:              "path",
				Description:     "Alert Id to get.",
				Required:        true,
				AllowEmptyValue: false,
				Schema: &huma.Schema{
					Type:    "string",
					Pattern: "^[a-zA-Z0-9]+$",
				},
			},
		},
		Tags:        []string{"Retrieve"},
		Summary:     "Retrieve an Alert Rule.",
		Description: "Clients can use this route to get an existing sensordata check in the form of an AlertRule.",
		RequestBody: &huma.RequestBody{},
		Responses: map[string]*huma.Response{
			"500": {
				Description: "Internal Server Error",
				Content: map[string]*huma.MediaType{
					"application/json": {
						Schema: huma.SchemaFromType(registry, reflect.TypeFor[HumaError]()),
						Example: HumaError{
							Schema: "http://localhost:3030/schemas/ErrorModel.json",
							Title:  "Internal Server Error",
							Status: 500,
							Detail: "Internal error while retrieving the alert.",
						},
					}}},
		},
	}
	huma.Register(api, operation, a.GetAlertRule)
	operation = huma.Operation{
		Method: "PUT",
		Path:   "/alert/{alertId}",
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
						Schema: huma.SchemaFromType(registry, reflect.TypeFor[HumaError]()),
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
						Schema: huma.SchemaFromType(registry, reflect.TypeFor[HumaError]()),
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
	operation = huma.Operation{
		Method: "DELETE",
		Path:   "/alert/{alertId}",
		Parameters: []*huma.Param{
			{
				Name:            "alertId",
				In:              "path",
				Description:     "Alert Id to be deleted.",
				Required:        true,
				AllowEmptyValue: false,
				Schema: &huma.Schema{
					Type:    "string",
					Pattern: "^[a-zA-Z0-9]+$",
				},
			},
		},
		Tags:        []string{"Delete"},
		Summary:     "Delete an alert rule.",
		Description: "Clients can use this route to delete a sensordata check. The deleted check will be returned in the response as an AlertRule.",
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
							Detail: "Internal error while deleting the alert.",
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
							Detail: "Provided AlertId is invalid.",
						},
					},
				},
			},
		},
	}
	huma.Register(api, operation, a.DeleteAlertRule)
}

func (a *App) RespondToAlert(
	ctx context.Context,
	in *struct{ Body alertsource.InfluxAlert }) (*struct{}, error) {
	source := alertsource.AlertFromSource(&in.Body)
	alert := source.AlertState()
	alertRule, err := a.AlertStore.GetAlertRule(alert.Id)
	if err != nil {
		if !errors.Is(err, alertstore.NoAlert) {
			log.Printf("getting alert rule for %s: %v", alert.Id, err)
			return nil, huma.Error500InternalServerError("Internal error getting the alert rule.")
		}
		log.Printf("Alert rule not found. %s", in.Body.Id)
		return nil, huma.Error404NotFound("Alert rule not found.")
	}
	if alertRule.QueryField != in.Body.QueryField || alertRule.DeviceId != in.Body.DeviceId {
		return nil, huma.Error404NotFound("Received alert with unknown DeviceId and QueryField combination.")
	}
	previousAlert, err := a.AlertStore.GetAlertState(alert.Id)
	if err != nil {
		if !errors.Is(err, alertstore.NoAlert) {
			log.Printf("getting previous alert for %s: %v", alert.Id, err)
			return nil, huma.Error500InternalServerError("Internal error getting previous alert state.")
		}
		//NOTE: if no previous state is found, assume first time alert.
		previousAlert.Level = alertstore.Ok
	}
	alert.Duration, err = alertstore.ParseDuration(alertRule.Duration)
	if err != nil {
		log.Printf("parsing duration for alert: %s, %s, err: %v", alert.Id, alertRule.Duration, err)
		return nil, huma.Error500InternalServerError("Internal error parsing the duration of the alert.")
	}
	if !a.ShouldNotifyCrit(alert, previousAlert) {
		return nil, nil
	}
	emails, err := a.AlertStore.GetAlertRecipients(alert.Id)
	if err != nil {
		log.Printf("GetAlertRecipients: %v", err)
		return nil, huma.Error500InternalServerError("Internal error getting alert recipients.")
	}
	message := notifier.AlertMessage{
		Name:       alert.Name,
		DeviceId:   alertRule.DeviceId,
		QueryField: alertRule.QueryField,
		Level:      alert.Level,
		Timestamp:  alert.Time,
	}
	if err = a.Notifier.Notify(message, emails...); err != nil {
		log.Printf("notifying users: %v", err)
		return nil, huma.Error500InternalServerError("Could not notify users.")
	}
	if err = a.AlertStore.UpdateAlertState(alert); err != nil {
		log.Printf("updating alert state for id: %s, err: %v", alert.Id, err)
		return nil, huma.Error500InternalServerError("Internal error updating alert state.")
	}
	return nil, nil
}

func (a *App) ShouldNotifyCrit(alert, previous alertstore.AlertState) bool {
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
	if in.Body.AlertType < alertstore.Range && len(in.Body.RangeValues) > 1 {
		log.Printf("can't have more than one range value on greater or less than alert.")
		return nil,
			huma.Error400BadRequest("More than one range value on less than or greater alert.")
	}
	if in.Body.AlertType == alertstore.Range && len(in.Body.RangeValues) != 2 {
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
	alertId, err := a.AlertSource.CreateAlertRule(in.Body)
	if err != nil {
		log.Printf("registering alert: %v", err)
		return nil, huma.Error500InternalServerError("Internal error while registering the alert.")
	}
	recipients := a.ConvertToRecipients(alertId, in.Body.EmailAddresses)
	dbRecord := alertstore.Schema{
		DeviceAlerts: []alertstore.DeviceAlertRule{
			{
				AlertId:    alertId,
				DeviceId:   in.Body.DeviceId,
				QueryField: in.Body.QueryField,
			},
		},
		AlertRecipients: recipients,
		AlertRules: []alertstore.StoredAlertRule{
			{
				AlertId:     alertId,
				RangeValues: in.Body.RangeValues,
				Duration:    in.Body.Duration,
				AlertType:   in.Body.AlertType,
			},
		},
	}
	if err := a.AlertStore.StoreAlertRule(dbRecord); err != nil {
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
	if in.Body.AlertType < alertstore.Range && len(in.Body.RangeValues) > 1 {
		log.Printf("can't have more than one range value on greater or less than alert.")
		return nil,
			huma.Error400BadRequest("More than one range value on less than or greater alert.")
	}
	if in.Body.AlertType == alertstore.Range && len(in.Body.RangeValues) != 2 {
		log.Printf("Need 2 range values for range alert.")
		return nil, huma.Error400BadRequest("Need 2 range values for range alert.")
	}
	in.Body.AlertId = in.AlertId
	existingAlert, err := a.AlertStore.GetAlertRule(in.AlertId)
	if err != nil {
		if errors.Is(err, alertstore.NoAlert) {
			return nil, huma.Error404NotFound("Alert not found.")
		}
		log.Printf("unexpected error while getting alert rule for: %s, error: %v", in.AlertId, err)
		return nil, huma.Error500InternalServerError(
			"Internal error while getting the alert.")
	}
	if existingAlert.DeviceId != in.Body.DeviceId {
		return nil, huma.Error404NotFound(
			"Not allowed to update DeviceId. Recommended to use Create route instead.")
	}
	if existingAlert.QueryField != in.Body.QueryField {
		return nil, huma.Error404NotFound(
			"Not allowed to update QueryField. Recommended to use Create route instead.")
	}
	recipients := a.ConvertToRecipients(in.AlertId, in.Body.EmailAddresses)
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
				AlertType:   in.Body.AlertType,
			},
		},
	}
	if err := a.AlertStore.UpdateAlertRule(in.Body.AlertId, dbRecord); err != nil {
		log.Printf("alertstore update alert rule for id: %s, error: %v", in.AlertId, err)
		return nil, huma.Error500InternalServerError(
			"Internal error while storing the updated alert rule.")
	}
	stateDuration, err := alertstore.ParseDuration(in.Body.Duration)
	if err != nil {
		log.Printf("parseDuration: %s, error: %v", in.Body.Duration, err)
		return nil, huma.Error400BadRequest(
			"Could not parse the provided rule duration.")
	}
	alertState := alertstore.AlertState{
		Duration:    stateDuration,
		RangeValues: in.Body.RangeValues,
		Id:          in.Body.AlertId,
	}
	if err = a.AlertStore.UpdateAlertState(alertState); err != nil {
		log.Printf("updating alert state for id: %s, err: %v", alertState.Id, err)
		return nil, huma.Error500InternalServerError("Internal error updating alert state.")
	}
	if err := a.AlertSource.UpdateAlertRule(in.Body); err != nil {
		if errors.Is(err, alertstore.NoAlert) {
			return nil, huma.Error404NotFound(
				"Could not update rule with alertsource. Alert not found.")
		}
		log.Printf("alertsource update alert rule for id: %s, error: %v", in.AlertId, err)
		return nil, huma.Error500InternalServerError(
			"Internal error while updating the alert rule with source.")
	}
	return &struct{ Body alertstore.AlertRule }{in.Body}, nil
}

func (a *App) ConvertToRecipients(alertId string, emails []string) []alertstore.AlertRecipient {
	recipients := make([]alertstore.AlertRecipient, 0, len(emails))
	emailExists := make(map[string]bool, len(emails))
	for _, email := range emails {
		if !emailExists[email] {
			recipients = append(recipients, alertstore.AlertRecipient{
				AlertId: alertId,
				Email:   email,
			})
			emailExists[email] = true
		}
	}
	return recipients
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

func (a *App) DeleteAlertRule(ctx context.Context,
	in *struct {
		AlertId string `path:"alertId"`
	},
) (*struct {
	Body alertstore.AlertRule
}, error) {
	if err := a.AlertStore.DeleteAlertState(in.AlertId); err != nil {
		if errors.Is(err, alertstore.NoAlert) {
			return nil, huma.Error404NotFound("Alert State with given id does not exist.")
		}
		log.Printf("deleting alert state: %s: %v", in.AlertId, err)
		return nil, huma.Error500InternalServerError("Internal error while deleting alert state.")
	}
	alertRule, err := a.AlertStore.DeleteAlertRule(in.AlertId)
	if err != nil {
		if errors.Is(err, alertstore.NoAlert) {
			return nil, huma.Error404NotFound("Alert Rule with given id does not exist.")
		}
		log.Printf("deleting alert rule: %s: %v", in.AlertId, err)
		return nil, huma.Error500InternalServerError("Internal error while deleting alert rule.")
	}
	if err = a.AlertSource.DeleteAlertRule(in.AlertId); err != nil {
		log.Printf("deleting source alert rule: %s: %v", in.AlertId, err)
		return nil, huma.Error500InternalServerError("Internal error while deleting the alert rule from source.")
	}
	return &struct{ Body alertstore.AlertRule }{alertRule}, nil
}
