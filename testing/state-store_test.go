package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/geraud22/sensordata-alerts/alertsource"
	"github.com/geraud22/sensordata-alerts/alertstore"
	"github.com/geraud22/sensordata-alerts/app"
	"github.com/geraud22/sensordata-alerts/notifier"
	"github.com/google/go-cmp/cmp"
)

var a = &app.App{}
var EmptyInboxes = map[string][]string{}
var EmptyAlertStore = &alertstore.Schema{}

const UnregisteredQueryField = "humidity"
const RegisteredQueryField = "temperature"
const UnregisteredDeviceId = "device2"
const RegisteredDeviceId = "device1"
const SomeDuration = (24 * time.Hour)
const AlertId = "alert1"
const UserEmail = "user1@alert.com"

var AlertReceived = time.Now()
var AfterDuration = time.Now().Add(25 * time.Hour)
var WithinDuration = time.Now().Add(6 * time.Hour)

func TestNotifyCrit(t *testing.T) {
	var got bool
	tests := map[string]struct {
		want            bool
		alert, previous *alertstore.AlertState
	}{
		"no change within duration, so do nothing": {
			want: false,
			alert: &alertstore.AlertState{
				Level:    alertstore.Crit,
				Time:     WithinDuration,
				Duration: SomeDuration,
			},
			previous: &alertstore.AlertState{
				Level:    alertstore.Crit,
				Time:     AlertReceived,
				Duration: SomeDuration,
			},
		},
		"still crit after duration, so notify": {
			want: true,
			alert: &alertstore.AlertState{
				Level:    alertstore.Crit,
				Time:     AfterDuration,
				Duration: SomeDuration,
			},
			previous: &alertstore.AlertState{
				Level:    alertstore.Crit,
				Time:     AlertReceived,
				Duration: SomeDuration,
			},
		},
		"change to crit, so notify": {
			want: true,
			alert: &alertstore.AlertState{
				Level: alertstore.Crit,
			},
			previous: &alertstore.AlertState{
				Level: alertstore.Ok,
			},
		},
		"change from crit to ok, so do nothing": {
			want: false,
			alert: &alertstore.AlertState{
				Level: alertstore.Ok,
			},
			previous: &alertstore.AlertState{
				Level: alertstore.Crit,
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got = a.ShouldNotifyCrit(tc.alert, tc.previous)
			if got != tc.want {
				t.Fatalf("tc.want = %v, got = %v", tc.want, got)
			}
		})
	}
}

func TestRespondToAlert(t *testing.T) {
	tests := map[string]struct {
		wantUserInboxes                map[string][]string
		mockNotifierStore              map[any]any
		mockAlertStore, wantAlertStore *alertstore.Schema
		alert                          alertsource.InfluxAlert
	}{
		"send alert because still crit after duration": {
			wantUserInboxes: map[string][]string{
				UserEmail: {
					"New alert message stating crit level.",
				},
			},
			mockAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Duration: SomeDuration,
						Message:  "Already crit.",
						Level:    alertstore.Crit,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         AlertId,
				Level:      "crit",
				Message:    "New alert message stating crit level.",
				Time:       AfterDuration,
				DeviceId:   RegisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
			wantAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Duration: SomeDuration,
						Message:  "New alert message stating crit level.",
						Level:    alertstore.Crit,
					},
				},
			},
		},

		"no notification because duration has not elapsed": {
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: &alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Duration: SomeDuration,
						Message:  "Already crit.",
						Level:    alertstore.Crit,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         AlertId,
				Level:      "crit",
				Message:    "Alert message stating crit level.",
				Time:       WithinDuration,
				DeviceId:   RegisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
			wantAlertStore: &alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Duration: SomeDuration,
						Message:  "Already crit.",
						Level:    alertstore.Crit,
					},
				},
			},
		},

		"send notification because alert level changing from ok to crit": {
			wantUserInboxes: map[string][]string{
				UserEmail: {
					"Status is now crit.",
				},
			},
			mockAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Duration: SomeDuration,
						Message:  "Status Ok.",
						Level:    alertstore.Crit,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         AlertId,
				Level:      "crit",
				Message:    "Alert message stating crit level.",
				Time:       WithinDuration,
				DeviceId:   RegisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
			wantAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Duration: SomeDuration,
						Message:  "Status is now crit.",
						Level:    alertstore.Crit,
					},
				},
			},
		},

		"no notification because alert on unregistered queryField": {
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: &alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         AlertId,
				Level:      "crit",
				Message:    "Alert that should be ignored.",
				DeviceId:   RegisteredDeviceId,
				QueryField: UnregisteredQueryField,
			},
			wantAlertStore: &alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
			},
		},

		"no notification because alert on unregistered deviceId": {
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: &alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         AlertId,
				Level:      "crit",
				Message:    "Alert message stating crit level.",
				DeviceId:   UnregisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
			wantAlertStore: &alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
			},
		},
	}
	var n *notifier.MockNotifier
	var as *alertstore.MockAlertStore
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			n = notifier.NewMockNotifier(tc.mockNotifierStore)
			as = alertstore.NewMockAlertStore(tc.mockAlertStore)
			a.AlertStore = as
			a.Notifier = n
			a.RespondToAlert(
				context.Background(),
				&struct{ Body alertsource.InfluxAlert }{tc.alert},
			)
			if diff := cmp.Diff(tc.wantUserInboxes, n.GetUserInboxes()); diff != "" {
				t.Fatalf("user inboxes mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantAlertStore, as.GetSnapshot()); diff != "" {
				t.Fatalf("alertstore mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCreateAlertRule(t *testing.T) {
	tests := map[string]struct {
		wantErr                        bool
		wantResponse                   alertstore.CreatedRule
		wantStatus                     int
		wantAlertStore, mockAlertStore *alertstore.Schema
		mockAlertSource                map[any]any
		alertRule                      alertstore.AlertRule
	}{

		"successfully create a less than alert rule": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			wantAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []int{5},
						Duration:    "24h",
						AlertRange:  alertstore.LessThan,
					},
				},
			},
			alertRule: alertstore.AlertRule{
				RangeValues:    []int{5},
				Duration:       "24h",
				EmailAddresses: []string{UserEmail},
				DeviceId:       "device1",
				QueryField:     "temperature",
				AlertRange:     alertstore.LessThan,
			},
			wantResponse: alertstore.CreatedRule{
				AlertId: AlertId,
			},
		},

		"invalid range on less than alert": {
			wantErr:        true,
			wantStatus:     http.StatusUnprocessableEntity,
			wantAlertStore: EmptyAlertStore,
			alertRule: alertstore.AlertRule{
				RangeValues: []int{5, 10},
				AlertRange:  alertstore.LessThan,
			},
		},

		"invalid range on range alert": {
			wantErr:        true,
			wantStatus:     http.StatusUnprocessableEntity,
			wantAlertStore: EmptyAlertStore,
			alertRule: alertstore.AlertRule{
				RangeValues: []int{5},
				AlertRange:  alertstore.Range,
			},
		},
		"invalid alert rule": {
			wantErr:        true,
			wantStatus:     http.StatusUnprocessableEntity,
			wantAlertStore: EmptyAlertStore,
			alertRule: alertstore.AlertRule{
				EmailAddresses: []string{},
				Duration:       "",
				AlertRange:     alertstore.Range,
			},
		},

		"identical alert already exists": {
			wantErr:    true,
			wantStatus: http.StatusConflict,
			wantAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []int{5},
						Duration:    "24h",
						AlertRange:  alertstore.LessThan,
					},
				},
			},
			mockAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						RangeValues: []int{5},
						Duration:    "24h",
						AlertRange:  alertstore.LessThan,
					},
				},
			},
			alertRule: alertstore.AlertRule{
				RangeValues: []int{5},
				Duration:    "24h",
				DeviceId:    RegisteredDeviceId,
				QueryField:  RegisteredQueryField,
				AlertRange:  alertstore.LessThan,
			},
		},

		"similar alert rule exists, but new one on different deviceId queryField combination": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			wantResponse: alertstore.CreatedRule{
				AlertId: "alert2",
			},
			wantAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
					{AlertId: "alert2", Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: "humidity",
						AlertId:    "alert2",
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []int{5},
						Duration:    "24h",
						AlertRange:  alertstore.LessThan,
					},
					{
						AlertId:     "alert2",
						RangeValues: []int{5},
						Duration:    "24h",
						AlertRange:  alertstore.LessThan,
					},
				},
			},
			mockAlertSource: map[any]any{
				"alertsCount": 1,
			},
			mockAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []int{5},
						Duration:    "24h",
						AlertRange:  alertstore.LessThan,
					},
				},
			},
			alertRule: alertstore.AlertRule{
				RangeValues:    []int{5},
				EmailAddresses: []string{UserEmail},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     "humidity",
				AlertRange:     alertstore.LessThan,
			},
		},

		"multiple alerts on deviceId and queryField combination": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			wantResponse: alertstore.CreatedRule{
				AlertId: "alert2",
			},
			wantAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
					{AlertId: "alert2", Email: UserEmail},
					{AlertId: "alert2", Email: "user2@alert.com"},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    "alert2",
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []int{5},
						Duration:    "24h",
						AlertRange:  alertstore.LessThan,
					},
					{
						AlertId:     "alert2",
						RangeValues: []int{10, 20},
						Duration:    "24h",
						AlertRange:  alertstore.Range,
					},
				},
			},
			mockAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []int{5},
						Duration:    "24h",
						AlertRange:  alertstore.LessThan,
					},
				},
			},
			mockAlertSource: map[any]any{
				"alertsCount": 1,
			},
			alertRule: alertstore.AlertRule{
				EmailAddresses: []string{UserEmail, "user2@alert.com"},
				RangeValues:    []int{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertRange:     alertstore.Range,
			},
		},
	}

	var as *alertstore.MockAlertStore
	var source *alertsource.MockAlertSource
	var err error
	var got alertstore.CreatedRule
	_, api := humatest.New(t)
	a.RegisterHumaOperations(api)
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			as = alertstore.NewMockAlertStore(tc.mockAlertStore)
			a.AlertStore = as
			source = alertsource.NewMockAlertSource(tc.mockAlertSource)
			a.AlertSource = source
			resp := api.Post("/alerts", tc.alertRule)
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatusCode: %d, response code: %d", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				if err = json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
					t.Fatalf("unmarshalling: %s", err)
				}
				if diff := cmp.Diff(tc.wantResponse, got); diff != "" {
					t.Fatalf("response mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := map[string]struct {
		wantErr bool
		input   string
		want    time.Duration
	}{
		"1 second": {
			wantErr: false,
			input:   "1s",
			want:    time.Duration(1 * time.Second),
		},
		"1 minute": {
			wantErr: false,
			input:   "1m",
			want:    time.Duration(1 * time.Minute),
		},
		"1 hour": {
			wantErr: false,
			input:   "1h",
			want:    time.Duration(1 * time.Hour),
		},
		"1 day": {
			wantErr: false,
			input:   "1d",
			want:    time.Duration(24 * time.Hour),
		},
		"24 hours": {
			wantErr: false,
			input:   "24h",
			want:    time.Duration(24 * time.Hour),
		},
		"invalid duration": {
			wantErr: true,
			input:   "invalid",
		},
		"invalid empty duration": {
			wantErr: true,
			input:   "",
		},
	}
	var got time.Duration
	var err error
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err = alertstore.ParseDuration(tc.input)
			if tc.wantErr != (err != nil) {
				t.Fatalf("wantErr: %v, got: %v", tc.want, err)
			}
			if !tc.wantErr {
				if tc.want != got {
					t.Fatalf("want: %v, got: %v", tc.want, got)
				}
			}
		})
	}
}

func TestUpdateAlertRule(t *testing.T) {
	tests := map[string]struct {
		wantErr                        bool
		wantStatus                     int
		alertId                        string
		alertRule                      alertstore.AlertRule
		mockAlertStore, wantAlertStore *alertstore.Schema
	}{

		"update the range values of an alert": {
			wantStatus: http.StatusOK,
			alertId:    AlertId,
			alertRule: alertstore.AlertRule{
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []int{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertRange:     alertstore.Range,
			},
			mockAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []int{43, 90},
						Duration:    "24h",
						AlertRange:  alertstore.Range,
					},
				},
			},
			wantAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []int{10, 20},
						Duration:    "24h",
						AlertRange:  alertstore.Range,
					},
				},
			},
		},

		"add more emails to the recipient list of an alert rule": {
			wantStatus: http.StatusOK,
			alertId:    AlertId,
			alertRule: alertstore.AlertRule{
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail, "user2@alert.com"},
				RangeValues:    []int{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertRange:     alertstore.Range,
			},
			mockAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []int{10, 20},
						Duration:    "24h",
						AlertRange:  alertstore.Range,
					},
				},
			},
			wantAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
					{AlertId: AlertId, Email: "user2@alert.com"},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []int{10, 20},
						Duration:    "24h",
						AlertRange:  alertstore.Range,
					},
				},
			},
		},

		"ignore duplicate emails in recipient list": {
			wantStatus: http.StatusOK,
			alertId:    AlertId,
			alertRule: alertstore.AlertRule{
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail, UserEmail},
				RangeValues:    []int{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertRange:     alertstore.Range,
			},
			mockAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []int{10, 20},
						Duration:    "24h",
						AlertRange:  alertstore.Range,
					},
				},
			},
			wantAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []int{10, 20},
						Duration:    "24h",
						AlertRange:  alertstore.Range,
					},
				},
			},
		},

		"error when trying to update non existent alert rule": {
			wantStatus: http.StatusNotFound,
			wantErr:    true,
			alertId:    AlertId,
			alertRule: alertstore.AlertRule{
				EmailAddresses: []string{UserEmail},
				RangeValues:    []int{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertRange:     alertstore.Range,
			},
		},

		"invalid: 1 range value on a range type alert check": {
			wantStatus: http.StatusBadRequest,
			wantErr:    true,
			alertId:    AlertId,
			alertRule: alertstore.AlertRule{
				EmailAddresses: []string{UserEmail},
				RangeValues:    []int{10},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertRange:     alertstore.Range,
			},
		},

		"invalid: 2 range values on a less than type alert check": {
			wantStatus: http.StatusBadRequest,
			wantErr:    true,
			alertId:    AlertId,
			alertRule: alertstore.AlertRule{
				EmailAddresses: []string{UserEmail},
				RangeValues:    []int{50, 90},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertRange:     alertstore.LessThan,
			},
		},
	}

	var as *alertstore.MockAlertStore
	var err error
	var got alertstore.AlertRule
	_, api := humatest.New(t)
	a.RegisterHumaOperations(api)
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			as = alertstore.NewMockAlertStore(tc.mockAlertStore)
			a.AlertStore = as
			resp := api.Put(fmt.Sprintf("/alerts/%s", tc.alertId), tc.alertRule)
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatusCode: %d, response code: %d", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				if err = json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
					t.Fatalf("unmarshalling: %s", err)
				}
				if diff := cmp.Diff(tc.alertRule, got); diff != "" {
					t.Fatalf("response mismatch (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(tc.wantAlertStore, as.GetSnapshot()); diff != "" {
					t.Fatalf("alertstore mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestGetAlertRule(t *testing.T) {
	tests := map[string]struct {
		wantErr        bool
		want           alertstore.AlertRule
		wantStatus     int
		alertId        string
		mockAlertStore *alertstore.Schema
	}{

		"successfully get a stored alert": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			alertId:    AlertId,
			mockAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						RangeValues: []int{10, 20},
						AlertRange:  alertstore.Range,
					},
				},
			},
			want: alertstore.AlertRule{
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []int{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertRange:     alertstore.Range,
			},
		},
		"alert doesn't exist": {
			wantErr:        true,
			want:           alertstore.AlertRule{},
			wantStatus:     404,
			alertId:        "alert2",
			mockAlertStore: &alertstore.Schema{},
		},
	}

	var err error
	var got alertstore.AlertRule
	_, api := humatest.New(t)
	a.RegisterHumaOperations(api)
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a.AlertStore = alertstore.NewMockAlertStore(tc.mockAlertStore)
			resp := api.Get(fmt.Sprintf("/alerts/%s", tc.alertId))
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatusCode: %d, response code: %d", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				if err = json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
					t.Fatalf("unmarshalling: %s", err)
				}
				if diff := cmp.Diff(tc.want, got); diff != "" {
					t.Fatalf("response mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}
