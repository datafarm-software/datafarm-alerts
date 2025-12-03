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
				Time:     AlertReceived,
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
				Time:     AlertReceived,
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
							Time:       AlertReceived,
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
							Time:       AlertReceived,
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
							Time:       AlertReceived,
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
							Time:       AlertReceived,
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
							Time:       AlertReceived,
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
				Message:    "Alert that should be ignored.",
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
							Time:       AlertReceived,
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
							Time:       AlertReceived,
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
							Time:       AlertReceived,
							DeviceId:   RegisteredDeviceId,
							QueryField: RegisteredQueryField,
							Id:         "alert1",
							Level:      alertsource.Ok,
						},
					},
				},
			},
		},
	}
	var n *notifier.MockNotifier
	var as *alertstore.MockAlertStore
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			n = notifier.NewMockNotifier(tt.mockNotifierStore)
			as = alertstore.NewMockAlertStore(tt.mockAlertStore)
			a.AlertStore = as
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

func TestCreateAlert(t *testing.T) {
	tests := map[string]struct {
		wantErr                         bool
		wantAlertStore                  map[any]any
		mockAlertStore, mockAlertSource map[any]any
		createAlert                     alertsource.CreateAlert
	}{
		"successfully create a less than alert rule": {
			wantErr: false,
			wantAlertStore: map[any]any{
				"alertRecipients": map[string][]string{
					"alert1": {"user1"},
				},
				"alertStore": map[string]map[string]alertsource.Alert{
					RegisteredDeviceId: {
						RegisteredQueryField: alertsource.Alert{
							Duration:   time.Duration(6 * time.Hour),
							DeviceId:   RegisteredDeviceId,
							QueryField: RegisteredQueryField,
							Id:         "alert1",
							Level:      alertsource.Ok,
						},
					},
				},
			},
			createAlert: alertsource.CreateAlert{
				RangeValues: []int{5},
				Duration:    "6h",
				Username:    "user1",
				DeviceId:    "device1",
				QueryField:  "temperature",
				AlertRange:  alertsource.LessThan,
			},
		},
		"invalid range on less than alert": {
			wantErr: true,
			wantAlertStore: map[any]any{
				"alertRecipients": map[string][]string{},
				"alertStore":      map[string]map[string]alertsource.Alert{},
			},
			createAlert: alertsource.CreateAlert{
				RangeValues: []int{5, 10},
				AlertRange:  alertsource.LessThan,
			},
		},
		"invalid range on range alert": {
			wantErr: true,
			wantAlertStore: map[any]any{
				"alertRecipients": map[string][]string{},
				"alertStore":      map[string]map[string]alertsource.Alert{},
			},
			createAlert: alertsource.CreateAlert{
				RangeValues: []int{5},
				AlertRange:  alertsource.Range,
			},
		},
	}
	var as *alertstore.MockAlertStore
	var source *alertsource.MockAlertSource
	var err error
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			as = alertstore.NewMockAlertStore(tt.mockAlertStore)
			a.AlertStore = as
			source = alertsource.NewMockAlertSource(tt.mockAlertSource)
			a.AlertSource = source
			_, err = a.CreateAlert(context.Background(),
				&struct{ Body alertsource.CreateAlert }{tt.createAlert})
			if tt.wantErr != (err != nil) {
				t.Fatalf("wantErr: %v, got: %v", tt.wantErr, err)
			}
			if diff := cmp.Diff(tt.wantAlertStore, as.GetValues()); diff != "" {
				t.Fatalf("alertstore mismatch (-want +got):\n%s", diff)
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
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err = alertstore.ParseDuration(tt.input)
			if tt.wantErr != (err != nil) {
				t.Fatalf("wantErr: %v, got: %v", tt.want, err)
			}
			if !tt.wantErr {
				if tt.want != got {
					t.Fatalf("want: %v, got: %v", tt.want, got)
				}
			}
		})
	}
}
