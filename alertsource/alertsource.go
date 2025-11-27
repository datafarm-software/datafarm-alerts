package alertsource

type AlertLevel int

const (
	Ok AlertLevel = iota
	Warn
	Crit
	Invalid
)
