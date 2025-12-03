package alertstore

import (
	"fmt"
	"strconv"
	"time"

	"github.com/geraud22/sensordata-alerts/alertsource"
)

type AlertStore interface {
	//NOTE: could return alertsource.FirstAlert
	GetPreviousAlert(deviceId, queryField string) (*alertsource.Alert, error)
	GetAlertRecipients(id string) ([]string, error)
	Update(*alertsource.Alert) error
	StoreAlert(alertsource.AlertRule) error
	AddUserToNotifyList(alertId string, username string) error
}

var InvalidDuration = time.Duration(-(1 * time.Hour))

func ParseDuration(d string) (time.Duration, error) {
	if len(d) != 2 {
		return InvalidDuration,
			fmt.Errorf("expected duration length 2, got: %d", len(d))
	}
	num, err := strconv.Atoi(string(d[0]))
	if err != nil {
		return InvalidDuration, err
	}
	numAsDuration := time.Duration(num)
	period := d[1]
	var duration time.Duration
	switch period {
	case byte('s'):
		duration = time.Duration(numAsDuration * time.Second)
	case byte('m'):
		duration = time.Duration(numAsDuration * time.Minute)
	case byte('h'):
		duration = time.Duration(numAsDuration * time.Hour)
	case byte('d'):
		duration = time.Duration(numAsDuration * (24 * time.Hour))
	default:
		return InvalidDuration, fmt.Errorf("unknown period selector: %s", string(period))
	}
	return duration, nil
}
