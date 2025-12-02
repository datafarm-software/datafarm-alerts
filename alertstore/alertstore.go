package alertstore

import "github.com/geraud22/sensordata-alerts/alertsource"

type AlertStore interface {
	//NOTE: could return alertsource.FirstAlert
	GetPreviousAlert(deviceId, queryField string) (*alertsource.Alert, error)
	GetAlertRecipients(id string) ([]string, error)
	Update(*alertsource.Alert) error
	CreateAlert(alertsource.CreateAlert) error
}
