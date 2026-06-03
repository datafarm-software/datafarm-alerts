package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"reflect"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/gorilla/mux"

	"github.com/danielgtaylor/huma/v2/adapters/humamux"
	"github.com/danielgtaylor/huma/v2/humacli"
	"github.com/geraud22/datafarm-alerts/alertsource"
	"github.com/geraud22/datafarm-alerts/alertstore"
	"github.com/geraud22/datafarm-alerts/auth"
	"github.com/geraud22/datafarm-alerts/notifier"
	"github.com/geraud22/datafarm-alerts/templates"
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
	Templater   templates.Templater
}

func Start(opts Opts) error {
	var err error
	app := &App{}
	app.Notifier, err = notifier.NewGomail(alertstore.Production, opts.Gomail)
	if err != nil {
		return fmt.Errorf("init notifier: %v", err)
	}
	app.AlertStore, err = alertstore.NewGorm(alertstore.Production, "alertstore/alerts")
	if err != nil {
		return fmt.Errorf("init alertstore: %v", err)
	}
	if err = app.AlertStore.PrepareDb(alertstore.Schema{}); err != nil {
		return fmt.Errorf("prepare alertstore: %v", err)
	}
	app.AlertSource, err = alertsource.NewInflux(opts.Influx)
	if err != nil {
		return fmt.Errorf("init alertsource: %v", err)
	}
	gohtmlOpts := templates.GohtmlOpts{Files: os.DirFS("templates")}
	app.Templater, err = templates.NewGohtml(gohtmlOpts)
	if err != nil {
		return fmt.Errorf("init templater: %v", err)
	}
	cli := humacli.New(func(hooks humacli.Hooks, options *Opts) {
		router := mux.NewRouter()
		config := huma.DefaultConfig("SensorData Alerts API", "1.0.0")
		api := humamux.New(router, config)
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
			app.Close()
			server.Shutdown(context.Background())
		})
	})
	cli.Run()
	return nil
}

func (a *App) Close() error {
	if err := a.Notifier.Close(); err != nil {
		log.Printf("closing notifier: %v", err)
	}
	if err := a.AlertSource.Close(); err != nil {
		log.Printf("closing alertsource: %v", err)
	}
	if err := a.AlertStore.Close(); err != nil {
		log.Printf("closing alertstore: %v", err)
	}
	log.Printf("Successfully closed app interfaces.")
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
		Tags:    []string{"Post"},
		Summary: "Post an alert.",
		Hidden:  false,
	}
	huma.Register(api, operation, a.RespondToAlert)
	operation = huma.Operation{
		Method:      "POST",
		Path:        "/alert",
		Tags:        []string{"Post"},
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
					},
				},
			},
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
						Schema: huma.SchemaFromType(registry, reflect.TypeFor[HumaError]()),
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
						Schema: huma.SchemaFromType(registry, reflect.TypeFor[HumaError]()),
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
	operation = huma.Operation{
		Method: "PUT",
		Path:   "/alert/{alertId}/emailaddresses",
		Parameters: []*huma.Param{
			{
				Name:            "alertId",
				In:              "path",
				Description:     "Alert Id to add email addresses to.",
				Required:        true,
				AllowEmptyValue: false,
				Schema: &huma.Schema{
					Type:    "string",
					Pattern: "^[a-zA-Z0-9]+$",
				},
			},
		},
		Tags:        []string{"Update"},
		Summary:     "Update an Alert Rule's User List.",
		Description: "Clients can use this route to add more users to the AlertRule.",
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
							Detail: "Internal error while updating the email addresses.",
						},
					},
				},
			},
			"404": {
				Description: "Not Found",
				Content: map[string]*huma.MediaType{
					"application/json": {
						Schema: huma.SchemaFromType(registry, reflect.TypeFor[HumaError]()),
						Example: HumaError{
							Schema: "http://localhost:3030/schemas/ErrorModel.json",
							Title:  "Not Found",
							Status: http.StatusNotFound,
							Detail: "AlertId Not Found.",
						},
					},
				},
			},
		},
	}
	huma.Register(api, operation, a.AddUsersToAlertRule)
	operation = huma.Operation{
		Method: "PATCH",
		Path:   "/alert/{alertId}/emailaddresses",
		Parameters: []*huma.Param{
			{
				Name:            "alertId",
				In:              "path",
				Description:     "Alert Id from which to remove email addresses.",
				Required:        true,
				AllowEmptyValue: false,
				Schema: &huma.Schema{
					Type:    "string",
					Pattern: "^[a-zA-Z0-9]+$",
				},
			},
		},
		Tags:        []string{"Patch"},
		Summary:     "Remove Users from Alert Rule.",
		Description: "Clients can use this route to remove users from the AlertRule.",
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
							Status: http.StatusInternalServerError,
							Detail: "Internal error while removing the email addresses.",
						},
					},
				},
			},
			"404": {
				Description: "Not Found",
				Content: map[string]*huma.MediaType{
					"application/json": {
						Schema: huma.SchemaFromType(registry, reflect.TypeFor[HumaError]()),
						Example: HumaError{
							Schema: "http://localhost:3030/schemas/ErrorModel.json",
							Title:  "Not Found",
							Status: http.StatusNotFound,
							Detail: "AlertId Not Found.",
						},
					},
				},
			},
			"204": {
				Description: "No Content will be returned in the event of the client removing all remaining users from the Alert Rule. In this case the sytem will also remove the Alert Rule.",
				Content: map[string]*huma.MediaType{
					"application/json": {
						Schema: huma.SchemaFromType(registry, reflect.TypeFor[HumaError]()),
						Example: HumaError{
							Schema: "http://localhost:3030/schemas/ErrorModel.json",
							Title:  "Not Found",
							Status: http.StatusNotFound,
							Detail: "AlertId Not Found.",
						},
					},
				},
			},
		},
	}
	huma.Register(api, operation, a.RemoveUsersFromAlertRule)
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
	shouldNotify := a.ShouldNotify(alert, previousAlert)
	if shouldNotify || alert.Level != previousAlert.Level || alert.Level == alertstore.Ok {
		//NOTE: not intended to update state with every alert that comes in, for the sake of comparing durations between alerts sent, with some edge cases around that like the first alert coming in etc.
		if err = a.AlertStore.UpdateAlertState(alert); err != nil {
			log.Printf("updating alert state for id: %s, err: %v", alert.Id, err)
			return nil, huma.Error500InternalServerError("Internal error updating alert state.")
		}
	}
	if !shouldNotify {
		return nil, nil
	}
	if alertRule.LinearCalibration != nil {
		val := alertRule.LinearCalibration.ToCalibrated(alert.Value)
		alert.Value = val.Round64(alertstore.TwoDecimalPlaces)
		calRange := make([]float32, 0, len(alertRule.RangeValues))
		for _, r := range alertRule.RangeValues {
			calRange = append(calRange, alertRule.LinearCalibration.
				ToCalibrated(float64(r)).Round32(alertstore.TwoDecimalPlaces))
		}
		alertRule.RangeValues = calRange
	}
	emails, err := a.AlertStore.GetAlertRecipients(alert.Id)
	if err != nil {
		return nil, huma.Error500InternalServerError("Internal error getting alert recipients.")
	}
	message := notifier.AlertNotification{
		Name:        alertRule.Name,
		Value:       alert.Value,
		DeviceId:    alertRule.DeviceId,
		QueryField:  alertRule.QueryField,
		AlertLevel:  alert.Level,
		Timestamp:   alert.Time,
		AlertValues: alertRule.RangeValues,
		Duration:    alertRule.Duration,
		AlertType:   alertRule.AlertType,
	}
	users := make([]auth.UserInfo, 0, len(emails))
	for _, e := range emails {
		users = append(users, auth.UserInfo{EmailAddress: e})
	}
	if err = a.Notifier.Notify(message, a.Templater, users...); err != nil {
		log.Printf("notifying users: %v", err)
		return nil, huma.Error500InternalServerError("Internal error notifying users.")
	}
	return nil, nil
}

func (a *App) ShouldNotify(alert, previous alertstore.AlertState) bool {
	if alert.Level > previous.Level {
		return true
	}
	emptyTime := time.Time{}
	if alert.Time.Equal(emptyTime) {
		return false
	}
	if previous.Time.Equal(emptyTime) {
		return false
	}
	if alert.Time.Sub(previous.Time) >= alert.Duration {
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
	alertCreatedMessageValues := in.Body.RangeValues
	if in.Body.LinearCalibration != nil {
		if !in.Body.LinearCalibration.IsEmpty() {
			newRange := make([]float32, 0, len(in.Body.RangeValues))
			var v alertstore.ValueRounder
			for _, val := range in.Body.RangeValues {
				v = in.Body.LinearCalibration.ToRaw(float64(val))
				newRange = append(newRange, v.F32())
			}
			in.Body.RangeValues = newRange
		}
	}
	existingAlertId, err := a.AlertStore.CheckAlertRuleExists(in.Body)
	if err != nil {
		if !errors.Is(err, alertstore.AlertExists) {
			log.Printf("checking alert exists: %v", err)
			return nil,
				huma.Error500InternalServerError("Internal error checking if alert exists.")
		}
		existingAlert, err := a.AlertStore.GetAlertRule(existingAlertId)
		if err != nil {
			log.Printf("getting existing alert rule: %v", err)
			return nil, huma.Error500InternalServerError(
				"Internal error while getting existing alert rule.")
		}
		slices.Sort(in.Body.EmailAddresses)
		slices.Sort(existingAlert.EmailAddresses)
		if slices.Equal(in.Body.EmailAddresses, existingAlert.EmailAddresses) {
			return nil, ErrorResponse{
				StatusError: huma.Error409Conflict(
					"Alert with identical parameters already exists."),
				AlertId: existingAlertId,
			}
		}
		if _, err = a.AddUsersToAlertRule(ctx, &struct {
			AlertId string   `path:"alertId"`
			Body    []string `doc:"List of user email addresses." format:"email"`
		}{
			existingAlertId, in.Body.EmailAddresses,
		}); err != nil {
			return nil, huma.Error500InternalServerError(
				"Internal error adding users to alert.")
		}
		return &struct{ Body alertstore.CreatedRule }{
			Body: alertstore.CreatedRule{
				AlertId: existingAlertId,
			},
		}, nil
	}
	alertId, err := a.AlertSource.CreateAlertRule(in.Body)
	if err != nil {
		log.Printf("registering alert: %v", err)
		return nil, huma.Error500InternalServerError("Internal error while registering the alert.")
	}
	uniqueEmails := removeDuplicates(in.Body.EmailAddresses)
	recipients := a.ConvertToRecipients(alertId, uniqueEmails)
	stateDuration, err := alertstore.ParseDuration(in.Body.Duration)
	if err != nil {
		log.Printf("parseDuration: %s, error: %v", in.Body.Duration, err)
		return nil, huma.Error400BadRequest(
			"Could not parse the provided rule duration.")
	}
	dbRecord := alertstore.Schema{
		AlertStates: []alertstore.AlertState{
			{
				Id:          alertId,
				Duration:    stateDuration,
				RangeValues: in.Body.RangeValues,
				Level:       in.Body.AlertLevel,
			},
		},
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
				Name:        in.Body.Name,
				RangeValues: in.Body.RangeValues,
				Duration:    in.Body.Duration,
				AlertType:   in.Body.AlertType,
				AlertLevel:  in.Body.AlertLevel,
			},
		},
	}
	if in.Body.LinearCalibration != nil {
		if !in.Body.LinearCalibration.IsEmpty() {
			dbRecord.LinearCalibrations = append(dbRecord.LinearCalibrations,
				alertstore.StoredLinearCalibration{
					AlertId: alertId, LinearCalibration: in.Body.LinearCalibration})
		}
	}
	if err := a.AlertStore.Store(dbRecord); err != nil {
		log.Printf("storing alert: %v", err)
		return nil, huma.Error500InternalServerError("internal error storing the alert rule")
	}
	alertCreated := notifier.AlertCreated{
		Name:        in.Body.Name,
		DeviceId:    in.Body.DeviceId,
		QueryField:  in.Body.QueryField,
		Duration:    in.Body.Duration,
		AlertLevel:  in.Body.AlertLevel,
		AlertType:   in.Body.AlertType,
		AlertValues: alertCreatedMessageValues,
	}
	alertCreated.LinearCalibration = in.Body.LinearCalibration
	users := make([]auth.UserInfo, 0, len(uniqueEmails))
	for _, e := range uniqueEmails {
		users = append(users, auth.UserInfo{EmailAddress: e})
	}
	if err := a.Notifier.Notify(alertCreated, a.Templater, users...); err != nil {
		log.Printf("alert created notify: %v", err)
		return nil, huma.Error500InternalServerError(
			"Internal error notifying users of alert creation.")
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
	alertCreatedMessageValues := in.Body.RangeValues
	if in.Body.LinearCalibration != nil {
		if !in.Body.LinearCalibration.IsEmpty() {
			newRange := make([]float32, 0, len(in.Body.RangeValues))
			var v alertstore.ValueRounder
			for _, val := range in.Body.RangeValues {
				v = in.Body.LinearCalibration.ToRaw(float64(val))
				newRange = append(newRange, v.F32())
			}
			in.Body.RangeValues = newRange
		}
	}
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
			"Not allowed to update DeviceId. Use Create route instead.")
	}
	if existingAlert.QueryField != in.Body.QueryField {
		return nil, huma.Error404NotFound(
			"Not allowed to update QueryField. Use Create route instead.")
	}
	if existingAlert.AlertType == alertstore.InactiveSensor {
		if in.Body.AlertType != alertstore.InactiveSensor {
			return nil, huma.Error400BadRequest(
				"Cannot update an existing Inactive Sensor check, to Threshold check.")
		}
	}
	if in.Body.AlertType == alertstore.InactiveSensor {
		if existingAlert.AlertType != alertstore.InactiveSensor {
			return nil, huma.Error400BadRequest(
				"Cannot update an existing alert to an Inactive Sensor check type.")
		}
	}
	if diff := cmp.Diff(existingAlert, in.Body); diff == "" {
		existingAlert.RangeValues = alertCreatedMessageValues
		return &struct{ Body alertstore.AlertRule }{existingAlert}, nil
	}
	var dbRecord alertstore.Schema
	uniqueEmails := removeDuplicates(in.Body.EmailAddresses)
	dbRecord.AlertRecipients = a.ConvertToRecipients(in.AlertId, uniqueEmails)
	dbRecord.DeviceAlerts = []alertstore.DeviceAlertRule{
		{
			DeviceId:   in.Body.DeviceId,
			QueryField: in.Body.QueryField,
			AlertId:    in.Body.AlertId,
		},
	}
	dbRecord.AlertRules = []alertstore.StoredAlertRule{
		{
			AlertId:     in.Body.AlertId,
			Name:        in.Body.Name,
			RangeValues: in.Body.RangeValues,
			Duration:    in.Body.Duration,
			AlertType:   in.Body.AlertType,
			AlertLevel:  in.Body.AlertLevel,
		},
	}
	if in.Body.LinearCalibration != nil {
		if !in.Body.LinearCalibration.IsEmpty() {
			dbRecord.LinearCalibrations = append(dbRecord.LinearCalibrations,
				alertstore.StoredLinearCalibration{
					AlertId:           in.Body.AlertId,
					LinearCalibration: in.Body.LinearCalibration,
				})
		}
	}
	if err := a.AlertStore.UpdateAlertRule(in.Body.AlertId, dbRecord); err != nil {
		log.Printf("alertstore update alert rule for id: %s, error: %v", in.AlertId, err)
		return nil, huma.Error500InternalServerError(
			"Internal error while storing the updated alert rule.")
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
		Level:       in.Body.AlertLevel,
	}
	if err = a.AlertStore.UpdateAlertState(alertState); err != nil {
		log.Printf("updating alert state for id: %s, err: %v", alertState.Id, err)
		return nil, huma.Error500InternalServerError("Internal error updating alert state.")
	}
	message := notifier.AlertUpdated{
		Name:        in.Body.Name,
		DeviceId:    in.Body.DeviceId,
		QueryField:  in.Body.QueryField,
		Duration:    in.Body.Duration,
		AlertLevel:  in.Body.AlertLevel,
		AlertType:   in.Body.AlertType,
		AlertValues: alertCreatedMessageValues,
	}
	if in.Body.LinearCalibration != nil {
		if !in.Body.LinearCalibration.IsEmpty() {
			message.LinearCalibration = in.Body.LinearCalibration
		}
	}
	users := make([]auth.UserInfo, 0, len(uniqueEmails))
	for _, e := range uniqueEmails {
		users = append(users, auth.UserInfo{EmailAddress: e})
	}
	if err = a.Notifier.Notify(message, a.Templater, users...); err != nil {
		log.Printf("update alert notify: %v", err)
		return nil, huma.Error500InternalServerError(
			"Internal error while notifying users of updated alert rule.")
	}
	in.Body.RangeValues = alertCreatedMessageValues
	return &struct{ Body alertstore.AlertRule }{in.Body}, nil
}

func removeDuplicates[T comparable](slice []T) []T {
	seen := make(map[T]struct{})
	result := []T{}
	for _, v := range slice {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			result = append(result, v)
		}
	}
	return result
}

func (a *App) ConvertToRecipients(alertId string, emails []string) []alertstore.AlertRecipient {
	recipients := make([]alertstore.AlertRecipient, 0, len(emails))
	for _, email := range emails {
		recipients = append(recipients, alertstore.AlertRecipient{
			AlertId: alertId,
			Email:   email,
		})
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
				"Internal error retrieving alert.")
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
	message := notifier.AlertDeleted{
		Name:        alertRule.Name,
		DeviceId:    alertRule.DeviceId,
		QueryField:  alertRule.QueryField,
		Duration:    alertRule.Duration,
		AlertLevel:  alertRule.AlertLevel,
		AlertType:   alertRule.AlertType,
		AlertValues: alertRule.RangeValues,
	}
	if alertRule.LinearCalibration != nil {
		message.LinearCalibration = alertRule.LinearCalibration
	}
	users := make([]auth.UserInfo, 0, len(alertRule.EmailAddresses))
	for _, e := range alertRule.EmailAddresses {
		users = append(users, auth.UserInfo{EmailAddress: e})
	}
	if err = a.Notifier.Notify(message, a.Templater, users...); err != nil {
		log.Printf("alertDeletedNotify: %v", err)
		return nil, huma.Error500InternalServerError(
			"Internal error notifying users alert is deleted.")
	}
	return &struct{ Body alertstore.AlertRule }{alertRule}, nil
}

func (a *App) AddUsersToAlertRule(ctx context.Context, in *struct {
	AlertId string   `path:"alertId"`
	Body    []string `doc:"List of user email addresses." format:"email"`
}) (*struct {
	Body alertstore.MutateAlertUsersResponse
}, error) {
	ar, err := a.AlertStore.GetAlertRule(in.AlertId)
	if err != nil {
		if errors.Is(err, alertstore.NoAlert) {
			return nil, huma.Error404NotFound("AlertId Not Found.")
		} else {
			return nil, huma.Error500InternalServerError(
				"Internal error checking if AlertId exists.")
		}
	}
	var dbRecord alertstore.Schema
	uniqueEmails := removeDuplicates(in.Body)
	dbRecord.AlertRecipients = a.ConvertToRecipients(in.AlertId, uniqueEmails)
	dbRecord.DeviceAlerts = []alertstore.DeviceAlertRule{
		{
			DeviceId:   ar.DeviceId,
			QueryField: ar.QueryField,
			AlertId:    ar.AlertId,
		},
	}
	dbRecord.AlertRules = []alertstore.StoredAlertRule{
		{
			AlertId:     ar.AlertId,
			Name:        ar.Name,
			RangeValues: ar.RangeValues,
			Duration:    ar.Duration,
			AlertType:   ar.AlertType,
			AlertLevel:  ar.AlertLevel,
		},
	}
	if err = a.AlertStore.UpdateAlertRule(ar.AlertId, dbRecord); err != nil {
		return nil, huma.Error500InternalServerError(
			"Internal error while updating alert rule.")
	}
	alertCreated := notifier.AlertCreated{
		Name:        ar.Name,
		DeviceId:    ar.DeviceId,
		QueryField:  ar.QueryField,
		Duration:    ar.Duration,
		AlertLevel:  ar.AlertLevel,
		AlertType:   ar.AlertType,
		AlertValues: ar.RangeValues,
	}
	users := make([]auth.UserInfo, 0, len(uniqueEmails))
	for _, e := range uniqueEmails {
		users = append(users, auth.UserInfo{EmailAddress: e})
	}
	if err := a.Notifier.Notify(alertCreated, a.Templater, users...); err != nil {
		log.Printf("alert created notify: %v", err)
		return nil, huma.Error500InternalServerError(
			"Internal error notifying users of alert registration.")
	}
	return &struct {
		Body alertstore.MutateAlertUsersResponse
	}{
		Body: alertstore.MutateAlertUsersResponse{
			AlertId: ar.AlertId, EmailAddresses: in.Body,
		},
	}, nil
}

func (a *App) RemoveUsersFromAlertRule(ctx context.Context, in *struct {
	AlertId string   `path:"alertId"`
	Body    []string `doc:"List of user email addresses." format:"email"`
}) (*struct {
	Status int
	Body   alertstore.MutateAlertUsersResponse
}, error) {
	ar, err := a.AlertStore.GetAlertRule(in.AlertId)
	if err != nil {
		if errors.Is(err, alertstore.NoAlert) {
			return nil, huma.Error404NotFound("AlertId Not Found.")
		} else {
			return nil, huma.Error500InternalServerError(
				"Internal error checking if AlertId exists.")
		}
	}
	emailsToDelete := make([]string, 0, len(in.Body))
	for _, email := range in.Body {
		if slices.Contains(ar.EmailAddresses, email) {
			emailsToDelete = append(emailsToDelete, email)
		}
	}
	emailsToDelete = removeDuplicates(emailsToDelete)
	shouldDeleteAlertRule := (len(ar.EmailAddresses) == len(emailsToDelete))
	if shouldDeleteAlertRule {
		if _, err := a.DeleteAlertRule(ctx, &struct {
			AlertId string `path:"alertId"`
		}{in.AlertId}); err != nil {
			return nil, huma.Error500InternalServerError(
				"Internal error deleting alert rule.")
		}
		return &struct {
			Status int
			Body   alertstore.MutateAlertUsersResponse
		}{Status: http.StatusNoContent}, nil
	}
	ars := a.ConvertToRecipients(in.AlertId, emailsToDelete)
	err = a.AlertStore.RemoveUsersFromAlertRule(ars)
	if err != nil {
		return nil, huma.Error500InternalServerError(
			"Internal error removing users from alert rule.")
	}
	message := notifier.AlertDeleted{
		Name:        ar.Name,
		DeviceId:    ar.DeviceId,
		QueryField:  ar.QueryField,
		Duration:    ar.Duration,
		AlertLevel:  ar.AlertLevel,
		AlertType:   ar.AlertType,
		AlertValues: ar.RangeValues,
	}
	users := make([]auth.UserInfo, 0, len(emailsToDelete))
	for _, e := range emailsToDelete {
		users = append(users, auth.UserInfo{EmailAddress: e})
	}
	if len(users) > 0 {
		if err = a.Notifier.Notify(message, a.Templater, users...); err != nil {
			log.Printf("remove users from alert notify: %v", err)
			return nil, huma.Error500InternalServerError(
				"Internal error notifying users unregistered from alert.")
		}
	}
	return &struct {
		Status int
		Body   alertstore.MutateAlertUsersResponse
	}{Status: http.StatusOK, Body: alertstore.MutateAlertUsersResponse{
		AlertId:        in.AlertId,
		EmailAddresses: in.Body,
	}}, nil
}
