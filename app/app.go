package app

import (
	"log"

	"github.com/geraud22/sensordata-alerts/alertsource"
)

type App struct{}

func (a *App) RespondToAlert(alert alertsource.InfluxAlert) {
	alertLevel := alert.GetLevel()
	log.Println("level: ", alertLevel)
}
