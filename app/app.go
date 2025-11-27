package app

import (
	"log"
	"time"

	"github.com/geraud22/sensordata-alerts/alertsource"
	"github.com/geraud22/sensordata-alerts/alertstore"
)

const TwentyFourHours = time.Duration(24 * time.Hour)

type App struct {
	alertstore alertstore.AlertStore
}

func (a *App) RespondToAlert(alerter alertsource.InfluxAlert) {
	alert := alerter.GetAlert()
	previous, err := a.alertstore.GetPreviousAlert(alert.DeviceId, alert.QueryField)
	if err != nil {
		log.Printf(
			"getting previous alert for %s: %s, error: %v",
			alert.DeviceId, alert.QueryField, err,
		)
	}
	if a.ShouldNotifyCrit(alert, previous) {
		users := a.alertstore.GetAlertRecipients(alert)
		if err = a.notifier.Notify(users, alert.Message); err != nil {
			log.Println("notifying users: %v", err)
		}
	}
}
func (a *App) ShouldNotifyCrit(alert *alertsource.AlertState, previous *alertsource.AlertState) bool {
	if alert.Level == previous.Level {
		return false
	}
	if alert.Level != alertsource.Crit {
		return false
	}
	if alert.Time.Sub(previous.Time) > TwentyFourHours {
		return true
	}
	return false
}
