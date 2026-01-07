package alertsource

import (
	"fmt"
	"time"

	"github.com/geraud22/sensordata-alerts/alertstore"
)

type InfluxAlert struct {
	Id                       string    `json:"_check_id" required:"true"`
	Name                     string    `json:"_check_name"`
	Level                    string    `json:"_level" required:"true"`
	Measurement              string    `json:"_measurement"`
	Message                  string    `json:"_message" required:"true"`
	NotificationEndpointId   string    `json:"_notification_endpoint_id"`
	NotificationEndpointName string    `json:"_notification_endpoint_name"`
	NotificationRuleId       string    `json:"_notification_rule_id"`
	NotificationRuleName     string    `json:"_notification_rule_name"`
	SourceMeasurement        string    `json:"_source_measurement"`
	SourceTimestamp          int       `json:"_source_timestamp"`
	Start                    string    `json:"_start"`
	Stop                     string    `json:"_stop"`
	StatusTimestamp          int       `json:"_status_timestamp"`
	Time                     time.Time `json:"_time" required:"true"`
	Type                     string    `json:"_type"`
	// Value                    float64  `json:"_value"`
	Version    int      `json:"_version"`
	DeviceId   string   `json:"deviceID" required:"true"`
	QueryField string   `json:"query_field" required:"true"`
	_          struct{} `json:"-" additionalProperties:"true"`
}

func (i *InfluxAlert) GetAlertLevel() alertstore.AlertLevel {
	var al alertstore.AlertLevel
	switch i.Level {
	case "ok":
		al = alertstore.Ok
	case "warn":
		al = alertstore.Warn
	case "crit":
		al = alertstore.Crit
	default:
		al = alertstore.Invalid
	}
	return al
}

func (i *InfluxAlert) GetAlertState() *alertstore.AlertState {
	return &alertstore.AlertState{
		Time:    i.Time,
		Message: i.Message,
		Id:      i.Id,
		Level:   i.GetAlertLevel(),
	}
}

func (i *InfluxAlert) RegisterAlertRule(ar alertstore.AlertRule) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (i *InfluxAlert) UpdateAlertRule(ar alertstore.AlertRule) error {
	return fmt.Errorf("not implemented")
}
