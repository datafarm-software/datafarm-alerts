package alertsource

import "time"

type AlertLevel int

const (
	Ok AlertLevel = iota
	Warn
	Crit
	Invalid
)

type AlertState struct {
	Time       time.Time
	Duration   time.Duration
	DeviceId   string
	Message    string
	QueryField string
	Id         string
	Level      AlertLevel
}
