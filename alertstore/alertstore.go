package alertstore

import (
	"fmt"
	"time"

	"github.com/geraud22/sensordata-alerts/alertsource"
)

type AlertStore interface {
	//NOTE: could return alertsource.FirstAlert
	GetPreviousAlert(deviceId, queryField string) (*alertsource.Alert, error)
	GetAlertRecipients(id string) ([]string, error)
	Update(*alertsource.Alert) error
	StoreAlert(alertsource.CreateAlert) error
}

var InvalidDuration = time.Duration(-(1 * time.Hour))

func ParseDuration(d string) (time.Duration, error) {
	if len(d) != 2 {
		return InvalidDuration,
			fmt.Errorf("expected duration length 2, got: %d", len(d))
	}
	num := d[0]
	numAsDuration := time.Duration(int(num))
	period := d[1]
	var duration time.Duration
	switch period {
	case byte('s'):
		duration = numAsDuration * time.Second
	case byte('m'):
		duration = numAsDuration * time.Minute
	case byte('h'):
		duration = numAsDuration * time.Hour
	case byte('d'):
		duration = numAsDuration * (24 * time.Hour)
	default:
		return InvalidDuration, fmt.Errorf("unknown period selector: %s", period)
	}
	return duration, nil
}
