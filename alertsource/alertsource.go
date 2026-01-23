package alertsource

import (
	"github.com/geraud22/sensordata-alerts/alertstore"
)

type AlertFromSource interface {
	AlertState() alertstore.AlertState
	AlertLevel() alertstore.AlertLevel
}

type TestingAlertSource interface {
	PrepareDb(...alertstore.AlertRule) error
	GetSnapshot() int
}

type AlertSource interface {
	TestingAlertSource
	Close() error
	CreateAlertRule(alertstore.AlertRule) (string, error)
	//NOTE: Could return alertstore.NoAlert
	UpdateAlertRule(alertstore.AlertRule) error
	//NOTE: Could return alertstore.NoAlert
	DeleteAlertRule(alertId string) error
	GetAlertRule(alertId string) (alert any, e error)
	GetTask(alert any)
}
