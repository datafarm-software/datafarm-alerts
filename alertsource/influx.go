package alertsource

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	cfy "github.com/geraud22/config-from-yaml"
	"github.com/geraud22/sensordata-alerts/alertstore"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/domain"
)

var msg string = `
	Check: ${r._check_name} DeviceId: %s, QueryField: %s
	State: ${r._level} at Time: %s, due to value ${r._value}`

const Flux string = `from(bucket: "%s")
	|> range(start: %s)
    |> filter(fn: (r) => r["deviceID"] == "%s")
    |> filter(fn: (r) => r["_field"] == "%s")
    |> aggregateWindow(every: %s, fn: mean, createEmpty: false)`

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
	Org   string `mapstructure:"org" validate:"required"`
	Url   string `mapstructure:"url" validate:"required"`
	Token string `mapstructure:"token" validate:"required"`
	Mode  alertstore.Mode
}

type Influx struct {
	ctxCancel          context.CancelFunc
	client             influxdb2.Client
	mode               alertstore.Mode
	orgId, org         string
	testingAlertsCount int
	testingAlertIds    []string
}

var once sync.Once
var testingInfluxOpts InfluxOpts

func NewTestingInflux() (*Influx, error) {
	var topErr error
	once.Do(func() {
		config, err := os.ReadFile("../config.yml")
		if err != nil {
			topErr = err
			return
		}
		opts, err := cfy.LoadConfig[struct {
			InfluxOpts InfluxOpts `mapstructure:"influx"`
		}](bytes.NewReader(config), "yaml", nil)
		if err != nil {
			topErr = err
			return
		}
		testingInfluxOpts = opts.InfluxOpts
		testingInfluxOpts.Mode = alertstore.Testing
	})
	if topErr != nil {
		return nil, topErr
	}
	return NewInflux(testingInfluxOpts)
}

func NewInflux(opts InfluxOpts) (*Influx, error) {
	c, cancel := context.WithCancel(context.Background())
	ctx = c
	client := influxdb2.NewClientWithOptions(opts.Url, opts.Token, influxdb2.DefaultOptions())
	if err := client.APIClient().GetPing(ctx); err != nil {
		cancel()
		return nil, err
	}
	org, err := client.OrganizationsAPI().FindOrganizationByName(ctx, opts.Org)
	if err != nil {
		cancel()
		return nil, err
	}
	if org.Id == nil {
		cancel()
		return nil, fmt.Errorf("found no org")
	}
	return &Influx{
		ctxCancel: cancel,
		client:    client,
		mode:      opts.Mode,
		org:       opts.Org,
		orgId:     *org.Id,
	}, nil
}

func (i *Influx) Close() error {
	var err error
	if i.mode == alertstore.Testing {
		var dp domain.DeleteChecksIDAllParams
		for _, id := range i.testingAlertIds {
			dp = domain.DeleteChecksIDAllParams{
				CheckID: id,
			}
			if err = i.client.APIClient().DeleteChecksID(ctx, &dp); err != nil {
				break
			}
		}
	}
	i.ctxCancel()
	i.client.Close()
	return err
}

func (i *Influx) CreateAlertRule(ar alertstore.AlertRule) (string, error) {
	var lesser domain.LesserThreshold
	var greater domain.GreaterThreshold
	var rang domain.RangeThreshold
	var thresholds []domain.Threshold
	lo := domain.CheckStatusLevelOK
	lc := domain.CheckStatusLevelCRIT
	allValues := true

	switch ar.AlertRange {
	case alertstore.LessThan:
		greater = domain.GreaterThreshold{
			Value:         ar.RangeValues[0],
			ThresholdBase: domain.ThresholdBase{Level: &lo},
		}
		lesser = domain.LesserThreshold{
			Value:         ar.RangeValues[0],
			ThresholdBase: domain.ThresholdBase{Level: &lc},
		}
		thresholds = append(thresholds, &lesser, &greater)

	case alertstore.GreaterThan:
		greater = domain.GreaterThreshold{
			Value:         ar.RangeValues[0],
			ThresholdBase: domain.ThresholdBase{Level: &lc, AllValues: &allValues},
		}
		lesser = domain.LesserThreshold{
			Value:         ar.RangeValues[0],
			ThresholdBase: domain.ThresholdBase{Level: &lo},
		}
		thresholds = []domain.Threshold{&lesser, &greater}

	case alertstore.Range:
		if ar.RangeValues[0] >= ar.RangeValues[1] {
			return "", fmt.Errorf("first boundary is not smaller than the second.")
		}
		rang = domain.RangeThreshold{
			ThresholdBase: domain.ThresholdBase{Level: &lc},
			Max:           ar.RangeValues[1],
			Min:           ar.RangeValues[0],
		}
		thresholds = append(thresholds, &rang)
	default:
		return "", fmt.Errorf("unknown alert range type: %d", ar.AlertRange)
	}

	key := "queryField"
	hydratedFlux := fmt.Sprintf(Flux, i.org, ar.Duration, ar.DeviceId, ar.QueryField, ar.Duration)
	check := domain.ThresholdCheck{
		CheckBaseExtend: domain.CheckBaseExtend{
			CheckBase: domain.CheckBase{
				Name:   ar.Name,
				OrgID:  i.orgId,
				Query:  domain.DashboardQuery{Text: &hydratedFlux},
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
	if i.mode == alertstore.Testing {
		i.testingAlertsCount++
		i.testingAlertIds = append(i.testingAlertIds, alertId)
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
