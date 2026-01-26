package alertsource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	cfy "github.com/geraud22/config-from-yaml"
	"github.com/geraud22/sensordata-alerts/alertstore"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/domain"
)

const Flux string = `from(bucket: "%s")
	|> range(start: v.timeRangeStart, stop: v.timeRangeStop)
    |> filter(fn: (r) => r["deviceID"] == "%s")
    |> filter(fn: (r) => r["_field"] == "%s")
    |> aggregateWindow(every: %s, fn: last, createEmpty: false)`

const MapQueryField = `|> map(fn: (r) => ({r with _value: r.%s}))`
const V1FieldAsCols = `|> v1["fieldsAsCols"]()`

var ThresholdStatusMessage string = `Check: ${r._check_name} State: ${r._level}.`
var DeadmanStatusMessage string = `Check: ${r._check_name}. No alert received within duration: %s`
var ZeroSeconds = "0s"
var ctx context.Context
var DontReportZero = false

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
	Value                    float64   `json:"_value"`
	Version                  int       `json:"_version"`
	DeviceId                 string    `json:"deviceID" required:"true"`
	QueryField               string    `json:"query_field" required:"true"`
	_                        struct{}  `json:"-" additionalProperties:"true"`
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
		Value: i.Value,
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
	var check domain.Check
	var err error
	if ar.AlertType == alertstore.InactiveSensor {
		check, err = i.createDeadmanCheck(ar)
	} else {
		check, err = i.createThresholdCheck(ar)
	}
	if err != nil {
		return "", fmt.Errorf("create check: %v", err)
	}
	params := domain.CreateCheckAllParams{
		Body: check,
	}
	nc, err := i.client.APIClient().CreateCheck(ctx, &params)
	if err != nil {
		return "", fmt.Errorf("createCheck: %v", err)
	}

	var alertId, taskId string
	if nc.Type() == "threshold" {
		tc, ok := nc.(*domain.ThresholdCheck)
		if !ok {
			return "", fmt.Errorf("invalid threshold check created")
		}
		alertId = *tc.CheckBase.Id
		taskId = *tc.CheckBase.TaskID
		task, err := i.getTask(taskId)
		if err != nil {
			return "", fmt.Errorf(
				"getting task for alert %s, task: %s, error: %v", alertId, taskId, err)
		}
		task.Flux = i.fluxInjectQueryFieldValue(task.Flux, ar.QueryField)
		if err = i.updateTask(task); err != nil {
			return "", fmt.Errorf("updateTask: %v", err)
		}
	} else {
		dc, ok := nc.(*domain.DeadmanCheck)
		if !ok {
			return "", fmt.Errorf("invalid deadman check created")
		}
		alertId = *dc.CheckBase.Id
	}
	return alertId, err
}

func (i *Influx) UpdateAlertRule(ar alertstore.AlertRule) error {
	check, err := i.createThresholdCheck(ar)
	if err != nil {
		return err
	}
	updateParams := domain.PutChecksIDAllParams{
		CheckID: ar.AlertId,
		Body:    check,
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

func (i *Influx) getAlertRule(alertId string) (domain.Check, error) {
	gp := domain.GetChecksIDAllParams{CheckID: alertId}
	return i.client.APIClient().GetChecksID(ctx, &gp)
}

func (i *Influx) getTask(taskId string) (*domain.Task, error) {
	tf := domain.GetTasksIDAllParams{TaskID: taskId}
	dt, err := i.client.APIClient().GetTasksID(ctx, &tf)
	if err != nil {
		return nil, fmt.Errorf("error getting task %s, error: %v", taskId, err)
	}
	return dt, nil
}

func (i *Influx) fluxInjectQueryFieldValue(flux, queryField string) string {
	mqf := fmt.Sprintf(MapQueryField, queryField)
	fluxSlice := strings.Split(flux, "\n")
	slices.Reverse(fluxSlice)
	for i, stmt := range fluxSlice {
		if !strings.Contains(stmt, V1FieldAsCols) {
			continue
		}
		fluxSlice = slices.Insert(fluxSlice, i, mqf)
	}
	slices.Reverse(fluxSlice)
	return strings.Join(fluxSlice, " ")
}

func (i *Influx) updateTask(task *domain.Task) error {
	_, err := i.client.TasksAPI().UpdateTask(ctx, task)
	return err
}

func (i *Influx) PrepareDb(...alertstore.AlertRule) error {
	return nil
}

func (i *Influx) GetSnapshot() int {
	return 0
}

func (i *Influx) createThresholdCheck(ar alertstore.AlertRule) (*domain.ThresholdCheck, error) {
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
			return nil, fmt.Errorf("first boundary is not smaller than the second.")
		}
		rang = domain.RangeThreshold{
			ThresholdBase: domain.ThresholdBase{Level: &lc},
			Max:           ar.RangeValues[1],
			Min:           ar.RangeValues[0],
		}
		thresholds = append(thresholds, &rang)
	case alertstore.InactiveSensor:
		return nil, fmt.Errorf("inactive sensor check requested.")
	default:
		return nil, fmt.Errorf("unknown alert type: %d", ar.AlertType)
	}

	key := "query_field"
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
			StatusMessageTemplate: &ThresholdStatusMessage,
			Tags: &[]struct {
				Key   *string `json:"key,omitempty"`
				Value *string `json:"value,omitempty"`
			}{{Key: &key, Value: &ar.QueryField}},
		},
		Thresholds: &thresholds,
	}
	return &check, nil
}

func (i *Influx) createDeadmanCheck(ar alertstore.AlertRule) (*domain.DeadmanCheck, error) {
	lc := domain.CheckStatusLevelCRIT
	hydratedFlux := fmt.Sprintf(Flux, i.org, ar.DeviceId, ar.QueryField, ar.Duration)
	hydratedStatusMessage := fmt.Sprintf(DeadmanStatusMessage, ar.Duration)
	check := domain.DeadmanCheck{
		CheckBaseExtend: domain.CheckBaseExtend{
			CheckBase: domain.CheckBase{
				Name:   ar.Name,
				OrgID:  i.orgId,
				Query:  domain.DashboardQuery{Text: &hydratedFlux},
				Status: domain.TaskStatusTypeActive,
			},
			Every:                 &ar.Duration,
			Offset:                &ZeroSeconds,
			StatusMessageTemplate: &hydratedStatusMessage,
		},
		ReportZero: &DontReportZero,
		Level:      &lc,
		TimeSince:  &ar.Duration,
		StaleTime:  &ZeroSeconds,
	}
	return &check, nil
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
