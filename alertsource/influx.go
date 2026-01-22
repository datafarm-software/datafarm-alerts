package alertsource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	cfy "github.com/geraud22/config-from-yaml"
	"github.com/geraud22/sensordata-alerts/alertstore"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/domain"
)

var msg string = `Check: ${r._check_name} State: ${r._level}.`

var ZeroSeconds = "0s"

const Flux string = `from(bucket: "%s")
	|> range(start: v.timeRangeStart, stop: v.timeRangeStop)
    |> filter(fn: (r) => r["deviceID"] == "%s")
    |> filter(fn: (r) => r["_field"] == "%s")
    |> aggregateWindow(every: %s, fn: last, createEmpty: false)`

var ctx context.Context

type EpochNano int64

func (e *EpochNano) UnmarshalJSON(b []byte) error {
	n := json.Number(strings.Trim(string(b), `"`))
	if i, err := n.Int64(); err != nil {
		*e = EpochNano(i)
		return nil
	}
	f, err := n.Float64()
	if err != nil {
		return err
	}
	*e = EpochNano(int64(f))
	return nil
}

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
	SourceTimestamp          EpochNano `json:"_source_timestamp"`
	StatusTimestamp          EpochNano `json:"_status_timestamp"`
	Start                    string    `json:"_start"`
	Stop                     string    `json:"_stop"`
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
	t := time.Unix(0, int64(i.SourceTimestamp))
	return alertstore.AlertState{
		Time:  t,
		Name:  i.Name,
		Id:    i.Id,
		Level: i.AlertLevel(),
	}
}

type InfluxOpts struct {
	Org   string `mapstructure:"org" validate:"required"`
	Url   string `mapstructure:"url" validate:"required"`
	Token string `mapstructure:"token" validate:"required"`
}

type Influx struct {
	ctxCancel  context.CancelFunc
	client     influxdb2.Client
	orgId, org string
}

func NewInflux(opts InfluxOpts) (AlertSource, error) {
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
		org:       opts.Org,
		orgId:     *org.Id,
	}, nil
}

func (i *Influx) Close() error {
	i.ctxCancel()
	i.client.Close()
	return nil
}

func (i *Influx) CreateAlertRule(ar alertstore.AlertRule) (string, error) {
	check, err := i.createThresholdCheck(ar)
	if err != nil {
		return "", err
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
	check, err := i.createThresholdCheck(ar)
	if err != nil {
		return err
	}
	updateParams := domain.PutChecksIDAllParams{
		CheckID: ar.AlertId,
		Body:    &check,
	}
	_, err = i.client.APIClient().PutChecksID(ctx, &updateParams)
	return err
}

func (i *Influx) DeleteAlertRule(alertId string) error {
	gp := domain.DeleteChecksIDAllParams{
		CheckID: alertId,
	}
	if err := i.client.APIClient().DeleteChecksID(ctx, &gp); err != nil {
		return err
	}
	return nil
}

func (i *Influx) DeleteAllAlertRules() error {
	var err error
	var checkIds []string
	var allChecks []domain.Check
	var resp *domain.Checks
	var nextUrl, nextOffsetStr string
	var nextOffset int
	var o domain.Offset
	var u *url.URL
	gp := domain.GetChecksParams{
		OrgID: i.orgId,
	}
	for {
		resp, err = i.client.APIClient().GetChecks(ctx, &gp)
		if err != nil {
			break
		}
		if resp.Checks != nil {
			allChecks = append(allChecks, *resp.Checks...)
		}
		if resp.Links == nil || resp.Links.Next == nil {
			break
		}
		nextUrl = string(*resp.Links.Next)
		u, err = url.Parse(nextUrl)
		if err != nil {
			break
		}
		nextOffsetStr = u.Query().Get("offset")
		if nextOffsetStr == "" {
			break
		}
		nextOffset, err = strconv.Atoi(nextOffsetStr)
		if err != nil {
			break
		}
		o = domain.Offset(nextOffset)
		gp.Offset = &o
	}
	if err != nil {
		return err
	}
	var tc *domain.ThresholdCheck
	var ok bool
	for _, c := range allChecks {
		tc, ok = c.(*domain.ThresholdCheck)
		if !ok {
			err = fmt.Errorf("check could not be type asserted to domain.ThresholdCheck")
			break
		}
		checkIds = append(checkIds, *tc.Id)
	}
	if err != nil {
		return err
	}
	var dp domain.DeleteChecksIDAllParams
	for _, id := range checkIds {
		dp = domain.DeleteChecksIDAllParams{
			CheckID: id,
		}
		if err = i.client.APIClient().DeleteChecksID(ctx, &dp); err != nil {
			break
		}
	}
	return err
}

func (i *Influx) PrepareDb(...alertstore.AlertRule) error {
	return nil
}

func (i *Influx) GetSnapshot() int {
	return 0
}

func (i *Influx) createThresholdCheck(ar alertstore.AlertRule) (domain.ThresholdCheck, error) {
	var lesser domain.LesserThreshold
	var greater domain.GreaterThreshold
	var rang domain.RangeThreshold
	var thresholds []domain.Threshold
	var check domain.ThresholdCheck
	lo := domain.CheckStatusLevelOK
	lc := domain.CheckStatusLevelCRIT
	allValues := true

	switch ar.AlertType {
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
			return check, fmt.Errorf("first boundary is not smaller than the second.")
		}
		rang = domain.RangeThreshold{
			ThresholdBase: domain.ThresholdBase{Level: &lc},
			Max:           ar.RangeValues[1],
			Min:           ar.RangeValues[0],
		}
		thresholds = append(thresholds, &rang)
	default:
		return check, fmt.Errorf("unknown alert type: %d", ar.AlertType)
	}

	key := "queryField"
	hydratedFlux := fmt.Sprintf(Flux, i.org, ar.DeviceId, ar.QueryField, ar.Duration)
	check = domain.ThresholdCheck{
		CheckBaseExtend: domain.CheckBaseExtend{
			CheckBase: domain.CheckBase{
				Name:   ar.Name,
				OrgID:  i.orgId,
				Query:  domain.DashboardQuery{Text: &hydratedFlux},
				Status: domain.TaskStatusTypeActive,
			},
			Every:                 &ar.Duration,
			Offset:                &ZeroSeconds,
			StatusMessageTemplate: &msg,
			Tags: &[]struct {
				Key   *string `json:"key,omitempty"`
				Value *string `json:"value,omitempty"`
			}{{Key: &key, Value: &ar.QueryField}},
		},
		Thresholds: &thresholds,
	}
	return check, nil
}

var once sync.Once
var testingInfluxOpts InfluxOpts

type TestingInflux struct {
	influx             AlertSource
	testingAlertsCount int
	testingAlertIdsMap map[string]string
}

func NewTestingInflux() (AlertSource, error) {
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
	})
	if topErr != nil {
		return nil, topErr
	}
	db, err := NewInflux(testingInfluxOpts)
	if err != nil {
		return nil, err
	}
	return &TestingInflux{
		testingAlertIdsMap: make(map[string]string),
		influx:             db,
	}, nil
}

func (i *TestingInflux) Close() error {
	var err error
	for _, id := range i.testingAlertIdsMap {
		if err = i.influx.DeleteAlertRule(id); err != nil {
			break
		}
	}
	if err != nil {
		return err
	}
	return i.influx.Close()
}

func (i *TestingInflux) PrepareDb(alerts ...alertstore.AlertRule) error {
	var err error
	for _, rule := range alerts {
		_, err = i.CreateAlertRule(rule)
		if err != nil {
			break
		}
	}
	return err
}

func (i *TestingInflux) CreateAlertRule(ar alertstore.AlertRule) (string, error) {
	alertId, err := i.influx.CreateAlertRule(ar)
	if err != nil {
		return "", err
	}
	i.testingAlertsCount++
	testingAlertId := fmt.Sprintf("alert%d", i.testingAlertsCount)
	i.testingAlertIdsMap[testingAlertId] = alertId
	return testingAlertId, nil
}

func (i *TestingInflux) UpdateAlertRule(ar alertstore.AlertRule) error {
	ar.AlertId = i.testingAlertIdsMap[ar.AlertId]
	return i.influx.UpdateAlertRule(ar)
}

func (i *TestingInflux) DeleteAlertRule(alertId string) error {
	alertId = i.testingAlertIdsMap[alertId]
	if err := i.influx.DeleteAlertRule(alertId); err != nil {
		return err
	}
	i.testingAlertsCount--
	return nil
}

func (i *TestingInflux) GetSnapshot() int {
	return i.testingAlertsCount
}
