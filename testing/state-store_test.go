package testing

import (
	"context"
	"testing"
	"time"

	"github.com/geraud22/sensordata-alerts/alertsource"
	"github.com/geraud22/sensordata-alerts/alertstore"
	"github.com/geraud22/sensordata-alerts/app"
	"github.com/geraud22/sensordata-alerts/notifier"
	"github.com/google/go-cmp/cmp"
)

var a = &app.App{}
var EmptyInboxes = map[string][]string{}

const UnregisteredQueryField = "humidity"
const RegisteredQueryField = "temperature"
const UnregisteredDeviceId = "device2"
const RegisteredDeviceId = "device1"
const SomeDuration = (24 * time.Hour)

var AlertReceived = time.Now()
var AfterDuration = time.Now().Add(25 * time.Hour)
var WithinDuration = time.Now().Add(6 * time.Hour)

func TestNotifyCrit(t *testing.T) {
	var got bool
	tests := map[string]struct {
		want            bool
		alert, previous *alertsource.Alert
	}{
		"no change within duration, so do nothing": {
			want: false,
			alert: &alertsource.Alert{
				Level:    alertsource.Crit,
				Time:     WithinDuration,
				Duration: SomeDuration,
			},
			previous: &alertsource.Alert{
				Level:    alertsource.Crit,
				Time:     PreviousAlertReceived,
				Duration: SomeDuration,
			},
		},
		"still crit after duration, so notify": {
			want: true,
			alert: &alertsource.Alert{
				Level:    alertsource.Crit,
				Time:     AfterDuration,
				Duration: SomeDuration,
			},
			previous: &alertsource.Alert{
				Level:    alertsource.Crit,
				Time:     PreviousAlertReceived,
				Duration: SomeDuration,
			},
		},
		"change to crit, so notify": {
			want: true,
			alert: &alertsource.Alert{
				Level: alertsource.Crit,
			},
			previous: &alertsource.Alert{
				Level: alertsource.Ok,
			},
		},
		"change from crit to ok, so do nothing": {
			want: false,
			alert: &alertsource.Alert{
				Level: alertsource.Ok,
			},
			previous: &alertsource.Alert{
				Level: alertsource.Crit,
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got = a.ShouldNotifyCrit(tt.alert, tt.previous)
			if got != tt.want {
				t.Fatalf("tt.want = %v, got = %v", tt.want, got)
			}
		})
	}
}

func TestRespondToAlert(t *testing.T) {
	tests := map[string]struct {
		wantUserInboxes                   map[string][]string
		wantAlertStore                    map[any]any
		mockNotifierStore, mockAlertStore map[any]any
		alert                             alertsource.InfluxAlert
	}{
		"send alert because still crit after duration": {
			wantUserInboxes: map[string][]string{
				"user1": {
					"Alert message stating crit level.",
				},
			},
			mockAlertStore: map[any]any{
				"alertRecipients": map[string][]string{
					"alert1": {"user1"},
				},
				"alertStore": map[string]map[string]alertsource.Alert{
					RegisteredDeviceId: {
						RegisteredQueryField: alertsource.Alert{
							Time:       PreviousAlertReceived,
							Duration:   SomeDuration,
							DeviceId:   RegisteredDeviceId,
							Message:    "Already crit.",
							QueryField: RegisteredQueryField,
							Id:         "alert1",
							Level:      alertsource.Crit,
						},
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         "alert1",
				Level:      "crit",
				Message:    "Alert message stating crit level.",
				Time:       AfterDuration,
				DeviceId:   RegisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
			wantAlertStore: map[any]any{
				"alertRecipients": map[string][]string{
					"alert1": {"user1"},
				},
				"alertStore": map[string]map[string]alertsource.Alert{
					RegisteredDeviceId: {
						RegisteredQueryField: alertsource.Alert{
							Time:       AfterDuration,
							Duration:   SomeDuration,
							DeviceId:   RegisteredDeviceId,
							Message:    "Alert message stating crit level.",
							QueryField: RegisteredQueryField,
							Id:         "alert1",
							Level:      alertsource.Crit,
						},
					},
				},
			},
		},
		"no notification because duration has not elapsed": {
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: map[any]any{
				"alertRecipients": map[string][]string{
					"alert1": {"user1"},
				},
				"alertStore": map[string]map[string]alertsource.Alert{
					RegisteredDeviceId: {
						RegisteredQueryField: alertsource.Alert{
							Time:       PreviousAlertReceived,
							Duration:   SomeDuration,
							DeviceId:   RegisteredDeviceId,
							Message:    "Already crit.",
							QueryField: RegisteredQueryField,
							Id:         "alert1",
							Level:      alertsource.Crit,
						},
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         "alert1",
				Level:      "crit",
				Message:    "Alert message stating crit level.",
				Time:       WithinDuration,
				DeviceId:   RegisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
			wantAlertStore: map[any]any{
				"alertRecipients": map[string][]string{
					"alert1": {"user1"},
				},
				"alertStore": map[string]map[string]alertsource.Alert{
					RegisteredDeviceId: {
						RegisteredQueryField: alertsource.Alert{
							Time:       PreviousAlertReceived,
							Duration:   SomeDuration,
							DeviceId:   RegisteredDeviceId,
							Message:    "Already crit.",
							QueryField: RegisteredQueryField,
							Id:         "alert1",
							Level:      alertsource.Crit,
						},
					},
				},
			},
		},
		"send notification because alert level changing from ok to crit": {
			wantUserInboxes: map[string][]string{
				"user1": {
					"Alert message stating crit level.",
				},
			},
			mockAlertStore: map[any]any{
				"alertRecipients": map[string][]string{
					"alert1": {"user1"},
				},
				"alertStore": map[string]map[string]alertsource.Alert{
					RegisteredDeviceId: {
						RegisteredQueryField: alertsource.Alert{
							Time:       PreviousAlertReceived,
							DeviceId:   RegisteredDeviceId,
							QueryField: RegisteredQueryField,
							Id:         "alert1",
							Level:      alertsource.Ok,
						},
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         "alert1",
				Level:      "crit",
				Message:    "Alert message stating crit level.",
				Time:       WithinDuration,
				DeviceId:   RegisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
			wantAlertStore: map[any]any{
				"alertRecipients": map[string][]string{
					"alert1": {"user1"},
				},
				"alertStore": map[string]map[string]alertsource.Alert{
					RegisteredDeviceId: {
						RegisteredQueryField: alertsource.Alert{
							Time:       WithinDuration,
							DeviceId:   RegisteredDeviceId,
							QueryField: RegisteredQueryField,
							Id:         "alert1",
							Level:      alertsource.Crit,
							Message:    "Alert message stating crit level.",
						},
					},
				},
			},
		},
		"no notification because alert on unregistered queryField": {
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: map[any]any{
				"alertRecipients": map[string][]string{
					"alert1": {"user1"},
				},
				"alertStore": map[string]map[string]alertsource.Alert{
					RegisteredDeviceId: {
						RegisteredQueryField: alertsource.Alert{
							Time:       PreviousAlertReceived,
							DeviceId:   RegisteredDeviceId,
							QueryField: RegisteredQueryField,
							Id:         "alert1",
							Level:      alertsource.Ok,
						},
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         "alert1",
				Level:      "crit",
				Message:    "Alert message stating crit level.",
				DeviceId:   RegisteredDeviceId,
				QueryField: UnregisteredQueryField,
			},
			wantAlertStore: map[any]any{
				"alertRecipients": map[string][]string{
					"alert1": {"user1"},
				},
				"alertStore": map[string]map[string]alertsource.Alert{
					RegisteredDeviceId: {
						RegisteredQueryField: alertsource.Alert{
							Time:       PreviousAlertReceived,
							DeviceId:   RegisteredDeviceId,
							QueryField: RegisteredQueryField,
							Id:         "alert1",
							Level:      alertsource.Ok,
						},
					},
				},
			},
		},
		"no notification because alert on unregistered deviceId": {
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: map[any]any{
				"alertRecipients": map[string][]string{
					"alert1": {"user1"},
				},
				"alertStore": map[string]map[string]alertsource.Alert{
					RegisteredDeviceId: {
						RegisteredQueryField: alertsource.Alert{
							Time:       PreviousAlertReceived,
							DeviceId:   RegisteredDeviceId,
							QueryField: RegisteredQueryField,
							Id:         "alert1",
							Level:      alertsource.Ok,
						},
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         "alert1",
				Level:      "crit",
				Message:    "Alert message stating crit level.",
				DeviceId:   UnregisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
			wantAlertStore: map[any]any{
				"alertRecipients": map[string][]string{
					"alert1": {"user1"},
				},
				"alertStore": map[string]map[string]alertsource.Alert{
					RegisteredDeviceId: {
						RegisteredQueryField: alertsource.Alert{
							Time:       PreviousAlertReceived,
							DeviceId:   RegisteredDeviceId,
							QueryField: RegisteredQueryField,
							Id:         "alert1",
							Level:      alertsource.Ok,
						},
					},
				},
			},
		},
		"send notification on the first alert ever received": {
			wantUserInboxes: map[string][]string{
				"user1": {
					"Alert message stating crit level.",
				},
			},
			wantAlertStore: map[any]any{
				"alertRecipients": map[string][]string{
					"alert1": {"user1"},
				},
				"alertStore": map[string]map[string]alertsource.Alert{
					RegisteredDeviceId: {
						RegisteredQueryField: alertsource.Alert{
							Time:       AlertReceived,
							DeviceId:   RegisteredDeviceId,
							QueryField: RegisteredQueryField,
							Id:         "alert1",
							Level:      alertsource.Crit,
						},
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         RegisteredDeviceId,
				Level:      "crit",
				Message:    "Alert message stating crit level.",
				Time:       AlertReceived,
				DeviceId:   RegisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
		},
	}
	var n *notifier.MockNotifier
	var as *alertstore.MockAlertStore
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			n = notifier.NewMockNotifier(tt.mockNotifierStore)
			as = alertstore.NewMockAlertStore(tt.mockAlertStore)
			a.Alertstore = as
			a.Notifier = n
			a.RespondToAlert(
				context.Background(),
				&struct{ Body alertsource.InfluxAlert }{tt.alert},
			)
			if diff := cmp.Diff(tt.wantUserInboxes, n.GetUserInboxes()); diff != "" {
				t.Fatalf("user inboxes mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantAlertStore, as.GetValues()); diff != "" {
				t.Fatalf("alertstore mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
