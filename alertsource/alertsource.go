package alertsource

import (
	"github.com/geraud22/sensordata-alerts/alertstore"
)

type AlertSource interface {
	UpdateAlertRule(alertstore.AlertRule) error
	CreateAlertRule(alertstore.AlertRule) (string, error)
	GetAlertState() alertstore.AlertState
	GetAlertLevel() alertstore.AlertLevel
	//NOTE: Could return alertstore.NoAlert
	DeleteAlertRule(alertId string) error
}
