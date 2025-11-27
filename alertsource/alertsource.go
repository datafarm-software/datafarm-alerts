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
	DeviceId   string
	Level      AlertLevel
	QueryField string
	LastAlert  time.Time
}
