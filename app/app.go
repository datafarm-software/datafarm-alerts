package app

import (
	"log"

	"github.com/geraud22/sensordata-alerts/alertsource"
	"github.com/geraud22/sensordata-alerts/alertstore"
	"github.com/geraud22/sensordata-alerts/notifier"
)

type App struct {
	Alertstore alertstore.AlertStore
	Notifier   notifier.Notifier
}

func (a *App) RespondToAlert(alerter alertsource.InfluxAlert) {
	alert := alerter.GetAlert()
	previous, err := a.Alertstore.GetPreviousAlert(alert.DeviceId, alert.QueryField)
	if err != nil {
		log.Printf(
			"getting previous alert for %s: %s, error: %v",
			alert.DeviceId, alert.QueryField, err,
		)
		return
	}
	alert.Duration = previous.Duration
	if a.ShouldNotifyCrit(alert, previous) {
		users, err := a.Alertstore.GetAlertRecipients(alert.Id)
		if err != nil {
			log.Printf("GetAlertRecipients: %v", err)
			return
		}
		if err = a.Notifier.Notify(users, alert.Message); err != nil {
			log.Printf("notifying users: %v", err)
			return
		}
	}
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
