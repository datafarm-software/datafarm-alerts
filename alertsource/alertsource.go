package alertsource

import "time"

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

type AlertRule struct {
	Usernames   []string   `json:"usernames" pattern:"^[a-zA-Z0-9_]+$" doc:"Usernames to notify in the event of an alert."`
	RangeValues []int      `json:"rangeValues" minItems:"1" maxItems:"2" doc:"One or two values, depending on whether the alertRange is a range or < >"`
	Duration    string     `json:"duration" pattern:"^[0-9]{1,2}[smhd]$" doc:"A duration literal, specifying how often a check should occur."`
	DeviceId    string     `json:"deviceId" pattern:"^[a-zA-Z0-9]+$" doc:"The device for which data should be checked."`
	QueryField  string     `json:"queryField" pattern:"^[a-zA-Z0-9_]+$" doc:"The field on the device, for which data should be checked."`
	AlertRange  AlertRange `json:"alertRange" maximum:"2" doc:"Specify type of check to perform. 0 = Less Than, 1 = Greater Than, 2 = Range."`
}

type CreatedRule struct {
	AlertId string `json:"alertId" readOnly:"true" doc:"Generated Alert Id."`
}

type AlertState struct {
	Time        time.Time
	Duration    time.Duration
	RangeValues []int
	Message     string
	Id          string
	Level       AlertLevel
}

type AlertSource interface {
	UpdateAlertRule(AlertRule) error
	GetAlertState() *AlertState
	RegisterAlertRule(AlertRule) (string, error)
	GetAlertLevel() AlertLevel
}
