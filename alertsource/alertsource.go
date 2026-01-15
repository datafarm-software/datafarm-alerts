package alertsource

import (
	"github.com/geraud22/sensordata-alerts/alertstore"
	"github.com/geraud22/sensordata-alerts/notifier"
)

type AlertSource interface {
	UpdateAlertRule(alertstore.AlertRule) error
	CreateAlertRule(alertstore.AlertRule) (string, error)
	GetAlertState() alertstore.AlertState
	GetAlertLevel() alertstore.AlertLevel
	//NOTE: Could return alertstore.NoAlert
	DeleteAlertRule(alertId string) error
	AlertMessage() notifier.AlertMessage
}
