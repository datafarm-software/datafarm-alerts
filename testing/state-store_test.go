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
				Time:     time.Now(),
				Duration: time.Duration(24 * time.Hour),
			},
			previous: &alertsource.AlertState{
				Level:    alertsource.Crit,
				Time:     time.Now().Add(-(6 * time.Hour)),
				Duration: time.Duration(24 * time.Hour),
			},
		},
		"still crit after duration, so notify": {
			want: true,
			alert: &alertsource.AlertState{
				Level:    alertsource.Crit,
				Time:     time.Now(),
				Duration: time.Duration(24 * time.Hour),
			},
			previous: &alertsource.AlertState{
				Level:    alertsource.Crit,
				Time:     time.Now().Add(-(25 * time.Hour)),
				Duration: time.Duration(24 * time.Hour),
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
		wantUserInboxes     map[string][]string
		mockNotifierInput   map[any]any
		mockAlertStoreInput map[any]any
		alert               alertsource.InfluxAlert
	}{
		"send alert because still crit after duration": {
			mockAlertStoreInput: map[any]any{
				"alertRecipients": map[string][]string{
					"alert1": {"user1"},
				},
				"alertStore": map[string]map[string]alertsource.AlertState{
					"device1": {
						"temperature": alertsource.AlertState{
							Time:       time.Now(),
							Duration:   (24 * time.Hour),
							DeviceId:   "device1",
							Message:    "Already crit.",
							QueryField: "temperature",
							Id:         "alert1",
							Level:      alertsource.Crit,
						},
					},
				},
			},
			wantUserInboxes: map[string][]string{
				"user1": {
					"Alert message stating crit level.",
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         "alert1",
				Level:      "crit",
				Message:    "Alert message stating crit level.",
				Time:       time.Now().Add(25 * time.Hour),
				DeviceId:   "device1",
				QueryField: "temperature",
			},
		},
	}
	var n *notifier.MockNotifier
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			n = notifier.NewMockNotifier(tt.mockNotifierInput)
			a.Alertstore = alertstore.NewMockAlertStore(tt.mockAlertStoreInput)
			a.Notifier = n
			a.RespondToAlert(tt.alert)
			if diff := cmp.Diff(tt.wantUserInboxes, n.GetUserInboxes()); diff != "" {
				t.Fatalf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
