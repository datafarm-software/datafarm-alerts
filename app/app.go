package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

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
	Alertstore alertstore.AlertStore
	Notifier   notifier.Notifier
}

func Start(opts Opts) error {
	app := &App{}
	app.Notifier = notifier.NewMockNotifier(nil)
	app.Alertstore = alertstore.NewMockAlertStore(nil)
	cli := humacli.New(func(hooks humacli.Hooks, options *Opts) {
		router := http.NewServeMux()
		api := humago.New(router, huma.DefaultConfig("SensorData Alerts API", "1.0.0"))
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

func (a *App) RegisterHumaOperations(api huma.API) {
	operation := huma.Operation{
		Method:  "POST",
		Path:    "/notify",
		Tags:    []string{"Post Alerts"},
		Summary: "Post an alert.",
	}
	huma.Register(api, operation, a.LogAlert)
}

func (a *App) LogAlert(
	ctx context.Context, in *struct {
		Body *alertsource.InfluxAlert
	}) (*struct{}, error) {
	log.Println("received: ", in.Body.GetAlert())
	return nil, nil
}

func (a *App) RespondToAlert(ctx context.Context, alerter *alertsource.InfluxAlert) (*struct{}, error) {
	alert := alerter.GetAlert()
	previous, err := a.Alertstore.GetPreviousAlert(alert.DeviceId, alert.QueryField)
	if err != nil {
		log.Printf(
			"getting previous alert for %s: %s, error: %v",
			alert.DeviceId, alert.QueryField, err,
		)
		return nil, nil
	}
	alert.Duration = previous.Duration
	if a.ShouldNotifyCrit(alert, previous) {
		users, err := a.Alertstore.GetAlertRecipients(alert.Id)
		if err != nil {
			log.Printf("GetAlertRecipients: %v", err)
			return nil, nil
		}
		if err = a.Notifier.Notify(users, alert.Message); err != nil {
			log.Printf("notifying users: %v", err)
			return nil, nil
		}
	}
	return nil, nil
}
func (a *App) ShouldNotifyCrit(alert *alertsource.AlertState, previous *alertsource.AlertState) bool {
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
