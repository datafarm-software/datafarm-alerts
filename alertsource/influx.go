package alertsource

import (
	"context"
	"fmt"
	"time"

	"github.com/geraud22/sensordata-alerts/alertstore"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/domain"
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
	Mode  alertstore.Mode `mapstructure:"mode"`
}

type Influx struct {
	ctxCancel          context.CancelFunc
	client             influxdb2.Client
	mode               alertstore.Mode
	testingAlertsCount int
}

func NewInflux(opts InfluxOpts) (*Influx, error) {
	c, cancel := context.WithCancel(context.Background())
	ctx = c
	client := influxdb2.NewClientWithOptions(opts.Url, opts.Token, influxdb2.DefaultOptions())
	err := client.APIClient().GetPing(ctx)
	return &Influx{
		ctxCancel: cancel,
		client:    client,
		mode:      opts.Mode,
	}, err
}

func (i *Influx) Close() error {
	i.ctxCancel()
	i.client.Close()
	return nil
}

func (i *Influx) CreateAlertRule(ar alertstore.AlertRule) (string, error) {
	var lesser domain.LesserThreshold
	var greater domain.GreaterThreshold
	var rang domain.RangeThreshold
	lo := domain.CheckStatusLevelOK
	lc := domain.CheckStatusLevelCRIT

	switch ar.AlertRange {
	case alertstore.LessThan:
		greater = domain.GreaterThreshold{
			Value:         ar.RangeValues[0],
			ThresholdBase: domain.ThresholdBase{Level: &lo},
		}
		lesser = domain.LesserThreshold{
			Value:         ar.RangeValues[1],
			ThresholdBase: domain.ThresholdBase{Level: &lc},
		}

	case alertstore.GreaterThan:
		greater = domain.GreaterThreshold{
			Value:         ar.RangeValues[0],
			ThresholdBase: domain.ThresholdBase{Level: &lc},
		}
		lesser = domain.LesserThreshold{
			Value:         ar.RangeValues[1],
			ThresholdBase: domain.ThresholdBase{Level: &lo},
		}

	case alertstore.Range:
		if ar.RangeValues[0] >= ar.RangeValues[1] {
			return "", fmt.Errorf("first boundary is not smaller than the second.")
		}
		rang = domain.RangeThreshold{
			ThresholdBase: domain.ThresholdBase{Level: &lo},
			Max:           ar.RangeValues[1],
			Min:           ar.RangeValues[0],
		}
		greater = domain.GreaterThreshold{
			ThresholdBase: domain.ThresholdBase{Level: &lc},
			Value:         ar.RangeValues[1],
		}
		lesser = domain.LesserThreshold{
			ThresholdBase: domain.ThresholdBase{Level: &lc},
			Value:         ar.RangeValues[0],
		}
	default:
		return "", fmt.Errorf("unknown alert range type: %d", ar.AlertRange)
	}

	thresholds := []domain.Threshold{&greater, &lesser, &rang}
	key := "queryField"
	check := domain.ThresholdCheck{
		CheckBaseExtend: domain.CheckBaseExtend{
			CheckBase: domain.CheckBase{
				Name:   ar.Name,
				OrgID:  *org.Id,
				Query:  domain.DashboardQuery{Text: &flux},
				Status: domain.TaskStatusTypeActive,
			},
			Every:                 &ar.Duration,
			StatusMessageTemplate: &msg,
			Tags: &[]struct {
				Key   *string `json:"key,omitempty"`
				Value *string `json:"value,omitempty"`
			}{{Key: &key, Value: &ar.QueryField}},
		},
		Thresholds: &thresholds,
	}

	params := domain.CreateCheckAllParams{
		Body: domain.CreateCheckJSONRequestBody(&check),
	}
	nc, err := i.client.APIClient().CreateCheck(ctx, &params)
	if err != nil {
		return "", err
	}

	var alertId string
	if nc.Type() == "threshold" {
		tc, ok := nc.(*domain.ThresholdCheck)
		if !ok {
			return "", fmt.Errorf("invalid check created")
		}
		alertId = *tc.CheckBase.Id
	}
	return alertId, nil
}

func (i *Influx) UpdateAlertRule(ar alertstore.AlertRule) error {
	return fmt.Errorf("not implemented")
}

func (i *Influx) DeleteAlertRule(alertId string) error {
	return fmt.Errorf("not implemented")
}

func (i *Influx) PrepareDb(alertsCount int) error {
	i.testingAlertsCount = alertsCount
	return nil
}

func (i *Influx) GetSnapshot() int {
	return i.testingAlertsCount
}
