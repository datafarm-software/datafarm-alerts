package alertsource

import "time"

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
	Type                     string    `json:"_type" format:"date-time"`
	// Value                    float64  `json:"_value"`
	Version    int      `json:"_version"`
	DeviceId   string   `json:"deviceID" required:"true"`
	QueryField string   `json:"query_field" required:"true"`
	_          struct{} `json:"-" additionalProperties:"true"`
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

func (i *InfluxAlert) GetAlert() *Alert {
	return &Alert{
		Level:      i.GetLevel(),
		Time:       i.Time,
		Message:    i.Message,
		DeviceId:   i.DeviceId,
		QueryField: i.QueryField,
		Id:         i.Id,
	}
}
