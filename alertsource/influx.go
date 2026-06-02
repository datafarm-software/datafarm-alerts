package alertsource

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	cfy "github.com/geraud22/config-from-yaml"
	"github.com/geraud22/datafarm-alerts/alertstore"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/domain"
)

var NoRangeValues = errors.New("no range values")

const Flux string = `from(bucket: "%s")
	|> range(start: v.timeRangeStart, stop: v.timeRangeStop)
    |> filter(fn: (r) => r["deviceID"] == "%s")
    |> filter(fn: (r) => r["_field"] == "%s")
    |> aggregateWindow(every: %s, fn: last, createEmpty: false)`

const CustomCheckMap = `|> map(fn: (r) => ({r with %s}))`
const CustomCheckFormat = `%s: r.%s`
const V1FieldAsCols = `|> v1["fieldsAsCols"]()`

var QueryField = "query_field"
var ThresholdStatusMessage string = `Check: ${r._check_name} State: ${r._level}.`
var DeadmanStatusMessage string = `Check: ${r._check_name}. No alert received within duration: %s`
var ZeroSeconds = "0s"
var ctx context.Context
var DontReportZero = false
var DeadmanEveryMax = int64(5 * 60)
var ThresholdEveryMax = int64(15 * 60)

type InfluxAlert struct {
	Id              string    `json:"_check_id" required:"true"`
	Level           string    `json:"_level" required:"true"`
	SensorTimestamp time.Time `json:"_sensor_timestamp"`
	Time            time.Time `json:"_time" required:"true"`
	Value           float64   `json:"_value" required:"false"`
	DeviceId        string    `json:"deviceID"`
	QueryField      string    `json:"query_field"`
	_               struct{}  `json:"-" additionalProperties:"true"`
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
		Time:  i.SensorTimestamp,
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
		return nil, fmt.Errorf("api client: %v", err)
	}
	org, err := client.OrganizationsAPI().FindOrganizationByName(ctx, opts.Org)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("org api: %v", err)
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

type localCheck interface {
	domain.Check
	AlertId() (string, error)
	TaskId() (string, error)
}

type localCheckImpl struct{ check domain.Check }

func (l localCheckImpl) Type() string {
	return l.check.Type()
}

func (l localCheckImpl) AlertId() (alertId string, err error) {
	checkType := l.check.Type()
	switch checkType {
	case "threshold":
		tc, ok := l.check.(*domain.ThresholdCheck)
		if !ok {
			return alertId, fmt.Errorf("invalid threshold check created")
		}
		alertId = *tc.CheckBase.Id
	case "deadman":
		dc, ok := l.check.(*domain.DeadmanCheck)
		if !ok {
			return "", fmt.Errorf("invalid deadman check created")
		}
		alertId = *dc.CheckBase.Id
	default:
		return alertId, fmt.Errorf("unknown check type: %s", checkType)
	}
	return alertId, nil
}

func (l localCheckImpl) TaskId() (taskId string, err error) {
	checkType := l.check.Type()
	switch checkType {
	case "threshold":
		tc, ok := l.check.(*domain.ThresholdCheck)
		if !ok {
			return taskId, fmt.Errorf("invalid threshold check created")
		}
		taskId = *tc.CheckBase.TaskID
	case "deadman":
		dc, ok := l.check.(*domain.DeadmanCheck)
		if !ok {
			return "", fmt.Errorf("invalid deadman check created")
		}
		taskId = *dc.CheckBase.TaskID
	default:
		return taskId, fmt.Errorf("unknown check type: %s", checkType)
	}
	return taskId, nil
}

func (i *Influx) CreateAlertRule(ar alertstore.AlertRule) (string, error) {
	check, err := i.domainCheck(ar)
	if err != nil {
		return "", err
	}
	params := domain.CreateCheckAllParams{
		Body: check,
	}
	nc, err := i.client.APIClient().CreateCheck(ctx, &params)
	if err != nil {
		return "", fmt.Errorf("createCheck: %v", err)
	}
	lc := localCheck(localCheckImpl{nc})
	taskId, err := lc.TaskId()
	if err != nil {
		return "", fmt.Errorf("taskId: %v", err)
	}
	alertId, err := lc.AlertId()
	if err != nil {
		return "", fmt.Errorf("alertId: %v", err)
	}
	if lc.Type() == "threshold" {
		if err = i.addCustomValuesToThreshold(taskId, ar); err != nil {
			return alertId, fmt.Errorf("addCustomValuesToThreshold: %v", err)
		}
	}
	return alertId, nil
}

func (i *Influx) addCustomValuesToThreshold(taskId string, ar alertstore.AlertRule) error {
	task, err := i.getTask(taskId)
	if err != nil {
		return fmt.Errorf(
			"getting task: %s, error: %v", taskId, err)
	}
	customVals := []CustomCheckValue{
		{customName: "_value", fluxValue: ar.QueryField},
		{customName: "_sensor_timestamp", fluxValue: "_time"},
	}
	task.Flux = i.addCustomCheckValue(task.Flux, customVals...)
	if err = i.updateTask(task); err != nil {
		return fmt.Errorf("updateTask: %v", err)
	}
	return nil
}

func (i *Influx) UpdateAlertRule(ar alertstore.AlertRule) error {
	check, err := i.domainCheck(ar)
	if err != nil {
		return err
	}
	updateParams := domain.PutChecksIDAllParams{
		CheckID: ar.AlertId,
		Body:    check,
	}
	check, err = i.client.APIClient().PutChecksID(ctx, &updateParams)
	if err != nil {
		return fmt.Errorf("PutChecks: %v", err)
	}
	lc := localCheck(localCheckImpl{check})
	taskId, err := lc.TaskId()
	if err != nil {
		return fmt.Errorf("taskId: %v", err)
	}
	if lc.Type() == "threshold" {
		if err = i.addCustomValuesToThreshold(taskId, ar); err != nil {
			return fmt.Errorf("addCustomValuesToThreshold: %v", err)
		}
	}
	return nil
}

func (i *Influx) domainCheck(ar alertstore.AlertRule) (domain.Check, error) {
	var check domain.Check
	var err error
	if ar.AlertType == alertstore.InactiveSensor {
		check, err = i.createDeadmanCheck(ar)
	} else {
		check, err = i.createThresholdCheck(ar)
	}
	return check, err
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

type CustomCheckValue struct {
	customName string
	fluxValue  string
}

func (c CustomCheckValue) String() string {
	return fmt.Sprintf(CustomCheckFormat, c.customName, c.fluxValue)
}

func (i *Influx) addCustomCheckValue(flux string, customVals ...CustomCheckValue) string {
	fluxSlice := strings.Split(flux, "\n")
	slices.Reverse(fluxSlice)
	cvb := strings.Builder{}
	newFlux := make([]string, 0, len(fluxSlice)+len(customVals))
	for i, c := range customVals {
		if i > 0 {
			cvb.WriteString(",")
		}
		fmt.Fprintf(&cvb, CustomCheckFormat, c.customName, c.fluxValue)
	}
	customCheckMap := fmt.Sprintf(CustomCheckMap, cvb.String())
	for _, stmt := range fluxSlice {
		if strings.Contains(stmt, V1FieldAsCols) {
			newFlux = append(newFlux, customCheckMap)
		}
		newFlux = append(newFlux, stmt)
	}
	slices.Reverse(newFlux)
	return strings.Join(newFlux, " ")
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
	var al domain.CheckStatusLevel
	switch ar.AlertLevel {
	case alertstore.Warn:
		al = domain.CheckStatusLevelWARN
	case alertstore.Crit:
		al = domain.CheckStatusLevelCRIT
	default:
		return nil, fmt.Errorf("alert rule level invalid: %d - %s",
			ar.AlertLevel,
			alertstore.AlertLevelString[ar.AlertLevel])
	}
	allValues := true
	threshold := "threshold"
	thresholdTrue := "true"

	if len(ar.RangeValues) < 1 {
		return nil, NoRangeValues
	}
	switch ar.AlertType {
	case alertstore.LessThan:
		greater = domain.GreaterThreshold{
			Value:         ar.RangeValues[0],
			ThresholdBase: domain.ThresholdBase{Level: &lo},
		}
		lesser = domain.LesserThreshold{
			Value:         ar.RangeValues[0],
			ThresholdBase: domain.ThresholdBase{Level: &al},
		}
		thresholds = append(thresholds, &lesser, &greater)

	case alertstore.GreaterThan:
		greater = domain.GreaterThreshold{
			Value:         ar.RangeValues[0],
			ThresholdBase: domain.ThresholdBase{Level: &al, AllValues: &allValues},
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
			ThresholdBase: domain.ThresholdBase{Level: &al},
			Max:           ar.RangeValues[1],
			Min:           ar.RangeValues[0],
		}
		thresholds = append(thresholds, &rang)
	case alertstore.InactiveSensor:
		return nil, fmt.Errorf("inactive sensor check requested.")
	default:
		return nil, fmt.Errorf("unknown alert type: %d", ar.AlertType)
	}
	every, err := parseInterval(ar.Duration, ar.AlertType)
	if err != nil {
		return nil, fmt.Errorf("parseInterval: %v", err)
	}
	hydratedFlux := fmt.Sprintf(Flux, i.org, ar.DeviceId, ar.QueryField, ar.Duration)
	check = domain.ThresholdCheck{
		CheckBaseExtend: domain.CheckBaseExtend{
			CheckBase: domain.CheckBase{
				Name:   ar.Name,
				OrgID:  i.orgId,
				Query:  domain.DashboardQuery{Text: &hydratedFlux},
				Status: domain.TaskStatusTypeActive,
			},
			Every:                 &every,
			Offset:                &ZeroSeconds,
			StatusMessageTemplate: &ThresholdStatusMessage,
			Tags: &[]struct {
				Key   *string `json:"key,omitempty"`
				Value *string `json:"value,omitempty"`
			}{
				{Key: &QueryField, Value: &ar.QueryField},
				{Key: &threshold, Value: &thresholdTrue},
			},
		},
		Thresholds: &thresholds,
	}
	return &check, nil
}

func (i *Influx) createDeadmanCheck(ar alertstore.AlertRule) (*domain.DeadmanCheck, error) {
	lc := domain.CheckStatusLevelCRIT
	hydratedFlux := fmt.Sprintf(Flux, i.org, ar.DeviceId, ar.QueryField, ar.Duration)
	hydratedStatusMessage := fmt.Sprintf(DeadmanStatusMessage, ar.Duration)
	every, err := parseInterval(ar.Duration, ar.AlertType)
	if err != nil {
		return nil, err
	}
	staleTime, err := parseStaleTime(ar.Duration)
	if err != nil {
		return nil, err
	}
	deadman := "deadman"
	deadmanTrue := "true"
	check := domain.DeadmanCheck{
		CheckBaseExtend: domain.CheckBaseExtend{
			CheckBase: domain.CheckBase{
				Name:   ar.Name,
				OrgID:  i.orgId,
				Query:  domain.DashboardQuery{Text: &hydratedFlux},
				Status: domain.TaskStatusTypeActive,
			},
			Every:                 &every,
			Offset:                &ZeroSeconds,
			StatusMessageTemplate: &hydratedStatusMessage,
			Tags: &[]struct {
				Key   *string `json:"key,omitempty"`
				Value *string `json:"value,omitempty"`
			}{
				{Key: &QueryField, Value: &ar.QueryField},
				{Key: &deadman, Value: &deadmanTrue},
			},
		},
		ReportZero: &DontReportZero,
		Level:      &lc,
		TimeSince:  &ar.Duration,
		StaleTime:  &staleTime,
	}
	return &check, nil
}

func parseInterval(duration string, t alertstore.AlertType) (string, error) {
	unit := duration[len(duration)-1]
	timeDuration := duration[:len(duration)-1]
	tdInt, err := strconv.Atoi(timeDuration)
	if err != nil {
		return "", fmt.Errorf(
			"could not parse timeduration: %s, error: %v", timeDuration, err)
	}
	var everySeconds int64
	switch unit {
	case 's':
		everySeconds = int64(tdInt)
	case 'm':
		everySeconds = int64(tdInt * 60)
	case 'h':
		everySeconds = int64(tdInt * 60 * 60)
	case 'd':
		everySeconds = int64(tdInt * 24 * 60 * 60)
	case 'w':
		everySeconds = int64(tdInt * 7 * 24 * 60 * 60)
	default:
		return "", fmt.Errorf("unknown relative time unit: %c", unit)
	}
	everyInt := max(int64(everySeconds/2), 1)
	every := fmt.Sprintf("%ds", everyInt)
	if t == alertstore.InactiveSensor {
		if everyInt >= DeadmanEveryMax {
			every = "5m"
		}
	} else {
		if everyInt >= ThresholdEveryMax {
			every = "15m"
		}
	}
	return every, nil
}

func parseStaleTime(duration string) (string, error) {
	unit := duration[len(duration)-1]
	timeDuration := duration[:len(duration)-1]
	tdInt, err := strconv.Atoi(timeDuration)
	if err != nil {
		return "", fmt.Errorf(
			"could not parse timeduration: %s, error: %v", timeDuration, err)
	}
	staleInt := tdInt * 2
	staleTime := fmt.Sprintf("%d%s", staleInt, string(unit))
	return staleTime, nil
}

var once sync.Once
var testingInfluxOpts InfluxOpts

type TestingInflux struct {
	influx             AlertSource
	testingAlertsCount int
	testingAlertIdsMap map[string]string
}

func NewTestingInflux(configPath string) (AlertSource, error) {
	var topErr error
	once.Do(func() {
		config, err := os.ReadFile(configPath)
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
