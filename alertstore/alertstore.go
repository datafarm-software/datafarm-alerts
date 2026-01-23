package alertstore

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

var InvalidDuration = time.Duration(-(1 * time.Hour))
var AlertExists = errors.New("alert exists")
var NoAlert = errors.New("no alert")

type AlertType int

const (
	LessThan AlertType = iota
	GreaterThan
	Range
)

type AlertLevel int

var AlertLevelString = []string{
	"OK",
	"WARN",
	"CRIT",
	"INVALID",
}

const (
	Ok AlertLevel = iota
	Warn
	Crit
	Invalid
)

type AlertStore interface {
	Close() error
	PrepareDb(Schema) error
	GetSnapshot() Schema
	//NOTE: could return alertstore.NoAlert
	GetAlertState(alertId string) (AlertState, error)
	//NOTE: could return alertstore.NoAlert
	GetAlertRule(alertId string) (AlertRule, error)
	GetAlertRecipients(alertId string) ([]string, error)
	UpdateAlertState(alert AlertState) error
	StoreAlertRule(dbRecord Schema) error
	UpdateAlertRule(alertId string, dbRecord Schema) error
	//NOTE: could return alertstore.AlertExists
	CheckAlertRuleExists(AlertRule) (string, error)
	DeleteAlertState(alertId string) error
	//NOTE: could return alertstore.NoAlert
	DeleteAlertRule(alertId string) (AlertRule, error)
}

type Schema struct {
	DeviceAlerts    []DeviceAlertRule
	AlertRecipients []AlertRecipient
	AlertStates     []AlertState
	AlertRules      []StoredAlertRule
}

type DeviceAlertRule struct {
	DeviceId   string `gorm:"index:idx_deviceId"`
	QueryField string `gorm:"index:idx_queryField"`
	AlertId    string `gorm:"primaryKey;unique;not null;check:length(alert_id)>0"`
}

type AlertRecipient struct {
	AlertId string `gorm:"check:length(alert_id)>0;index:uniq_alert_email,unique"`
	Email   string `gorm:"check:length(email)>0;index:uniq_alert_email,unique"`
}

type AlertState struct {
	Time        time.Time
	Duration    time.Duration
	RangeValues []float32 `gorm:"serializer:json"`
	Name        string    `gorm:"-"`
	Value       float64
	Id          string `gorm:"primaryKey;unique;not null;check:length(id)>0"`
	Level       AlertLevel
}

type StoredAlertRule struct {
	AlertId     string    `gorm:"primaryKey;unique;not null;check:length(alert_id)>0"`
	RangeValues []float32 `gorm:"serializer:json"`
	Name        string    `gorm:"name"`
	Duration    string    `gorm:"index:idx_duration"`
	AlertType   AlertType `gorm:"index:idx_alertType"`
}

// NOTE: AlertRule never gets stored in the database. A read-only view is created from the normalized schema, which can return an instance of AlertRule. So the gorm tags are for serializing into memory only.
type AlertRule struct {
	AlertId        string    `json:"alertId" doc:"Alert Id."`
	EmailAddresses []string  `gorm:"serializer:json" json:"emailAddresses" minItems:"1" format:"email" doc:"List of email addresses to notify in the event of the alert. If updated, new list will replace old one as an overwrite."` //NOTE: implementation should know to automatically store this in a join table with alertId.
	RangeValues    []float32 `gorm:"serializer:json" json:"rangeValues" minItems:"1" maxItems:"2" doc:"One or two values, depending on whether the alertRange is a range or < >"`                                                     //NOTE: this should get stored as a string
	Name           string    `json:"name" pattern:"^[a-zA-Z0-9 ]+$" doc:"The name of the alert. For easier recognition."`
	Duration       string    `json:"duration" pattern:"^[0-9]{1,2}[smhd]$" doc:"A duration literal, specifying how often a check should occur."`
	DeviceId       string    `json:"deviceId" pattern:"^[a-zA-Z0-9]+$" doc:"The device for which data should be checked."`
	QueryField     string    `json:"queryField" pattern:"^[a-zA-Z0-9_]+$" doc:"The field on the device, for which data should be checked."`
	AlertType      AlertType `json:"alertType" maximum:"2" doc:"Specify type of check to perform. 0 = Less Than, 1 = Greater Than, 2 = Range."`
}

type CreatedRule struct {
	AlertId string `json:"alertId" readOnly:"true" doc:"Generated Alert Id."`
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
