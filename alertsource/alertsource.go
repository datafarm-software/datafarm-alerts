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

type CreateAlert struct {
	RangeValues []int      `json:"rangeValues" maxItems:"2"`
	Username    string     `json:"username" pattern:"^[a-zA-Z0-9_]+$"`
	Duration    string     `json:"duration" pattern:"^[a-zA-Z0-9]+$"`
	DeviceId    string     `json:"deviceId" pattern:"^[a-zA-Z0-9]+$"`
	QueryField  string     `json:"queryField" pattern:"^[a-zA-Z0-9_]+$"`
	AlertRange  AlertRange `json:"alertRange" maximum:"2"`
}

type Alert struct {
	Time       time.Time
	Duration   time.Duration
	DeviceId   string
	Message    string
	QueryField string
	Id         string
	Level      AlertLevel
}

type AlertSource interface {
	GetAlert() *Alert
	CreateAlert(CreateAlert) error
}
