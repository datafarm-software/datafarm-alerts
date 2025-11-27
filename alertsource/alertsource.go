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
	LastAlert  time.Time
	DeviceId   string
	Message    string
	QueryField string
	Level      AlertLevel
}
