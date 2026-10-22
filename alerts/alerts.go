package alerts

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/datafarm-software/telemetry"
	"github.com/datafarm-software/telemetry/logging"
	"github.com/datafarm-software/telemetry/metering"
	"github.com/datafarm-software/telemetry/middlewares"
	"github.com/gorilla/mux"

	"github.com/google/go-cmp/cmp"

	"github.com/danielgtaylor/huma/v2/adapters/humamux"
	"github.com/danielgtaylor/huma/v2/humacli"
	"github.com/datafarm-software/datafarm-alerts/alerts/auth"
	"github.com/datafarm-software/datafarm-alerts/alerts/notifier"
	"github.com/datafarm-software/datafarm-alerts/alerts/source"
	"github.com/datafarm-software/datafarm-alerts/alerts/store"
	"github.com/datafarm-software/datafarm-alerts/alerts/templates"
)

type HttpOpts struct {
	Address string `mapstructure:"address"`
	Port    int    `mapstructure:"port" validate:"required"`
}

type Opts struct {
	Http          HttpOpts            `mapstructure:"http" validate:"required"`
	Influx        source.InfluxOpts   `mapstructure:"influx" validate:"required"`
	Gomail        notifier.GomailOpts `mapstructure:"gomail" validate:"required"`
	TelemetryOpts telemetry.Opts      `mapstructure:"telemetry"`
}

type App struct {
	AlertSource source.AlertSource
	AlertStore  store.AlertStore
	Notifier    notifier.Notifier
	Templater   templates.Templater
	Telemetry   *telemetry.Telemetry
}

func Start(opts Opts) error {
	var err error
	app := &App{}
	app.Notifier, err = notifier.NewGomail(store.Production, opts.Gomail)
	if err != nil {
		return fmt.Errorf("init notifier: %v", err)
	}
	app.AlertStore, err = store.NewGorm(store.Production, "alerts/store/alerts")
	if err != nil {
		return fmt.Errorf("init store: %v", err)
	}
	if err = app.AlertStore.PrepareDb(store.Schema{}); err != nil {
		return fmt.Errorf("prepare store: %v", err)
	}
	app.AlertSource, err = source.NewInflux(opts.Influx)
	if err != nil {
		return fmt.Errorf("init source: %v", err)
	}
	gohtmlOpts := templates.GohtmlOpts{Files: os.DirFS("alerts/templates")}
	app.Templater, err = templates.NewGohtml(gohtmlOpts)
	if err != nil {
		return fmt.Errorf("init templater: %v", err)
	}
	opts.TelemetryOpts.MeterOpts = append(opts.TelemetryOpts.MeterOpts,
		metering.WithLatency(), metering.WithMemoryUsage(),
		metering.WithOtelRuntime(), metering.WithUptime())
	app.Telemetry, err = telemetry.NewTelemetry(opts.TelemetryOpts)
	if err != nil {
		return fmt.Errorf("init telemetry: %v", err)
	}

	cli := humacli.New(func(hooks humacli.Hooks, _ *struct{}) {
		router := mux.NewRouter()
		api := humamux.New(router, Config())
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

var versionRegex = regexp.MustCompile(`[\d]+.[\d]+.[\d]+`)

func findVersion(stdOut []byte) string {
	verStr := string(stdOut)
	return versionRegex.FindString(verStr)
}

func Config() (config huma.Config) {
	var cmd *exec.Cmd
	cmd = exec.Command("git", "describe", "--tags", "--abbrev=0")
	stdOut, _ := cmd.Output()
	verStr := findVersion(stdOut)
	if verStr == "" {
		cmd = exec.Command("cat", "/app/.git-version")
		stdOut, _ = cmd.Output()
		verStr = findVersion(stdOut)
	}
	if verStr == "" {
		// NOTE: if all else fails, fallback to v1
		verStr = "1"
	}
	config = huma.DefaultConfig("DataFarm Alerts API", verStr)
	config.Info.Description = `
## Welcome

The DataFarm Alerts API provides our clients with email notifications on their SensorData.

### Contributing

DataFarm welcomes external contribution to this API, through Open Source under the GPL-3.0 License. If you would like to contribute please visit the project's Github page to get started: www.github.com/datafarm-software/datafarm-alerts.
`
	return config
}

func (a *App) Close() error {
	if err := a.Notifier.Close(); err != nil {
		log.Printf("closing notifier: %v", err)
	}
	if err := a.AlertSource.Close(); err != nil {
		log.Printf("closing source: %v", err)
	}
	if err := a.AlertStore.Close(); err != nil {
		log.Printf("closing store: %v", err)
	}
	if err := a.Telemetry.Close(context.Background()); err != nil {
		log.Printf("closing telemetry: %v", err)
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
	mwProvider, _ := middlewares.NewHumaProvider(a.Telemetry)
	mws := middlewares.Middlewares[huma.Context]{
		mwProvider.TraceRequest, mwProvider.LogRequest, mwProvider.RecordLatency,
	}
	allMw := huma.Middlewares(mws)

	registry := huma.NewMapRegistry("#/errors", huma.DefaultSchemaNamer)
	operation := huma.Operation{
		Method:      "POST",
		Path:        "/alert/notify",
		Tags:        []string{"Post"},
		Summary:     "Post an alert.",
		Middlewares: allMw,
	}
	huma.Register(api, operation, a.RespondToAlert)
	operation = huma.Operation{
		Method:      "POST",
		Path:        "/alert",
		Middlewares: allMw,
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
					},
				},
			},
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
		Method:      "GET",
		Path:        "/alert/{alertId}",
		Middlewares: allMw,
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
			"400": {
				Description: "Bad Request",
				Content: map[string]*huma.MediaType{
					"application/json": {
						Schema: huma.SchemaFromType(registry, reflect.TypeFor[HumaError]()),
						Example: HumaError{
							Schema: "http://localhost:3030/schemas/ErrorModel.json",
							Title:  "Bad Request",
							Status: 400,
							Detail: "AlertId failed the regex.",
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
	huma.Register(api, operation, a.GetAlertRule)
	operation = huma.Operation{
		Method:      "PUT",
		Path:        "/alert/{alertId}",
		Middlewares: allMw,
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
			"400": {
				Description: "Bad Request",
				Content: map[string]*huma.MediaType{
					"application/json": {
						Schema: huma.SchemaFromType(registry, reflect.TypeFor[HumaError]()),
						Example: HumaError{
							Schema: "http://localhost:3030/schemas/ErrorModel.json",
							Title:  "Bad Request",
							Status: 400,
							Detail: "AlertId failed the regex.",
						},
					},
				},
			},
		},
	}
	huma.Register(api, operation, a.UpdateAlertRule)
	operation = huma.Operation{
		Method:      "DELETE",
		Path:        "/alert/{alertId}",
		Middlewares: allMw,
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
		Method:      "PUT",
		Path:        "/alert/{alertId}/emailaddresses",
		Middlewares: allMw,
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
			"400": {
				Description: "Bad Request",
				Content: map[string]*huma.MediaType{
					"application/json": {
						Schema: huma.SchemaFromType(registry, reflect.TypeFor[HumaError]()),
						Example: HumaError{
							Schema: "http://localhost:3030/schemas/ErrorModel.json",
							Title:  "Bad Request",
							Status: http.StatusBadRequest,
							Detail: "Contains Invalid Email Address.",
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
		Method:      "PATCH",
		Path:        "/alert/{alertId}/emailaddresses",
		Middlewares: allMw,
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
			"400": {
				Description: "Bad Request",
				Content: map[string]*huma.MediaType{
					"application/json": {
						Schema: huma.SchemaFromType(registry, reflect.TypeFor[HumaError]()),
						Example: HumaError{
							Schema: "http://localhost:3030/schemas/ErrorModel.json",
							Title:  "Bad Request",
							Status: http.StatusBadRequest,
							Detail: "Contains Invalid Email Address.",
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
				Content:     map[string]*huma.MediaType{},
			},
		},
	}
	huma.Register(api, operation, a.RemoveUsersFromAlertRule)
}

func (a *App) RespondToAlert(
	ctx context.Context,
	in *struct{ Body source.InfluxAlert },
) (_ *struct{}, err error) {
	log, ok := ctx.Value("request-log").(logging.LogAccumulator)
	if !ok {
		return nil, huma.Error500InternalServerError(
			"Internal error while getting request log.")
	}
	source := source.AlertFromSource(&in.Body)
	alert := source.AlertState()
	defer func() {
		if err != nil {
			log.Struct(alert)
		}
	}()
	alertRule, err := a.AlertStore.GetAlertRule(alert.Id)
	if err != nil {
		if !errors.Is(err, store.NoAlert) {
			log.AddMetadata(logging.Metadata{
				"source":        {"store.getAlertRule"},
				"error.message": {err.Error()},
			})
			return nil, huma.Error500InternalServerError(
				"Internal error getting the alert rule.")
		}
		return nil, huma.Error404NotFound("Alert rule not found.")
	}
	if alertRule.QueryField != in.Body.QueryField || alertRule.DeviceId != in.Body.DeviceId {
		return nil, huma.Error404NotFound(
			"Received alert with unknown DeviceId and QueryField combination.")
	}
	previousAlert, err := a.AlertStore.GetAlertState(alert.Id)
	if err != nil {
		if !errors.Is(err, store.NoAlert) {
			log.AddMetadata(logging.Metadata{
				"source":        {"store.getAlertState"},
				"error.message": {err.Error()},
			})
			return nil, huma.Error500InternalServerError(
				"Internal error getting previous alert state.")
		}
		// NOTE: if no previous state is found, assume first time alert.
		previousAlert.Level = store.Ok
	}
	alert.Duration, err = store.ParseDuration(alertRule.Duration)
	if err != nil {
		log.AddMetadata(logging.Metadata{
			"source":        {"store.parseDuration"},
			"error.message": {err.Error()},
		})
		return nil, huma.Error500InternalServerError(
			"Internal error parsing the duration of the alert.")
	}
	shouldNotify := a.ShouldNotify(alert, previousAlert)
	if shouldNotify || alert.Level != previousAlert.Level || alert.Level == store.Ok {
		// NOTE: not intended to update state with every alert that comes in, for the sake of comparing durations between alerts sent, with some edge cases around that like the first alert coming in etc.
		if err = a.AlertStore.UpdateAlertState(alert); err != nil {
			log.AddMetadata(logging.Metadata{
				"source":        {"store.updateAlertState"},
				"error.message": {err.Error()},
			})
			return nil, huma.Error500InternalServerError("Internal error updating alert state.")
		}
	}
	if !shouldNotify {
		return nil, nil
	}
	if alertRule.LinearCalibration != nil {
		val := alertRule.LinearCalibration.ToCalibrated(alert.Value)
		alert.Value = val.Round64(store.TwoDecimalPlaces)
		calRange := make([]float32, 0, len(alertRule.RangeValues))
		for _, r := range alertRule.RangeValues {
			calRange = append(calRange, alertRule.LinearCalibration.
				ToCalibrated(float64(r)).Round32(store.NearestOne))
		}
		alertRule.RangeValues = calRange
	}
	emails, err := a.AlertStore.GetAlertRecipients(alert.Id)
	if err != nil {
		log.AddMetadata(logging.Metadata{
			"source":        {"store.getAlertRecipients"},
			"error.message": {err.Error()},
		})
		return nil, huma.Error500InternalServerError(
			"Internal error getting alert recipients.")
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
		log.AddMetadata(logging.Metadata{
			"source":        {"notifier.Notify"},
			"error.message": {err.Error()},
		})
		return nil, huma.Error500InternalServerError("Internal error notifying users.")
	}
	return nil, nil
}

func (a *App) ShouldNotify(alert, previous store.AlertState) bool {
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
		Body store.AlertRule
	},
) (_ *struct{ Body store.CreatedRule }, err error) {
	log, ok := ctx.Value("request-log").(logging.LogAccumulator)
	if !ok {
		return nil, huma.Error500InternalServerError(
			"Internal error while getting request log.")
	}
	log.Struct(in.Body)
	if in.Body.AlertType < store.Range && len(in.Body.RangeValues) > 1 {
		return nil,
			huma.Error400BadRequest("More than one range value on less than or greater alert.")
	}
	if in.Body.AlertType == store.Range && len(in.Body.RangeValues) != 2 {
		return nil, huma.Error400BadRequest("Need 2 range values for range alert.")
	}
	alertCreatedMessageValues := in.Body.RangeValues
	if in.Body.LinearCalibration != nil {
		if !in.Body.LinearCalibration.IsEmpty() {
			in.Body.RangeValues = Calculate(in.Body.LinearCalibration.ToRaw, in.Body.RangeValues)
		}
	}
	existingAlertId, err := a.AlertStore.CheckAlertRuleExists(in.Body)
	if err != nil {
		if !errors.Is(err, store.AlertExists) {
			log.AddMetadata(logging.Metadata{
				"source":        {"store.checkAlertRuleExists"},
				"error.message": {err.Error()},
			})
			return nil,
				huma.Error500InternalServerError("Internal error checking if alert exists.")
		}
		existingAlert, err := a.AlertStore.GetAlertRule(existingAlertId)
		if err != nil {
			log.AddMetadata(logging.Metadata{
				"source":           {"store.getAlertRule"},
				"alertId.existing": {existingAlertId},
				"error.message":    {err.Error()},
			})
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
			AlertId string `path:"alertId"`
			Body    store.Emails
		}{
			existingAlertId, store.Emails{Addresses: in.Body.EmailAddresses},
		}); err != nil {
			return nil, huma.Error500InternalServerError(
				"Internal error adding users to alert.")
		}
		return &struct{ Body store.CreatedRule }{
			Body: store.CreatedRule{
				AlertId: existingAlertId,
			},
		}, nil
	}
	alertId, err := a.AlertSource.CreateAlertRule(in.Body)
	if err != nil {
		log.AddMetadata(logging.Metadata{
			"source":        {"source.createAlertRule"},
			"error.message": {err.Error()},
		})
		return nil, huma.Error500InternalServerError(
			"Internal error while registering the alert.")
	}
	uniqueEmails := removeDuplicates(in.Body.EmailAddresses)
	recipients := a.ConvertToRecipients(alertId, uniqueEmails)
	stateDuration, err := store.ParseDuration(in.Body.Duration)
	if err != nil {
		log.AddMetadata(logging.Metadata{
			"source":        {"store.parseDuration"},
			"error.message": {err.Error()},
		})
		return nil, huma.Error400BadRequest(
			"Could not parse the provided rule duration.")
	}
	dbRecord := store.Schema{
		AlertStates: []store.AlertState{
			{
				Id:          alertId,
				Duration:    stateDuration,
				RangeValues: in.Body.RangeValues,
				Level:       in.Body.AlertLevel,
			},
		},
		DeviceAlerts: []store.DeviceAlertRule{
			{
				AlertId:    alertId,
				DeviceId:   in.Body.DeviceId,
				QueryField: in.Body.QueryField,
			},
		},
		AlertRecipients: recipients,
		AlertRules: []store.StoredAlertRule{
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
				store.StoredLinearCalibration{
					AlertId: alertId, LinearCalibration: in.Body.LinearCalibration,
				})
		}
	}
	if err := a.AlertStore.Store(dbRecord); err != nil {
		log.AddMetadata(logging.Metadata{
			"source":        {"store.store"},
			"error.message": {err.Error()},
		})
		return nil, huma.Error500InternalServerError(
			"internal error storing the alert rule")
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
		log.AddMetadata(logging.Metadata{
			"source":        {"notifier.notify"},
			"error.message": {err.Error()},
		})
		return nil, huma.Error500InternalServerError(
			"Internal error notifying users of alert creation.")
	}
	return &struct{ Body store.CreatedRule }{
		Body: store.CreatedRule{
			AlertId: alertId,
		},
	}, nil
}

func Calculate(F func(float64) store.ValueRounder, rangeValues []float32) (newRange []float32) {
	newRange = make([]float32, 0, len(rangeValues))
	var v store.ValueRounder
	for _, val := range rangeValues {
		v64 := float64(val)
		v = F(v64)
		newRange = append(newRange, v.F32())
	}
	return
}

func (a *App) UpdateAlertRule(
	ctx context.Context,
	in *struct {
		AlertId string `path:"alertId"`
		Body    store.AlertRule
	},
) (*struct{ Body store.AlertRule }, error) {
	if in.Body.AlertType < store.Range && len(in.Body.RangeValues) > 1 {
		return nil,
			huma.Error400BadRequest("More than one range value on less than or greater alert.")
	}
	if in.Body.AlertType == store.Range && len(in.Body.RangeValues) != 2 {
		return nil, huma.Error400BadRequest("Need 2 range values for range alert.")
	}
	alertCreatedMessageValues := in.Body.RangeValues
	in.Body.AlertId = in.AlertId
	log, ok := ctx.Value("request-log").(logging.LogAccumulator)
	if !ok {
		return nil, huma.Error500InternalServerError(
			"Internal error while getting request log.")
	}
	log.Struct(in.Body)
	if in.Body.LinearCalibration != nil {
		if !in.Body.LinearCalibration.IsEmpty() {
			in.Body.RangeValues = Calculate(
				in.Body.LinearCalibration.ToRaw, in.Body.RangeValues)
		}
	}
	existingAlert, err := a.AlertStore.GetAlertRule(in.AlertId)
	if err != nil {
		if errors.Is(err, store.NoAlert) {
			return nil, huma.Error404NotFound("Alert not found.")
		}
		log.AddMetadata(logging.Metadata{
			"source":        {"store.getAlertRule"},
			"error.message": {err.Error()},
		})
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
	if existingAlert.AlertType == store.InactiveSensor {
		if in.Body.AlertType != store.InactiveSensor {
			return nil, huma.Error400BadRequest(
				"Cannot update an existing Inactive Sensor check, to Threshold check.")
		}
	}
	if in.Body.AlertType == store.InactiveSensor {
		if existingAlert.AlertType != store.InactiveSensor {
			return nil, huma.Error400BadRequest(
				"Cannot update an existing alert to an Inactive Sensor check type.")
		}
	}
	if diff := cmp.Diff(existingAlert, in.Body); diff == "" {
		existingAlert.RangeValues = alertCreatedMessageValues
		return &struct{ Body store.AlertRule }{existingAlert}, nil
	}
	var dbRecord store.Schema
	uniqueEmails := removeDuplicates(in.Body.EmailAddresses)
	dbRecord.AlertRecipients = a.ConvertToRecipients(in.AlertId, uniqueEmails)
	dbRecord.DeviceAlerts = []store.DeviceAlertRule{
		{
			DeviceId:   in.Body.DeviceId,
			QueryField: in.Body.QueryField,
			AlertId:    in.Body.AlertId,
		},
	}
	dbRecord.AlertRules = []store.StoredAlertRule{
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
				store.StoredLinearCalibration{
					AlertId:           in.Body.AlertId,
					LinearCalibration: in.Body.LinearCalibration,
				})
		}
	}
	if err := a.AlertStore.UpdateAlertRule(in.Body.AlertId, dbRecord); err != nil {
		log.AddMetadata(logging.Metadata{
			"source":        {"store.updateAlertRule"},
			"error.message": {err.Error()},
		})
		return nil, huma.Error500InternalServerError(
			"Internal error while storing the updated alert rule.")
	}
	if err := a.AlertSource.UpdateAlertRule(in.Body); err != nil {
		if errors.Is(err, store.NoAlert) {
			return nil, huma.Error404NotFound(
				"Could not update rule with source. Alert not found.")
		}
		log.AddMetadata(logging.Metadata{
			"source":        {"source.updateAlertRule"},
			"error.message": {err.Error()},
		})
		return nil, huma.Error500InternalServerError(
			"Internal error while updating the alert rule with source.")
	}
	stateDuration, err := store.ParseDuration(in.Body.Duration)
	if err != nil {
		log.AddMetadata(logging.Metadata{
			"source":        {"store.parseDuration"},
			"error.message": {err.Error()},
		})
		return nil, huma.Error400BadRequest(
			"Could not parse the provided rule duration.")
	}
	alertState := store.AlertState{
		Duration:    stateDuration,
		RangeValues: in.Body.RangeValues,
		Id:          in.Body.AlertId,
		Level:       in.Body.AlertLevel,
	}
	if err = a.AlertStore.UpdateAlertState(alertState); err != nil {
		log.AddMetadata(logging.Metadata{
			"source":        {"store.updateAlertState"},
			"error.message": {err.Error()},
		})
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
		log.AddMetadata(logging.Metadata{
			"source":        {"notifer.notify"},
			"error.message": {err.Error()},
		})
		return nil, huma.Error500InternalServerError(
			"Internal error while notifying users of updated alert rule.")
	}
	in.Body.RangeValues = alertCreatedMessageValues
	return &struct{ Body store.AlertRule }{in.Body}, nil
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

func (a *App) ConvertToRecipients(alertId string, emails []string) []store.AlertRecipient {
	recipients := make([]store.AlertRecipient, 0, len(emails))
	for _, email := range emails {
		recipients = append(recipients, store.AlertRecipient{
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
	Body store.AlertRule
}, error,
) {
	log, ok := ctx.Value("request-log").(logging.LogAccumulator)
	if !ok {
		return nil, huma.Error500InternalServerError(
			"Internal error while getting request log.")
	}
	alert, err := a.AlertStore.GetAlertRule(in.AlertId)
	if err != nil {
		if !errors.Is(err, store.NoAlert) {
			log.AddMetadata(logging.Metadata{
				"source":        {"store.getAlertRule"},
				"alertId":       {in.AlertId},
				"error.message": {err.Error()},
			})
			return nil, huma.Error500InternalServerError(
				"Internal error retrieving alert.")
		}
		return nil, huma.Error404NotFound("No alert rule found.")
	}
	return &struct{ Body store.AlertRule }{alert}, nil
}

func (a *App) DeleteAlertRule(ctx context.Context,
	in *struct {
		AlertId string `path:"alertId"`
	},
) (_ *struct {
	Body store.AlertRule
}, err error,
) {
	log, ok := ctx.Value("request-log").(logging.LogAccumulator)
	if !ok {
		return nil, huma.Error500InternalServerError(
			"Internal error while getting request log.")
	}
	if err := a.AlertStore.DeleteAlertState(in.AlertId); err != nil {
		if errors.Is(err, store.NoAlert) {
			return nil, huma.Error404NotFound("Alert State with given id does not exist.")
		}
		log.AddMetadata(logging.Metadata{
			"source":        {"store.deleteAlertState"},
			"alertId":       {in.AlertId},
			"error.message": {err.Error()},
		})
		return nil, huma.Error500InternalServerError(
			"Internal error while deleting alert state.")
	}
	alertRule, err := a.AlertStore.DeleteAlertRule(in.AlertId)
	if err != nil {
		if errors.Is(err, store.NoAlert) {
			return nil, huma.Error404NotFound("Alert Rule with given id does not exist.")
		}
		log.AddMetadata(logging.Metadata{
			"source":        {"store.deleteAlertRule"},
			"alertId":       {in.AlertId},
			"error.message": {err.Error()},
		})
		return nil, huma.Error500InternalServerError(
			"Internal error while deleting alert rule.")
	}
	if err = a.AlertSource.DeleteAlertRule(in.AlertId); err != nil {
		log.AddMetadata(logging.Metadata{
			"source":        {"source.deleteAlertRule"},
			"alertId":       {in.AlertId},
			"error.message": {err.Error()},
		})
		return nil, huma.Error500InternalServerError(
			"Internal error while deleting the alert rule from source.")
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
		if !alertRule.LinearCalibration.IsEmpty() {
			message.AlertValues = Calculate(
				alertRule.LinearCalibration.ToCalibrated, alertRule.RangeValues)
			message.LinearCalibration = alertRule.LinearCalibration
		}
	}
	users := make([]auth.UserInfo, 0, len(alertRule.EmailAddresses))
	for _, e := range alertRule.EmailAddresses {
		users = append(users, auth.UserInfo{EmailAddress: e})
	}
	if err = a.Notifier.Notify(message, a.Templater, users...); err != nil {
		log.AddMetadata(logging.Metadata{
			"source":        {"notifier.notify"},
			"error.message": {err.Error()},
		})
		return nil, huma.Error500InternalServerError(
			"Internal error notifying users alert is deleted.")
	}
	return &struct{ Body store.AlertRule }{alertRule}, nil
}

func (a *App) AddUsersToAlertRule(ctx context.Context, in *struct {
	AlertId string `path:"alertId"`
	Body    store.Emails
}) (_ *struct {
	Body store.MutateAlertUsersResponse
}, err error,
) {
	log, ok := ctx.Value("request-log").(logging.LogAccumulator)
	if !ok {
		return nil, huma.Error500InternalServerError(
			"Internal error while getting request log.")
	}
	defer func() {
		if err != nil {
			log.AddMetadata(logging.Metadata{
				"alertId":         {in.AlertId},
				"email-addresses": in.Body.Addresses,
			})
		}
	}()
	ar, err := a.AlertStore.GetAlertRule(in.AlertId)
	if err != nil {
		if errors.Is(err, store.NoAlert) {
			return nil, huma.Error404NotFound("AlertId Not Found.")
		} else {
			return nil, huma.Error500InternalServerError(
				"Internal error checking if AlertId exists.")
		}
	}
	var dbRecord store.Schema
	uniqueEmails := removeDuplicates(in.Body.Addresses)
	dbRecord.AlertRecipients = a.ConvertToRecipients(in.AlertId, uniqueEmails)
	dbRecord.DeviceAlerts = []store.DeviceAlertRule{
		{
			DeviceId:   ar.DeviceId,
			QueryField: ar.QueryField,
			AlertId:    ar.AlertId,
		},
	}
	dbRecord.AlertRules = []store.StoredAlertRule{
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
		log.AddMetadata(logging.Metadata{
			"source":        {"notifier.notify"},
			"error.message": {err.Error()},
		})
		return nil, huma.Error500InternalServerError(
			"Internal error notifying users of alert registration.")
	}
	return &struct {
		Body store.MutateAlertUsersResponse
	}{
		Body: store.MutateAlertUsersResponse{
			AlertId: ar.AlertId, EmailAddresses: in.Body.Addresses,
		},
	}, nil
}

func (a *App) RemoveUsersFromAlertRule(ctx context.Context, in *struct {
	AlertId string `path:"alertId"`
	Body    store.Emails
}) (_ *struct {
	Status int
	Body   store.MutateAlertUsersResponse
}, err error,
) {
	log, ok := ctx.Value("request-log").(logging.LogAccumulator)
	if !ok {
		return nil, huma.Error500InternalServerError(
			"Internal error while getting request log.")
	}
	defer func() {
		if err != nil {
			log.AddMetadata(logging.Metadata{
				"alertId":         {in.AlertId},
				"email-addresses": in.Body.Addresses,
			})
		}
	}()
	ar, err := a.AlertStore.GetAlertRule(in.AlertId)
	if err != nil {
		if errors.Is(err, store.NoAlert) {
			return nil, huma.Error404NotFound("AlertId Not Found.")
		} else {
			return nil, huma.Error500InternalServerError(
				"Internal error checking if AlertId exists.")
		}
	}
	emailsToDelete := make([]string, 0, len(in.Body.Addresses))
	for _, email := range in.Body.Addresses {
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
			Body   store.MutateAlertUsersResponse
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
			log.AddMetadata(logging.Metadata{
				"source":        {"notifier.notify"},
				"error.message": {err.Error()},
			})
			return nil, huma.Error500InternalServerError(
				"Internal error notifying users unregistered from alert.")
		}
	}
	return &struct {
		Status int
		Body   store.MutateAlertUsersResponse
	}{Status: http.StatusOK, Body: store.MutateAlertUsersResponse{
		AlertId:        in.AlertId,
		EmailAddresses: in.Body.Addresses,
	}}, nil
}
