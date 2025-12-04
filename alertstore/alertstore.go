package alertstore

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/geraud22/sensordata-alerts/alertsource"
)

var InvalidDuration = time.Duration(-(1 * time.Hour))
var AlertExists = errors.New("alert exists")

type AlertStore interface {
	GetPreviousAlert(deviceId, queryField string) (*alertsource.Alert, error)
	GetAlertRecipients(id string) ([]string, error)
	UpdateAlertLevel(alertId string, level alertsource.AlertLevel) error
	StoreAlert(alertId string, alert alertsource.AlertRule) error
	AddUsersToNotifyList(alertId string, usernames []string) error
	//NOTE: could return alertstore.AlertExists
	CheckAlertExists(alertsource.AlertRule) (string, error)
	UpdateAlertRule(alertsource.AlertRule) error
}

func ParseDuration(d string) (time.Duration, error) {
	var num int
	var err error
	var period byte
	switch {
	case len(d) == 3:
		num, err = strconv.Atoi(string(d[0:2]))
		period = d[2]
	case len(d) == 2:
		num, err = strconv.Atoi(string(d[0]))
		period = d[1]
	default:
		return InvalidDuration,
			fmt.Errorf("invalid duration length, got: %d", len(d))
	}
	if err != nil {
		return InvalidDuration, err
	}
	numAsDuration := time.Duration(num)
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
