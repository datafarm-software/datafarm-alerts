package alertsource

import (
	"context"
	"fmt"
	"time"

	"github.com/geraud22/sensordata-alerts/alertstore"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

var ctx context.Context

type InfluxAlert struct {
	Id                       string    `json:"_check_id" required:"true"`
	Name                     string    `json:"_check_name"`
	Level                    string    `json:"_level" required:"true"`
	Measurement              string    `json:"_measurement"`
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
	// Value                    float64   `json:"_value"`
	Version    int      `json:"_version"`
	DeviceId   string   `json:"deviceID" required:"true"`
	QueryField string   `json:"query_field" required:"true"`
	_          struct{} `json:"-" additionalProperties:"true"`
}

func (i *InfluxAlert) AlertLevel() alertstore.AlertLevel {
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

func (i *InfluxAlert) AlertState() alertstore.AlertState {
	return alertstore.AlertState{
		Time:  i.Time,
		Name:  i.Name,
		Id:    i.Id,
		Level: i.AlertLevel(),
	}
}

type InfluxOpts struct {
	Org   string          `mapstructure:"org" validate:"required"`
	Url   string          `mapstructure:"url" validate:"required"`
	Token string          `mapstructure:"token" validate:"required"`
	Mode  alertstore.Mode `mapstructure:"mode" validate:"required"`
}

type Influx struct {
	ctxCancel context.CancelFunc
	client    influxdb2.Client
}

func NewInflux(opts InfluxOpts) (*Influx, error) {
	c, cancel := context.WithCancel(context.Background())
	ctx = c
	client := influxdb2.NewClientWithOptions(opts.Url, opts.Token, influxdb2.DefaultOptions())
	err := client.APIClient().GetPing(ctx)
	return &Influx{
		ctxCancel: cancel,
		client:    client,
	}, err
}

func (i *Influx) Close() error {
	i.ctxCancel()
	i.client.Close()
	return nil
}

func (i *Influx) CreateAlertRule(ar alertstore.AlertRule) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (i *Influx) UpdateAlertRule(ar alertstore.AlertRule) error {
	return fmt.Errorf("not implemented")
}

func (i *Influx) DeleteAlertRule(alertId string) error {
	return fmt.Errorf("not implemented")
}
