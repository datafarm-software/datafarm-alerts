package app

import (
	"log"

	"github.com/geraud22/sensordata-alerts/alertsource"
)

type App struct{}

func (a *App) RespondToAlert(alert alertsource.InfluxAlert) {
	alertState := alert.GetState()
	log.Println("state: ", alertState)
}
