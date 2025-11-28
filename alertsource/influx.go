package alertsource

import "time"

type InfluxAlert struct {
	Id                       string    `json:"_check_id"`
	Name                     string    `json:"_check_name"`
	Level                    string    `json:"_level"`
	Measurement              string    `json:"_measurement"`
	Message                  string    `json:"_message"`
	NotificationEndpointId   string    `json:"_notification_endpoint_id"`
	NotificationEndpointName string    `json:"_notification_endpoint_name"`
	NotificationRuleId       string    `json:"_notification_rule_id"`
	NotificationRuleName     string    `json:"_notification_rule_name"`
	SourceMeasurement        string    `json:"_source_measurement"`
	SourceTimestamp          int       `json:"_source_timestamp"`
	Start                    string    `json:"_start"`
	Stop                     string    `json:"_stop"`
	StatusTimestamp          int       `json:"_status_timestamp"`
	Time                     time.Time `json:"_time"`
	Type                     string    `json:"_type"`
	Value                    float64   `json:"_value"`
	Version                  int       `json:"_version"`
	DeviceId                 string    `json:"deviceID"`
	QueryField               string    `json:"query_field"`
}

func (i *InfluxAlert) GetLevel() AlertLevel {
	var al AlertLevel
	switch i.Level {
	case "ok":
		al = Ok
	case "warn":
		al = Warn
	case "crit":
		al = Crit
	default:
		al = Invalid
	}
	return al
}

func (i *InfluxAlert) GetAlert() *AlertState {
	return &AlertState{
		DeviceId:   i.DeviceId,
		Level:      i.GetLevel(),
		QueryField: i.QueryField,
		Time:       i.Time,
		Message:    i.Message,
		Id:         i.Id,
	}
}
