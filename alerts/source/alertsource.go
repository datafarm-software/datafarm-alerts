package source

import (
	"github.com/datafarm-software/datafarm-alerts/alerts/store"
)

type AlertFromSource interface {
	AlertState() store.AlertState
	AlertLevel() store.AlertLevel
}

type TestingAlertSource interface {
	PrepareDb(...store.AlertRule) error
	GetSnapshot() int
}

type AlertSource interface {
	TestingAlertSource
	Close() error
	CreateAlertRule(store.AlertRule) (string, error)
	// NOTE: Could return store.NoAlert
	UpdateAlertRule(store.AlertRule) error
	// NOTE: Could return store.NoAlert
	DeleteAlertRule(alertId string) error
}
