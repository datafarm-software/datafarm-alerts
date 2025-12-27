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
const AlertId = "alert1"
const UserId = "user1"

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
				UserId: {
					"New alert message stating crit level.",
				},
			},
			mockAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, UserId: UserId},
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
						Level:    alertsource.Crit,
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
					{AlertId: AlertId, UserId: UserId},
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
						Level:    alertsource.Crit,
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
						Level:    alertsource.Crit,
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
						Level:    alertsource.Crit,
					},
				},
			},
		},

		"send notification because alert level changing from ok to crit": {
			wantUserInboxes: map[string][]string{
				UserId: {
					"Status is now crit.",
				},
			},
			mockAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, UserId: UserId},
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
						Level:    alertsource.Crit,
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
					{AlertId: AlertId, UserId: UserId},
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
						Level:    alertsource.Crit,
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
		wantResponse                   *struct{ Body alertsource.CreatedRule }
		wantAlertStore, mockAlertStore *alertstore.Schema
		mockAlertSource                map[any]any
		alertRule                      alertstore.AlertRule
	}{

		"successfully create a less than alert rule": {
			wantErr: false,
			wantAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, UserId: UserId},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.AlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []int{5},
						Duration:    "24h",
						Usernames:   []string{UserId},
						DeviceId:    RegisteredDeviceId,
						QueryField:  RegisteredQueryField,
						AlertRange:  alertsource.LessThan,
					},
				},
			},
			alertRule: alertstore.AlertRule{
				RangeValues: []int{5},
				Duration:    "24h",
				Usernames:   []string{UserId},
				DeviceId:    "device1",
				QueryField:  "temperature",
				AlertRange:  alertsource.LessThan,
			},
			wantResponse: &struct{ Body alertsource.CreatedRule }{
				Body: alertsource.CreatedRule{
					AlertId: AlertId,
				},
			},
		},

		"invalid range on less than alert": {
			wantErr:        true,
			wantAlertStore: nil,
			alertRule: alertstore.AlertRule{
				RangeValues: []int{5, 10},
				AlertRange:  alertsource.LessThan,
			},
		},

		"invalid range on range alert": {
			wantErr:        true,
			wantAlertStore: nil,
			alertRule: alertstore.AlertRule{
				RangeValues: []int{5},
				AlertRange:  alertsource.Range,
			},
		},
		"invalid alert rule": {
			wantErr:        true,
			wantAlertStore: nil,
			alertRule: alertstore.AlertRule{
				Usernames:  []string{},
				Duration:   "",
				AlertRange: alertsource.Range,
			},
		},

		"identical alert already exists": {
			wantErr: true,
			wantAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, UserId: UserId},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.AlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []int{5},
						Duration:    "24h",
						Usernames:   []string{UserId},
						DeviceId:    RegisteredDeviceId,
						QueryField:  RegisteredQueryField,
						AlertRange:  alertsource.LessThan,
					},
				},
			},
			mockAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, UserId: UserId},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.AlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []int{5},
						Duration:    "24h",
						Usernames:   []string{UserId},
						DeviceId:    RegisteredDeviceId,
						QueryField:  RegisteredQueryField,
						AlertRange:  alertsource.LessThan,
					},
				},
			},
			alertRule: alertstore.AlertRule{
				Usernames:   []string{UserId},
				RangeValues: []int{5},
				Duration:    "24h",
				DeviceId:    RegisteredDeviceId,
				QueryField:  RegisteredQueryField,
				AlertRange:  alertsource.LessThan,
			},
		},

		"multiple alerts on deviceId and queryField combination": {
			wantErr: false,
			wantAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, UserId: UserId},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.AlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []int{5},
						Duration:    "24h",
						Usernames:   []string{UserId},
						DeviceId:    RegisteredDeviceId,
						QueryField:  RegisteredQueryField,
						AlertRange:  alertsource.LessThan,
					},
					{
						AlertId: "alert2",
					},
				},
			},
			mockAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, UserId: UserId},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.AlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []int{5},
						Duration:    "24h",
						Usernames:   []string{UserId},
						DeviceId:    RegisteredDeviceId,
						QueryField:  RegisteredQueryField,
						AlertRange:  alertsource.LessThan,
					},
				},
			},
			alertRule: alertstore.AlertRule{
				Usernames:   []string{UserId, "user2"},
				RangeValues: []int{10, 20},
				Duration:    "24h",
				DeviceId:    RegisteredDeviceId,
				QueryField:  RegisteredQueryField,
				AlertRange:  alertsource.Range,
			},
		},
	}

	var as *alertstore.MockAlertStore
	var source *alertsource.MockAlertSource
	var err error
	var got *struct{ Body alertsource.CreatedRule }
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			as = alertstore.NewMockAlertStore(tc.mockAlertStore)
			a.AlertStore = as
			source = alertsource.NewMockAlertSource(tc.mockAlertSource)
			a.AlertSource = source
			got, err = a.CreateAlertRule(context.Background(),
				&struct{ Body alertstore.AlertRule }{tc.alertRule})
			if tc.wantErr != (err != nil) {
				t.Fatalf("wantErr: %v, got: %v", tc.wantErr, err)
			}
			if !tc.wantErr {
				if diff := cmp.Diff(tc.wantResponse.Body, got.Body); diff != "" {
					t.Fatalf("response mismatch (-want +got):\n%s", diff)
				}
			}
			if diff := cmp.Diff(tc.wantAlertStore, as.GetSnapshot()); diff != "" {
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
		alertId                        string
		input                          alertstore.AlertRule
		mockAlertStore, wantAlertStore *alertstore.Schema
	}{
		"update the range values of an alert": {
			wantErr: false,
			alertId: AlertId,
			input: alertstore.AlertRule{
				Usernames:   []string{UserId},
				RangeValues: []int{10, 20},
				Duration:    "24h",
				DeviceId:    RegisteredDeviceId,
				QueryField:  RegisteredQueryField,
				AlertRange:  alertsource.Range,
			},
			mockAlertStore: &alertstore.Schema{
				AlertRules: []alertstore.AlertRule{
					{
						Usernames:   []string{UserId},
						RangeValues: []int{43, 90},
						Duration:    "24h",
						DeviceId:    RegisteredDeviceId,
						QueryField:  RegisteredQueryField,
						AlertRange:  alertsource.Range,
					},
				},
			},
			wantAlertStore: &alertstore.Schema{
				AlertRules: []alertstore.AlertRule{
					{
						Usernames:   []string{UserId},
						RangeValues: []int{10, 20},
						Duration:    "24h",
						DeviceId:    RegisteredDeviceId,
						QueryField:  RegisteredQueryField,
						AlertRange:  alertsource.Range,
					},
				},
			},
		},

		"error when trying to update non existent alert rule": {
			wantErr: true,
			alertId: AlertId,
			input: alertstore.AlertRule{
				Usernames:   []string{UserId},
				RangeValues: []int{10, 20},
				Duration:    "24h",
				DeviceId:    RegisteredDeviceId,
				QueryField:  RegisteredQueryField,
				AlertRange:  alertsource.Range,
			},
		},

		"invalid: 1 range value on a range type alert check": {
			wantErr: true,
			alertId: AlertId,
			input: alertstore.AlertRule{
				Usernames:   []string{UserId},
				RangeValues: []int{10},
				Duration:    "24h",
				DeviceId:    RegisteredDeviceId,
				QueryField:  RegisteredQueryField,
				AlertRange:  alertsource.Range,
			},
		},

		"invalid: 2 range values on a less than type alert check": {
			wantErr: true,
			alertId: AlertId,
			input: alertstore.AlertRule{
				Usernames:   []string{UserId},
				RangeValues: []int{50, 90},
				Duration:    "24h",
				DeviceId:    RegisteredDeviceId,
				QueryField:  RegisteredQueryField,
				AlertRange:  alertsource.LessThan,
			},
		},
	}
	var err error
	var as *alertstore.MockAlertStore
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			as = alertstore.NewMockAlertStore(tc.mockAlertStore)
			a.AlertStore = as
			_, err = a.UpdateAlertRule(
				context.Background(),
				&struct {
					AlertId string `path:"alertId"`
					Body    alertstore.AlertRule
				}{tc.alertId, tc.input},
			)
			if tc.wantErr != (err != nil) {
				t.Fatalf("wantErr: %v, got: %v", tc.wantErr, err)
			}
			if !tc.wantErr {
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
		want           *struct{ Body alertstore.AlertRule }
		input          alertsource.CreatedRule
		mockAlertStore *alertstore.Schema
	}{

		"successfully get a stored alert": {
			wantErr: false,
			input:   alertsource.CreatedRule{AlertId: AlertId},
			mockAlertStore: &alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, UserId: UserId},
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
						Id:          AlertId,
						Duration:    SomeDuration,
						RangeValues: []int{10, 20},
						Level:       alertsource.Ok,
					},
				},
			},
			want: &struct{ Body alertstore.AlertRule }{
				alertstore.AlertRule{
					Usernames:   []string{UserId},
					RangeValues: []int{10, 20},
					Duration:    "24h",
					DeviceId:    RegisteredDeviceId,
					QueryField:  RegisteredQueryField,
					AlertRange:  alertsource.Range,
				},
			},
		},
	}

	var err error
	var got *struct{ Body alertstore.AlertRule }
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err = a.GetAlertRule(
				context.Background(),
				&struct{ Body alertsource.CreatedRule }{tc.input},
			)
			if tc.wantErr != (err != nil) {
				t.Fatalf("wantErr: %v, got: %v", tc.wantErr, err)
			}
			if !tc.wantErr {
				if diff := cmp.Diff(tc.want, got); diff != "" {
					t.Fatalf("response mismatch (-want +got):\n%s", diff)
				}
			}

		})
	}
}
