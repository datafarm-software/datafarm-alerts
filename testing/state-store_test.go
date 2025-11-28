package testing

import (
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
const SomeDuration = (24 * time.Hour)

var AlertReceived = time.Now()
var AfterDuration = time.Now().Add(25 * time.Hour)
var WithinDuration = time.Now().Add(6 * time.Hour)

func TestNotifyCrit(t *testing.T) {
	var got bool
	tests := map[string]struct {
		want            bool
		alert, previous *alertsource.AlertState
	}{
		"no change within duration, so do nothing": {
			want: false,
			alert: &alertsource.AlertState{
				Level:    alertsource.Crit,
				Time:     WithinDuration,
				Duration: SomeDuration,
			},
			previous: &alertsource.AlertState{
				Level:    alertsource.Crit,
				Time:     AlertReceived,
				Duration: SomeDuration,
			},
		},
		"still crit after duration, so notify": {
			want: true,
			alert: &alertsource.AlertState{
				Level:    alertsource.Crit,
				Time:     AfterDuration,
				Duration: SomeDuration,
			},
			previous: &alertsource.AlertState{
				Level:    alertsource.Crit,
				Time:     AlertReceived,
				Duration: SomeDuration,
			},
		},
		"change to crit, so notify": {
			want: true,
			alert: &alertsource.AlertState{
				Level: alertsource.Crit,
			},
			previous: &alertsource.AlertState{
				Level: alertsource.Ok,
			},
		},
		"change from crit to ok, so do nothing": {
			want: false,
			alert: &alertsource.AlertState{
				Level: alertsource.Ok,
			},
			previous: &alertsource.AlertState{
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
		wantUserInboxes   map[string][]string
		mockNotifierStore map[any]any
		mockAlertStore    map[any]any
		alert             alertsource.InfluxAlert
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
				"alertStore": map[string]map[string]alertsource.AlertState{
					"device1": {
						RegisteredQueryField: alertsource.AlertState{
							Time:       time.Now(),
							Duration:   SomeDuration,
							DeviceId:   "device1",
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
				DeviceId:   "device1",
				QueryField: RegisteredQueryField,
			},
		},
		"no notification because duration has not elapsed": {
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: map[any]any{
				"alertRecipients": map[string][]string{
					"alert1": {"user1"},
				},
				"alertStore": map[string]map[string]alertsource.AlertState{
					"device1": {
						RegisteredQueryField: alertsource.AlertState{
							Time:       time.Now(),
							Duration:   SomeDuration,
							DeviceId:   "device1",
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
				DeviceId:   "device1",
				QueryField: RegisteredQueryField,
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
				"alertStore": map[string]map[string]alertsource.AlertState{
					"device1": {
						RegisteredQueryField: alertsource.AlertState{
							Time:       time.Now(),
							Duration:   SomeDuration,
							DeviceId:   "device1",
							Message:    "Alert level Ok.",
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
				DeviceId:   "device1",
				QueryField: RegisteredQueryField,
			},
		},
		"no notification because alert on unregistered queryField": {
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: map[any]any{
				"alertRecipients": map[string][]string{
					"alert1": {"user1"},
				},
				"alertStore": map[string]map[string]alertsource.AlertState{
					"device1": {
						RegisteredQueryField: alertsource.AlertState{},
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         "alert1",
				Level:      "crit",
				Message:    "Alert message stating crit level.",
				Time:       WithinDuration,
				DeviceId:   "device1",
				QueryField: UnregisteredQueryField,
			},
		},
	}
	var n *notifier.MockNotifier
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			n = notifier.NewMockNotifier(tt.mockNotifierStore)
			a.Alertstore = alertstore.NewMockAlertStore(tt.mockAlertStore)
			a.Notifier = n
			a.RespondToAlert(tt.alert)
			if diff := cmp.Diff(tt.wantUserInboxes, n.GetUserInboxes()); diff != "" {
				t.Fatalf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
