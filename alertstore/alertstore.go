package alertstore

import "github.com/geraud22/sensordata-alerts/alertsource"

type AlertStore interface {
	GetPreviousAlert(deviceId, queryField string) (*alertsource.AlertState, error)
	GetAlertRecipients(*alertsource.AlertState) ([]string, error)
}
