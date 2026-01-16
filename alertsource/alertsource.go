package alertsource

import (
	"github.com/geraud22/sensordata-alerts/alertstore"
)

type AlertFromSource interface {
	AlertState() alertstore.AlertState
	AlertLevel() alertstore.AlertLevel
}

type AlertSource interface {
	UpdateAlertRule(alertstore.AlertRule) error
	CreateAlertRule(alertstore.AlertRule) (string, error)
	//NOTE: Could return alertstore.NoAlert
	DeleteAlertRule(alertId string) error
}
