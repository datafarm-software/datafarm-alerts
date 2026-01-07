package alertsource

import "github.com/geraud22/sensordata-alerts/alertstore"

type AlertSource interface {
	UpdateAlertRule(alertstore.AlertRule) error
	GetAlertState() alertstore.AlertState
	RegisterAlertRule(alertstore.AlertRule) (string, error)
	GetAlertLevel() alertstore.AlertLevel
}
