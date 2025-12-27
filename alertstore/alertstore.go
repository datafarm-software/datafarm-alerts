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
var NoAlert = errors.New("no alert")

type AlertStore interface {
	//NOTE: could return alertstore.NoAlert
	GetAlertState(alertId string) (*AlertState, error)
	GetAlertRule(alertId string) (AlertRule, error)
	GetAlertRecipients(alertId string) ([]string, error)
	UpdateAlertState(alert *AlertState) error
	StoreAlertRule(alertId string, alert AlertRule) error
	AddUsersToNotifyList(alertId string, usernames []string) error
	//NOTE: could return alertstore.AlertExists
	CheckAlertRuleExists(AlertRule) (string, error)
}

type Schema struct {
	DeviceAlerts    []DeviceAlertRule
	AlertRecipients []AlertRecipient
	AlertStates     []AlertState
	AlertRules      []AlertRule
}

type DeviceAlertRule struct {
	DeviceId   string
	QueryField string
	AlertId    string
}

type AlertRecipient struct {
	AlertId string
	UserId  string
}

type AlertState struct {
	Time        time.Time
	Duration    time.Duration
	RangeValues []int
	Message     string
	Id          string
	Level       alertsource.AlertLevel
}

type AlertRule struct {
	AlertId     string                 `json:"alertId" doc:"Alert Id."`
	Usernames   []string               `json:"usernames" pattern:"^[a-zA-Z0-9_]+$" doc:"Usernames to notify in the event of an alert."`
	RangeValues []int                  `json:"rangeValues" minItems:"1" maxItems:"2" doc:"One or two values, depending on whether the alertRange is a range or < >"`
	Duration    string                 `json:"duration" pattern:"^[0-9]{1,2}[smhd]$" doc:"A duration literal, specifying how often a check should occur."`
	DeviceId    string                 `json:"deviceId" pattern:"^[a-zA-Z0-9]+$" doc:"The device for which data should be checked."`
	QueryField  string                 `json:"queryField" pattern:"^[a-zA-Z0-9_]+$" doc:"The field on the device, for which data should be checked."`
	AlertRange  alertsource.AlertRange `json:"alertRange" maximum:"2" doc:"Specify type of check to perform. 0 = Less Than, 1 = Greater Than, 2 = Range."`
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
