package app

import (
	"log"

	"github.com/geraud22/sensordata-alerts/alertsource"
	"github.com/geraud22/sensordata-alerts/alertstore"
)

type App struct {
	alertstore alertstore.AlertStore
}

func (a *App) RespondToAlert(alerter alertsource.InfluxAlert) {
	alert := alerter.GetAlert()
	previous, err := a.alertstore.GetPreviousAlert(alert.DeviceId, alert.QueryField)
	if err != nil {
		log.Println(
			"getting previous alert for %s: %s, error: %v",
			alert.DeviceId, alert.QueryField, err,
		)
	}
	if a.ShouldNotify(alert, previous) {
		users := a.alertstore.GetAlertRecipients(alert)
		if err = a.notifier.Notify(users, alert.Message); err != nil {
			log.Println("notifying users: %v", err)
		}
	}
}
