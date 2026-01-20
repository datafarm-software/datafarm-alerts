package testing

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/geraud22/sensordata-alerts/alertsource"
	"github.com/geraud22/sensordata-alerts/alertstore"
	"github.com/geraud22/sensordata-alerts/app"
	"github.com/geraud22/sensordata-alerts/notifier"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var a = &app.App{}
var EmptyInboxes = map[string][]notifier.AlertMessage{}
var EmptyAlertStore = alertstore.Schema{}

const UnregisteredQueryField = "humidity"
const RegisteredQueryField = "temperature"
const UnregisteredDeviceId = "device2"
const RegisteredDeviceId = "device1"
const SomeDuration = (24 * time.Hour)
const AlertId = "alert1"
const UserEmail = "user1@alert.com"
const RegisteredAlertName = "Temperature Check"

var AlertReceived = time.Date(1994, 6, 24, 6, 0, 0, 0, time.UTC)
var AfterDuration = AlertReceived.Add(25 * time.Hour)
var WithinDuration = AlertReceived.Add(6 * time.Hour)

func TestShouldNotifyCrit(t *testing.T) {
	var got bool
	tests := map[string]struct {
		want            bool
		alert, previous alertstore.AlertState
	}{
		"no change within duration, so do nothing": {
			want: false,
			alert: alertstore.AlertState{
				Level:    alertstore.Crit,
				Time:     WithinDuration,
				Duration: SomeDuration,
			},
			previous: alertstore.AlertState{
				Level:    alertstore.Crit,
				Time:     AlertReceived,
				Duration: SomeDuration,
			},
		},
		"still crit after duration, so notify": {
			want: true,
			alert: alertstore.AlertState{
				Level:    alertstore.Crit,
				Time:     AfterDuration,
				Duration: SomeDuration,
			},
			previous: alertstore.AlertState{
				Level:    alertstore.Crit,
				Time:     AlertReceived,
				Duration: SomeDuration,
			},
		},
		"change to crit, so notify": {
			want: true,
			alert: alertstore.AlertState{
				Level: alertstore.Crit,
			},
			previous: alertstore.AlertState{
				Level: alertstore.Ok,
			},
		},
		"change from crit to ok, so do nothing": {
			want: false,
			alert: alertstore.AlertState{
				Level: alertstore.Ok,
			},
			previous: alertstore.AlertState{
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
		wantStatus                     int
		wantUserInboxes                map[string][]notifier.AlertMessage
		mockNotifierStore              map[any]any
		mockAlertStore, wantAlertStore alertstore.Schema
		alert                          alertsource.InfluxAlert
	}{

		"send alert because still crit after duration": {
			wantStatus: http.StatusNoContent,
			wantUserInboxes: map[string][]notifier.AlertMessage{
				UserEmail: {
					{
						Name:       RegisteredAlertName,
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						Level:      alertstore.Crit,
						Timestamp:  AfterDuration,
					},
				},
			},
			mockAlertStore: alertstore.Schema{
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
						AlertId:  AlertId,
						Duration: "24h",
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Duration: SomeDuration,
						Level:    alertstore.Crit,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         AlertId,
				Level:      "crit",
				Name:       RegisteredAlertName,
				Time:       AfterDuration,
				DeviceId:   RegisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
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
						AlertId:  AlertId,
						Duration: "24h",
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived.Add(SomeDuration).Add(1 * time.Hour),
						Duration: SomeDuration,
						Level:    alertstore.Crit,
					},
				},
			},
		},

		"no notification because duration has not elapsed": {
			wantStatus:      http.StatusNoContent,
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:  AlertId,
						Duration: "24h",
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Duration: SomeDuration,
						Level:    alertstore.Crit,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         AlertId,
				Name:       RegisteredAlertName,
				Level:      "crit",
				Time:       WithinDuration,
				DeviceId:   RegisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:  AlertId,
						Duration: "24h",
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Duration: SomeDuration,
						Level:    alertstore.Crit,
					},
				},
			},
		},

		"send notification because alert level changing from ok to crit": {
			wantStatus: http.StatusNoContent,
			wantUserInboxes: map[string][]notifier.AlertMessage{
				UserEmail: {
					{
						Name:       RegisteredAlertName,
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						Level:      alertstore.Crit,
						Timestamp:  WithinDuration,
					},
				},
			},
			mockAlertStore: alertstore.Schema{
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
						AlertId:  AlertId,
						Duration: "24h",
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Duration: SomeDuration,
						Level:    alertstore.Ok,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         AlertId,
				Level:      "crit",
				Name:       RegisteredAlertName,
				Time:       WithinDuration,
				DeviceId:   RegisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
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
						AlertId:  AlertId,
						Duration: "24h",
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     WithinDuration,
						Duration: SomeDuration,
						Level:    alertstore.Crit,
					},
				},
			},
		},

		"no notification because alert on unregistered queryField": {
			wantStatus:      http.StatusNotFound,
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:  AlertId,
						Duration: "24h",
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Time:     AlertReceived,
						Duration: SomeDuration,
						Id:       AlertId,
						Level:    alertstore.Ok,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         AlertId,
				Name:       RegisteredAlertName,
				Level:      "crit",
				DeviceId:   RegisteredDeviceId,
				QueryField: UnregisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:  AlertId,
						Duration: "24h",
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Time:     AlertReceived,
						Duration: SomeDuration,
						Id:       AlertId,
						Level:    alertstore.Ok,
					},
				},
			},
		},

		"no notification because alert on unregistered deviceId": {
			wantStatus:      http.StatusNotFound,
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:  AlertId,
						Duration: "24h",
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Time:     AlertReceived,
						Duration: SomeDuration,
						Id:       AlertId,
						Level:    alertstore.Ok,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         AlertId,
				Name:       RegisteredAlertName,
				Level:      "crit",
				DeviceId:   UnregisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:  AlertId,
						Duration: "24h",
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Time:     AlertReceived,
						Duration: SomeDuration,
						Id:       AlertId,
						Level:    alertstore.Ok,
					},
				},
			},
		},

		"respond to first alert": {
			wantStatus: http.StatusNoContent,
			wantUserInboxes: map[string][]notifier.AlertMessage{
				UserEmail: {
					{
						Name:       RegisteredAlertName,
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						Level:      alertstore.Crit,
						Timestamp:  AfterDuration,
					},
				},
			},
			mockAlertStore: alertstore.Schema{
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
						AlertId:  AlertId,
						Duration: "24h",
					},
				},
				AlertStates: []alertstore.AlertState{},
			},
			alert: alertsource.InfluxAlert{
				Id:         AlertId,
				Level:      "crit",
				Name:       RegisteredAlertName,
				Time:       AfterDuration,
				DeviceId:   RegisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
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
						AlertId:  AlertId,
						Duration: "24h",
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived.Add(SomeDuration).Add(1 * time.Hour),
						Duration: SomeDuration,
						Level:    alertstore.Crit,
					},
				},
			},
		},
	}

	var n *notifier.MockNotifier
	var err error
	_, api := humatest.New(t)
	a.RegisterHumaOperations(api)
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			n = notifier.NewMockNotifier(tc.mockNotifierStore)
			a.Notifier = n
			a.AlertStore, err = alertstore.NewGorm(alertstore.Testing, "testing")
			require.Nil(t, err)
			defer a.AlertStore.Close()
			err = a.AlertStore.PrepareDb(tc.mockAlertStore)
			require.Nil(t, err)
			resp := api.Post("/alert/notify", tc.alert)
			if resp.Code != tc.wantStatus {
				t.Fatalf("wantStatus: %d, response status: %d", tc.wantStatus, resp.Code)
			}
			if tc.wantStatus < 300 {
				if diff := cmp.Diff(tc.wantUserInboxes, n.GetUserInboxes()); diff != "" {
					t.Fatalf("user inboxes mismatch (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(tc.wantAlertStore, a.AlertStore.GetSnapshot(),
					cmpopts.EquateApproxTime(1*time.Second)); diff != "" {
					t.Fatalf("alertstore mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestCreateAlertRule(t *testing.T) {
	tests := map[string]struct {
		wantErr                                        bool
		wantResponse                                   alertstore.CreatedRule
		wantStatus                                     int
		wantAlertStore, mockAlertStore                 alertstore.Schema
		startingAlertSourceCount, wantAlertSourceCount int
		alertRule                                      alertstore.AlertRule
	}{

		"successfully create a less than alert rule": {
			wantErr:              false,
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			mockAlertStore: alertstore.Schema{
				DeviceAlerts:    []alertstore.DeviceAlertRule{},
				AlertRecipients: []alertstore.AlertRecipient{},
				AlertStates:     []alertstore.AlertState{},
				AlertRules:      []alertstore.StoredAlertRule{},
			},
			wantAlertStore: alertstore.Schema{
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
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
					},
				},
			},
			alertRule: alertstore.AlertRule{
				RangeValues:    []float32{5},
				Name:           RegisteredAlertName,
				Duration:       "24h",
				EmailAddresses: []string{UserEmail},
				DeviceId:       "device1",
				QueryField:     "temperature",
				AlertType:      alertstore.LessThan,
			},
			wantResponse: alertstore.CreatedRule{
				AlertId: AlertId,
			},
		},

		"successfully create a greater than alert rule": {
			wantErr:              false,
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			mockAlertStore: alertstore.Schema{
				DeviceAlerts:    []alertstore.DeviceAlertRule{},
				AlertRecipients: []alertstore.AlertRecipient{},
				AlertStates:     []alertstore.AlertState{},
				AlertRules:      []alertstore.StoredAlertRule{},
			},
			wantAlertStore: alertstore.Schema{
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
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.GreaterThan,
					},
				},
			},
			alertRule: alertstore.AlertRule{
				RangeValues:    []float32{5},
				Name:           RegisteredAlertName,
				Duration:       "24h",
				EmailAddresses: []string{UserEmail},
				DeviceId:       "device1",
				QueryField:     "temperature",
				AlertType:      alertstore.GreaterThan,
			},
			wantResponse: alertstore.CreatedRule{
				AlertId: AlertId,
			},
		},

		"successfully create a range alert rule": {
			wantErr:              false,
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			mockAlertStore: alertstore.Schema{
				DeviceAlerts:    []alertstore.DeviceAlertRule{},
				AlertRecipients: []alertstore.AlertRecipient{},
				AlertStates:     []alertstore.AlertState{},
				AlertRules:      []alertstore.StoredAlertRule{},
			},
			wantAlertStore: alertstore.Schema{
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
						RangeValues: []float32{5, 10},
						Duration:    "24h",
						AlertType:   alertstore.Range,
					},
				},
			},
			alertRule: alertstore.AlertRule{
				RangeValues:    []float32{5, 10},
				Name:           RegisteredAlertName,
				Duration:       "24h",
				EmailAddresses: []string{UserEmail},
				DeviceId:       "device1",
				QueryField:     "temperature",
				AlertType:      alertstore.Range,
			},
			wantResponse: alertstore.CreatedRule{
				AlertId: AlertId,
			},
		},

		"invalid range on less than alert": {
			wantErr:    true,
			wantStatus: http.StatusUnprocessableEntity,
			mockAlertStore: alertstore.Schema{
				DeviceAlerts:    []alertstore.DeviceAlertRule{},
				AlertRecipients: []alertstore.AlertRecipient{},
				AlertStates:     []alertstore.AlertState{},
				AlertRules:      []alertstore.StoredAlertRule{},
			},
			wantAlertStore: EmptyAlertStore,
			alertRule: alertstore.AlertRule{
				Name:        RegisteredAlertName,
				RangeValues: []float32{5, 10},
				AlertType:   alertstore.LessThan,
			},
		},

		"invalid range on range alert": {
			wantErr:    true,
			wantStatus: http.StatusUnprocessableEntity,
			mockAlertStore: alertstore.Schema{
				DeviceAlerts:    []alertstore.DeviceAlertRule{},
				AlertRecipients: []alertstore.AlertRecipient{},
				AlertStates:     []alertstore.AlertState{},
				AlertRules:      []alertstore.StoredAlertRule{},
			},
			wantAlertStore: EmptyAlertStore,
			alertRule: alertstore.AlertRule{
				Name:        RegisteredAlertName,
				RangeValues: []float32{5},
				AlertType:   alertstore.Range,
			},
		},
		"invalid alert rule": {
			wantErr:        true,
			wantStatus:     http.StatusUnprocessableEntity,
			wantAlertStore: EmptyAlertStore,
			mockAlertStore: alertstore.Schema{
				DeviceAlerts:    []alertstore.DeviceAlertRule{},
				AlertRecipients: []alertstore.AlertRecipient{},
				AlertStates:     []alertstore.AlertState{},
				AlertRules:      []alertstore.StoredAlertRule{},
			},
			alertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				EmailAddresses: []string{},
				Duration:       "",
				AlertType:      alertstore.Range,
			},
		},

		"identical alert already exists": {
			wantErr:    true,
			wantStatus: http.StatusConflict,
			wantAlertStore: alertstore.Schema{
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
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
					},
				},
			},
			mockAlertStore: alertstore.Schema{
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
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
					},
				},
			},
			alertRule: alertstore.AlertRule{
				Name:        RegisteredAlertName,
				RangeValues: []float32{5},
				Duration:    "24h",
				DeviceId:    RegisteredDeviceId,
				QueryField:  RegisteredQueryField,
				AlertType:   alertstore.LessThan,
			},
		},

		"similar alert rule exists, but new one on different deviceId queryField combination": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			wantResponse: alertstore.CreatedRule{
				AlertId: "alert2",
			},
			wantAlertStore: alertstore.Schema{
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
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
					},
					{
						AlertId:     "alert2",
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
					},
				},
			},
			startingAlertSourceCount: 1,
			wantAlertSourceCount:     2,
			mockAlertStore: alertstore.Schema{
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
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
					},
				},
			},
			alertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				RangeValues:    []float32{5},
				EmailAddresses: []string{UserEmail},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     "humidity",
				AlertType:      alertstore.LessThan,
			},
		},

		"multiple alerts on deviceId and queryField combination": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			wantResponse: alertstore.CreatedRule{
				AlertId: "alert2",
			},
			wantAlertStore: alertstore.Schema{
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
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
					},
					{
						AlertId:     "alert2",
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertType:   alertstore.Range,
					},
				},
			},
			mockAlertStore: alertstore.Schema{
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
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
					},
				},
			},
			startingAlertSourceCount: 1,
			wantAlertSourceCount:     2,
			alertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail, "user2@alert.com"},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.Range,
			},
		},
	}

	var got alertstore.CreatedRule
	var err error
	_, api := humatest.New(t)
	a.RegisterHumaOperations(api)
	cmpOpts := enforceAlertIdNonEmpty()

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a.AlertStore, err = alertstore.NewGorm(alertstore.Testing, "testing")
			require.Nil(t, err)
			defer a.AlertStore.Close()
			err = a.AlertStore.PrepareDb(tc.mockAlertStore)
			require.Nil(t, err)
			a.AlertSource, err = alertsource.NewTestingInflux()
			require.Nil(t, err)
			defer a.AlertSource.Close()
			err = a.AlertSource.PrepareDb(tc.startingAlertSourceCount)
			require.Nil(t, err)
			resp := api.Post("/alert", tc.alertRule)
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatusCode: %d, response code: %d", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				err = json.Unmarshal(resp.Body.Bytes(), &got)
				require.Nil(t, err)
				gotAlertSourceCount := a.AlertSource.GetSnapshot()
				if tc.wantAlertSourceCount != gotAlertSourceCount {
					t.Fatalf(
						"wantAlertSourceCount: %d, got alertsCount: %d",
						tc.wantAlertSourceCount, gotAlertSourceCount)
				}
				if diff := cmp.Diff(tc.wantResponse, got, cmpOpts); diff != "" {
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

func TestConvertToRecipients(t *testing.T) {
	tests := map[string]struct {
		emails         []string
		alertId        string
		wantRecipients []alertstore.AlertRecipient
	}{
		"convert emails to slice of AlertRecipient": {
			emails:  []string{UserEmail, "user2@alert.com"},
			alertId: AlertId,
			wantRecipients: []alertstore.AlertRecipient{
				{AlertId: AlertId, Email: UserEmail},
				{AlertId: AlertId, Email: "user2@alert.com"},
			},
		},
		"ignore duplicate emails": {
			emails:  []string{UserEmail, UserEmail},
			alertId: AlertId,
			wantRecipients: []alertstore.AlertRecipient{
				{AlertId: AlertId, Email: UserEmail},
			},
		},
	}
	var recipients []alertstore.AlertRecipient
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			recipients = a.ConvertToRecipients(tc.alertId, tc.emails)
			if !reflect.DeepEqual(tc.wantRecipients, recipients) {
				t.Fatalf("wantRecipients: %v, got: %v", tc.wantRecipients, recipients)
			}
		})
	}
}

func TestUpdateAlertRule(t *testing.T) {
	tests := map[string]struct {
		wantErr                                        bool
		wantStatus                                     int
		alertId                                        string
		wantAlertRule                                  alertstore.AlertRule
		mockAlertStore, wantAlertStore                 alertstore.Schema
		startingAlertSourceCount, wantAlertSourceCount int
	}{

		"update the range values of an alert": {
			wantStatus:               http.StatusOK,
			startingAlertSourceCount: 1,
			wantAlertSourceCount:     1,
			alertId:                  AlertId,
			wantAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.Range,
			},
			mockAlertStore: alertstore.Schema{
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
						RangeValues: []float32{43, 90},
						Duration:    "24h",
						AlertType:   alertstore.Range,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				AlertStates: []alertstore.AlertState{},
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
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertType:   alertstore.Range,
					},
				},
			},
		},

		"update alert type from less than to greater than": {
			wantStatus:               http.StatusOK,
			startingAlertSourceCount: 1,
			wantAlertSourceCount:     1,
			alertId:                  AlertId,
			wantAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.GreaterThan,
			},
			mockAlertStore: alertstore.Schema{
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
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				AlertStates: []alertstore.AlertState{},
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
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.GreaterThan,
					},
				},
			},
		},

		"update alert type from greater than to less than": {
			wantStatus:               http.StatusOK,
			startingAlertSourceCount: 1,
			wantAlertSourceCount:     1,
			alertId:                  AlertId,
			wantAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.LessThan,
			},
			mockAlertStore: alertstore.Schema{
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
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.GreaterThan,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				AlertStates: []alertstore.AlertState{},
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
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
					},
				},
			},
		},

		"update alert type from less than to range": {
			wantStatus:               http.StatusOK,
			startingAlertSourceCount: 1,
			wantAlertSourceCount:     1,
			alertId:                  AlertId,
			wantAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.Range,
			},
			mockAlertStore: alertstore.Schema{
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
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				AlertStates: []alertstore.AlertState{},
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
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertType:   alertstore.Range,
					},
				},
			},
		},

		"can't update the queryField of an alert. Constitutes a new alert entirely": {
			wantStatus:               http.StatusNotFound,
			wantErr:                  true,
			startingAlertSourceCount: 1,
			wantAlertSourceCount:     1,
			alertId:                  AlertId,
			wantAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     UnregisteredQueryField,
				AlertType:      alertstore.Range,
			},
			mockAlertStore: alertstore.Schema{
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
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
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
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
					},
				},
			},
		},

		"can't update the deviceId of an alert. Constitutes a new alert entirely": {
			wantStatus:               http.StatusNotFound,
			wantErr:                  true,
			startingAlertSourceCount: 1,
			wantAlertSourceCount:     1,
			alertId:                  AlertId,
			wantAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       UnregisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.Range,
			},
			mockAlertStore: alertstore.Schema{
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
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
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
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
					},
				},
			},
		},

		"add more emails to the recipient list of an alert rule": {
			wantStatus: http.StatusOK,
			alertId:    AlertId,
			wantAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail, "user2@alert.com"},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.Range,
			},
			mockAlertStore: alertstore.Schema{
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
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertType:   alertstore.Range,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				AlertStates: []alertstore.AlertState{},
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
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertType:   alertstore.Range,
					},
				},
			},
		},

		"ignore duplicate emails in recipient list": {
			wantStatus:               http.StatusOK,
			startingAlertSourceCount: 1,
			wantAlertSourceCount:     1,
			alertId:                  AlertId,
			wantAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail, UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.Range,
			},
			mockAlertStore: alertstore.Schema{
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
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertType:   alertstore.Range,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				AlertStates: []alertstore.AlertState{},
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
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertType:   alertstore.Range,
					},
				},
			},
		},

		"error when trying to update non existent alert rule": {
			wantStatus:     http.StatusNotFound,
			wantErr:        true,
			alertId:        AlertId,
			mockAlertStore: alertstore.Schema{},
			wantAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.Range,
			},
		},

		"invalid: 1 range value on a range type alert check": {
			wantStatus:     http.StatusBadRequest,
			wantErr:        true,
			mockAlertStore: alertstore.Schema{},
			alertId:        AlertId,
			wantAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.Range,
			},
		},

		"invalid: 2 range values on a less than type alert check": {
			wantStatus:     http.StatusBadRequest,
			wantErr:        true,
			mockAlertStore: alertstore.Schema{},
			alertId:        AlertId,
			wantAlertRule: alertstore.AlertRule{
				EmailAddresses: []string{UserEmail},
				Name:           RegisteredAlertName,
				RangeValues:    []float32{50, 90},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.LessThan,
			},
		},
	}

	var err error
	var got alertstore.AlertRule
	var alertRules []alertstore.AlertRule
	_, api := humatest.New(t)
	a.RegisterHumaOperations(api)
	cmpOpts := enforceAlertIdNonEmpty()
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a.AlertStore, err = alertstore.NewMockAlertStore(alertstore.Testing, "testing")
			defer a.AlertStore.Close()
			err = a.AlertStore.PrepareDb(tc.mockAlertStore)
			require.Nil(t, err)
			a.AlertSource, err = alertsource.NewMockAlertSource()
			require.Nil(t, err)
			defer a.AlertSource.Close()
			alertRules = SchemaToAlertRules(tc.mockAlertStore)
			err = a.AlertSource.PrepareDb(tc.startingAlertSourceCount, alertRules...)
			require.Nil(t, err)
			resp := api.Put(fmt.Sprintf("/alert/%s", tc.alertId), tc.wantAlertRule)
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatusCode: %d, response code: %d", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				err = json.Unmarshal(resp.Body.Bytes(), &got)
				require.Nil(t, err)
				if diff := cmp.Diff(tc.wantAlertRule, got, cmpOpts); diff != "" {
					t.Fatalf("response alertrule mismatch (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(tc.wantAlertStore, a.AlertStore.GetSnapshot()); diff != "" {
					t.Fatalf("alertstore mismatch (-want +got):\n%s", diff)
				}
				alertSourceCount := a.AlertSource.GetSnapshot()
				if alertSourceCount != tc.wantAlertSourceCount {
					t.Fatalf("alertSourceCount: %d, wantAlertSourceCount: %d",
						alertSourceCount, tc.wantAlertSourceCount)
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
		mockAlertStore alertstore.Schema
	}{

		"successfully get a stored alert": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			alertId:    AlertId,
			mockAlertStore: alertstore.Schema{
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
						RangeValues: []float32{10, 20},
						AlertType:   alertstore.Range,
					},
				},
			},
			want: alertstore.AlertRule{
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.Range,
			},
		},
		"alert doesn't exist": {
			wantErr:        true,
			want:           alertstore.AlertRule{},
			wantStatus:     404,
			alertId:        "alert2",
			mockAlertStore: alertstore.Schema{},
		},
	}

	var err error
	var got alertstore.AlertRule
	_, api := humatest.New(t)
	a.RegisterHumaOperations(api)
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a.AlertStore, err = alertstore.NewGorm(alertstore.Testing, "testing")
			defer a.AlertStore.Close()
			err = a.AlertStore.PrepareDb(tc.mockAlertStore)
			require.Nil(t, err)
			resp := api.Get(fmt.Sprintf("/alert/%s", tc.alertId))
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatusCode: %d, response code: %d", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				err = json.Unmarshal(resp.Body.Bytes(), &got)
				require.Nil(t, err)
				if diff := cmp.Diff(tc.want, got); diff != "" {
					t.Fatalf("response mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestDeleteAlert(t *testing.T) {
	tests := map[string]struct {
		wantErr                                        bool
		wantStatus                                     int
		alertId                                        string
		wantAlertRule                                  alertstore.AlertRule
		mockAlertStore, wantAlertStore                 alertstore.Schema
		startingAlertSourceCount, wantAlertSourceCount int
	}{

		"successfully delete an alert": {
			wantErr:                  false,
			wantStatus:               http.StatusOK,
			alertId:                  AlertId,
			startingAlertSourceCount: 1,
			wantAlertSourceCount:     0,
			mockAlertStore: alertstore.Schema{
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
						RangeValues: []float32{10, 20},
						AlertType:   alertstore.Range,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id: AlertId,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				DeviceAlerts:    []alertstore.DeviceAlertRule{},
				AlertRecipients: []alertstore.AlertRecipient{},
				AlertStates:     []alertstore.AlertState{},
				AlertRules:      []alertstore.StoredAlertRule{},
			},
			wantAlertRule: alertstore.AlertRule{
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.Range,
			},
		},

		"try to delete non existent alert": {
			wantErr:        true,
			wantStatus:     404,
			alertId:        "alert2",
			mockAlertStore: alertstore.Schema{},
		},
	}

	var got alertstore.AlertRule
	var alertRules []alertstore.AlertRule
	var err error
	_, api := humatest.New(t)
	a.RegisterHumaOperations(api)
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a.AlertSource, err = alertsource.NewMockAlertSource()
			require.Nil(t, err)
			defer a.AlertSource.Close()
			alertRules = SchemaToAlertRules(tc.mockAlertStore)
			err = a.AlertSource.PrepareDb(tc.startingAlertSourceCount, alertRules...)
			require.Nil(t, err)
			a.AlertStore, err = alertstore.NewGorm(alertstore.Testing, "testing")
			require.Nil(t, err)
			defer a.AlertStore.Close()
			err = a.AlertStore.PrepareDb(tc.mockAlertStore)
			require.Nil(t, err)
			resp := api.Delete(fmt.Sprintf("/alert/%s", tc.alertId))
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatus: %d, response Code: %v", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				err = json.Unmarshal(resp.Body.Bytes(), &got)
				require.Nil(t, err)
				if diff := cmp.Diff(tc.wantAlertRule, got); err != nil {
					t.Fatalf("alertRule mismatch (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(tc.wantAlertStore, a.AlertStore.GetSnapshot()); diff != "" {
					t.Fatalf("alertStore mismatch (-want +got):\n%s", diff)
				}
				alertsCount := a.AlertSource.GetSnapshot()
				if tc.wantAlertSourceCount != alertsCount {
					t.Fatalf("wantAlertSourceCount: %d, got alertsCount: %d",
						tc.wantAlertSourceCount, alertsCount)
				}
			}
		})
	}
}

func SchemaToAlertRules(dbRecord alertstore.Schema) []alertstore.AlertRule {
	var ar alertstore.AlertRule
	rules := make([]alertstore.AlertRule, 0, len(dbRecord.DeviceAlerts))
	for _, da := range dbRecord.DeviceAlerts {
		ar.AlertId = da.AlertId
		ar.DeviceId = da.DeviceId
		ar.QueryField = da.QueryField

		for _, rule := range dbRecord.AlertRules {
			ar.RangeValues = rule.RangeValues
			ar.Duration = rule.Duration
			ar.AlertType = rule.AlertType
		}
		rules = append(rules, ar)
	}
	return rules
}

func enforceAlertIdNonEmpty() cmp.Options {
	//NOTE: these opts are used to enforce that AlertId not be empty. This reflects that implementations do not necessarily have to exactly match 'Alert1' with returned alert ids.
	return cmp.Options{
		cmp.FilterPath(
			func(p cmp.Path) bool {
				_, ok := p.Last().(cmp.StructField)
				return ok &&
					(p.Last().String() == "AlertId" || p.Last().String() == "Id")
			},
			cmp.Comparer(func(x, y string) bool { return x != "" }),
		),
		cmpopts.IgnoreFields(alertstore.AlertRule{}, "AlertId"),
		cmpopts.IgnoreFields(alertstore.StoredAlertRule{}, "AlertId"),
		cmpopts.IgnoreFields(alertstore.AlertState{}, "Id"),
		cmpopts.IgnoreFields(alertstore.AlertRecipient{}, "AlertId"),
		cmpopts.IgnoreFields(alertstore.DeviceAlertRule{}, "AlertId"),
		cmpopts.IgnoreFields(alertstore.CreatedRule{}, "AlertId"),
	}
}
