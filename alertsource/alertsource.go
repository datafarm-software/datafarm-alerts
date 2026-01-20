package alertsource

import (
	"github.com/geraud22/sensordata-alerts/alertstore"
)

type AlertSetup interface {
	RangeValues() []float32
	Duration() string
	AlertType() alertstore.AlertType
}

type AlertFromSource interface {
	AlertState() alertstore.AlertState
	AlertLevel() alertstore.AlertLevel
}

type AlertSource interface {
	Close() error
	UpdateAlertRule(alertstore.AlertRule) (AlertSetup, error)
	CreateAlertRule(alertstore.AlertRule) (string, error)
	//NOTE: Could return alertstore.NoAlert
	DeleteAlertRule(alertId string) error
	//NOTE: used in testing
	PrepareDb(int) error
	GetSnapshot() int
}
