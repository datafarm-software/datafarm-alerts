package alertsource

type AlertLevel int

const (
	Ok AlertLevel = iota
	Warn
	Crit
	Invalid
)

type AlertRange int

const (
	LessThan AlertRange = iota
	GreaterThan
	Range
)

type CreatedRule struct {
	AlertId string `json:"alertId" readOnly:"true" doc:"Generated Alert Id."`
}

type AlertSource interface {
	UpdateAlertRule(AlertRule) error
	GetAlertState() *AlertState
	RegisterAlertRule(AlertRule) (string, error)
	GetAlertLevel() AlertLevel
}
