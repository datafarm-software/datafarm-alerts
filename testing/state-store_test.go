package testing

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/segmentio/encoding/json"

	"github.com/stretchr/testify/require"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/geraud22/datafarm-alerts/alertsource"
	"github.com/geraud22/datafarm-alerts/alertstore"
	"github.com/geraud22/datafarm-alerts/app"
	"github.com/geraud22/datafarm-alerts/notifier"
	"github.com/geraud22/datafarm-alerts/templates"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var EmptyInboxes = notifier.Schema{
	Notifications: map[string][]notifier.AlertNotification{},
	CreatedAlerts: map[string][]notifier.AlertCreated{},
	DeletedAlerts: map[string][]notifier.AlertDeleted{},
}
var EmptyAlertStore = alertstore.Schema{}

const ConfigPath = "../config.yml"
const UnregisteredQueryField = "humidity"
const RegisteredQueryField = "temperature"
const UnregisteredDeviceId = "device2"
const RegisteredDeviceId = "device1"
const SomeDuration = (24 * time.Hour)
const AlertId = "alert1"
const UnregisteredAlertId = "alertUnregistered"
const UserEmail = "user1@alert.com"
const AnotherUserEmail = "another@email.com"
const RegisteredAlertName = "Temperature Check"
const NewValue = 20
const OldValue = 10
const GreaterThanTen = "10.00 <"
const LessThanTen = "< 10.00"
const GreaterThanFive = "5.00 <"
const LessThanFive = "< 5.00"
const InactiveSensorCondition = "Inactive Sensor"
const RangeCondition = "> 10.00, 20.00 <"

var AlertReceived = time.Date(1994, 6, 24, 6, 0, 0, 0, time.UTC)
var BeforeDuration = AlertReceived.Add(-1 * time.Hour)
var AfterDuration = AlertReceived.Add(25 * time.Hour)
var OnDuration = AlertReceived.Add(24 * time.Hour)
var WithinDuration = AlertReceived.Add(6 * time.Hour)
var SixtyThousandLitres = alertstore.LinearCalibration{
	RawMin: 4,
	RawMax: 10.6,
	CalMin: 0,
	CalMax: 97000,
}

func TestShouldNotify(t *testing.T) {
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

		"crit exactly on duration, so notify": {
			want: true,
			alert: alertstore.AlertState{
				Level:    alertstore.Crit,
				Time:     OnDuration,
				Duration: SomeDuration,
			},
			previous: alertstore.AlertState{
				Level:    alertstore.Crit,
				Time:     AlertReceived,
				Duration: SomeDuration,
			},
		},

		"notify on first state received, not due to alert rule registration": {
			want: true,
			alert: alertstore.AlertState{
				Level:       alertstore.Crit,
				Time:        AlertReceived,
				Duration:    SomeDuration,
				RangeValues: []float32{10, 20},
				Id:          AlertId,
			},
			previous: alertstore.AlertState{
				Duration:    SomeDuration,
				RangeValues: []float32{10, 20},
				Id:          AlertId,
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
				Level:    alertstore.Ok,
				Time:     WithinDuration,
				Duration: SomeDuration,
			},
			previous: alertstore.AlertState{
				Level: alertstore.Crit,
				Time:  WithinDuration,
			},
		},

		"change to warn, so notify": {
			want: true,
			alert: alertstore.AlertState{
				Level: alertstore.Warn,
			},
			previous: alertstore.AlertState{
				Level: alertstore.Ok,
			},
		},

		"change from warn to ok, so do nothing": {
			want: false,
			alert: alertstore.AlertState{
				Level: alertstore.Ok,
			},
			previous: alertstore.AlertState{
				Level: alertstore.Warn,
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a, _, closeFunc := setupApp(t, alertstore.Schema{})
			defer closeFunc()
			got = a.ShouldNotify(tc.alert, tc.previous)
			if got != tc.want {
				t.Fatalf("tc.want = %v, got = %v", tc.want, got)
			}
		})
	}
}

func TestRespondToAlert(t *testing.T) {
	tests := map[string]struct {
		wantStatus                     int
		wantUserInboxes                notifier.Schema
		mockAlertStore, wantAlertStore alertstore.Schema
		alert                          alertsource.InfluxAlert
	}{

		"send alert because still crit after duration": {
			wantStatus: http.StatusNoContent,
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{},
				Notifications: map[string][]notifier.AlertNotification{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							Value:       NewValue,
							QueryField:  RegisteredQueryField,
							AlertLevel:  alertstore.Crit,
							Timestamp:   AfterDuration,
							AlertType:   alertstore.GreaterThan,
							AlertValues: []float32{OldValue},
							Duration:    "24h",
							Condition:   GreaterThanTen,
						},
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
						Duration:    "24h",
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Duration: SomeDuration,
						Value:    OldValue,
						Level:    alertstore.Crit,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:              AlertId,
				Level:           "crit",
				Value:           NewValue,
				SensorTimestamp: AfterDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
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
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AfterDuration,
						Value:    NewValue,
						Duration: SomeDuration,
						Level:    alertstore.Crit,
					},
				},
			},
		},

		"send alert based on linear calibration": {
			wantStatus: http.StatusNoContent,
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{},
				Notifications: map[string][]notifier.AlertNotification{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							Value:       59831.36,
							QueryField:  "pressure",
							AlertLevel:  alertstore.Warn,
							Timestamp:   AfterDuration,
							AlertType:   alertstore.LessThan,
							AlertValues: []float32{60000},
							Duration:    "24h",
							Condition:   "< 60000.00",
						},
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
						QueryField: "pressure",
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:  AlertId,
						Duration: "24h",
						RangeValues: []float32{
							SixtyThousandLitres.ToRaw(60000).F32(),
						},
						Name:       RegisteredAlertName,
						AlertType:  alertstore.LessThan,
						AlertLevel: alertstore.Warn,
					},
				},
				LinearCalibrations: []alertstore.StoredLinearCalibration{
					{
						AlertId: AlertId,
						LinearCalibration: &alertstore.LinearCalibration{
							RawMin: 4,
							RawMax: 10.6,
							CalMax: 97000,
						},
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Duration: SomeDuration,
						Value:    OldValue,
						Level:    alertstore.Crit,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:              AlertId,
				Level:           "warn",
				Value:           8.071,
				SensorTimestamp: AfterDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      "pressure",
			},
			wantAlertStore: alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: "pressure",
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:  AlertId,
						Duration: "24h",
						RangeValues: []float32{
							SixtyThousandLitres.ToRaw(60000).F32(),
						},
						Name:       RegisteredAlertName,
						AlertType:  alertstore.LessThan,
						AlertLevel: alertstore.Warn,
					},
				},
				LinearCalibrations: []alertstore.StoredLinearCalibration{
					{
						AlertId: AlertId,
						LinearCalibration: &alertstore.LinearCalibration{
							RawMin: 4,
							RawMax: 10.6,
							CalMax: 97000,
						},
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AfterDuration,
						Value:    8.071,
						Duration: SomeDuration,
						Level:    alertstore.Warn,
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
						Value:    OldValue,
						Level:    alertstore.Crit,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:              AlertId,
				Level:           "crit",
				Value:           NewValue,
				SensorTimestamp: WithinDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertRecipients:    []alertstore.AlertRecipient{},
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
						Value:    OldValue,
						Duration: SomeDuration,
						Level:    alertstore.Crit,
					},
				},
			},
		},

		"no notification because going from crit to warn": {
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
						Value:    OldValue,
						Level:    alertstore.Crit,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:              AlertId,
				Level:           "warn",
				Value:           NewValue,
				SensorTimestamp: WithinDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertRecipients:    []alertstore.AlertRecipient{},
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
						Time:     time.Unix(0, WithinDuration.UnixNano()),
						Value:    NewValue,
						Duration: SomeDuration,
						Level:    alertstore.Warn,
					},
				},
			},
		},

		"no notification because going from crit to ok": {
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
						Value:    OldValue,
						Level:    alertstore.Crit,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:              AlertId,
				Level:           "ok",
				Value:           NewValue,
				SensorTimestamp: WithinDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertRecipients:    []alertstore.AlertRecipient{},
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
						Time:     time.Unix(0, WithinDuration.UnixNano()),
						Value:    NewValue,
						Duration: SomeDuration,
						Level:    alertstore.Ok,
					},
				},
			},
		},

		"send notification because alert level changing from ok to crit": {
			wantStatus: http.StatusNoContent,
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{},
				Notifications: map[string][]notifier.AlertNotification{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Value:       NewValue,
							AlertLevel:  alertstore.Crit,
							Timestamp:   WithinDuration,
							AlertType:   alertstore.GreaterThan,
							AlertValues: []float32{OldValue},
							Duration:    "24h",
							Condition:   GreaterThanTen,
						},
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
						Duration:    "24h",
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Value:    OldValue,
						Duration: SomeDuration,
						Level:    alertstore.Ok,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:              AlertId,
				Level:           "crit",
				Value:           NewValue,
				SensorTimestamp: WithinDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
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
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Value:    NewValue,
						Time:     WithinDuration,
						Duration: SomeDuration,
						Level:    alertstore.Crit,
					},
				},
			},
		},

		"send notification because alert level changing from ok to warn": {
			wantStatus: http.StatusNoContent,
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{},
				Notifications: map[string][]notifier.AlertNotification{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Value:       NewValue,
							AlertLevel:  alertstore.Warn,
							Timestamp:   WithinDuration,
							AlertType:   alertstore.GreaterThan,
							AlertValues: []float32{OldValue},
							Duration:    "24h",
							Condition:   GreaterThanTen,
						},
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
						Duration:    "24h",
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Value:    OldValue,
						Duration: SomeDuration,
						Level:    alertstore.Ok,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:              AlertId,
				Level:           "warn",
				Value:           NewValue,
				SensorTimestamp: WithinDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
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
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Value:    NewValue,
						Time:     WithinDuration,
						Duration: SomeDuration,
						Level:    alertstore.Warn,
					},
				},
			},
		},

		"send notification because alert level changing from warn to crit": {
			wantStatus: http.StatusNoContent,
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{},
				Notifications: map[string][]notifier.AlertNotification{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Value:       NewValue,
							AlertLevel:  alertstore.Crit,
							Timestamp:   WithinDuration,
							AlertType:   alertstore.GreaterThan,
							AlertValues: []float32{OldValue},
							Duration:    "24h",
							Condition:   GreaterThanTen,
						},
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
						Duration:    "24h",
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Value:    OldValue,
						Duration: SomeDuration,
						Level:    alertstore.Warn,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:              AlertId,
				Level:           "crit",
				Value:           NewValue,
				SensorTimestamp: WithinDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
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
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Value:    NewValue,
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
						Value:    OldValue,
						Id:       AlertId,
						Level:    alertstore.Ok,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         AlertId,
				Level:      "crit",
				Value:      NewValue,
				DeviceId:   RegisteredDeviceId,
				QueryField: UnregisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
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
						Value:    OldValue,
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
						Value:    OldValue,
						Id:       AlertId,
						Level:    alertstore.Ok,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         AlertId,
				Value:      NewValue,
				Level:      "crit",
				DeviceId:   UnregisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
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
						Value:    NewValue,
						Duration: SomeDuration,
						Id:       AlertId,
						Level:    alertstore.Ok,
					},
				},
			},
		},

		"notification on first alert": {
			wantStatus: http.StatusNoContent,
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{},
				Notifications: map[string][]notifier.AlertNotification{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Value:       NewValue,
							AlertLevel:  alertstore.Crit,
							Timestamp:   AfterDuration,
							AlertType:   alertstore.GreaterThan,
							AlertValues: []float32{OldValue},
							Duration:    "24h",
							Condition:   GreaterThanTen,
						},
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
						Duration:    "24h",
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
				AlertStates: []alertstore.AlertState{},
			},
			alert: alertsource.InfluxAlert{
				Id:              AlertId,
				Level:           "crit",
				Value:           NewValue,
				SensorTimestamp: AfterDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
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
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Value:    NewValue,
						Time:     AfterDuration,
						Duration: SomeDuration,
						Level:    alertstore.Crit,
					},
				},
			},
		},

		"no notification first alert": {
			wantStatus:      http.StatusNoContent,
			wantUserInboxes: EmptyInboxes,
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
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
				AlertStates: []alertstore.AlertState{},
			},
			alert: alertsource.InfluxAlert{
				Id:              AlertId,
				Level:           "ok",
				Value:           NewValue,
				SensorTimestamp: AfterDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
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
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Value:    NewValue,
						Time:     AfterDuration,
						Duration: SomeDuration,
						Level:    alertstore.Ok,
					},
				},
			},
		},

		"no notification because alert status ok": {
			wantStatus:      http.StatusNoContent,
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
						AlertId:     AlertId,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						RangeValues: []float32{10},
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Time:     AlertReceived,
						Duration: SomeDuration,
						Value:    11,
						Id:       AlertId,
						Level:    alertstore.Crit,
					},
				},
			},
			alert: alertsource.InfluxAlert{
				Id:         AlertId,
				Level:      "ok",
				Value:      9,
				DeviceId:   RegisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []alertstore.AlertRecipient{},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						RangeValues: []float32{10},
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Duration: SomeDuration,
						Id:       AlertId,
						Value:    9,
						Level:    alertstore.Ok,
					},
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a, api, closeFunc := setupApp(t, tc.mockAlertStore)
			defer closeFunc()
			resp := api.Post("/alert/notify", tc.alert)
			if resp.Code != tc.wantStatus {
				t.Fatalf("wantStatus: %d, response status: %d", tc.wantStatus, resp.Code)
			}
			if tc.wantStatus < 300 {
				if diff := cmp.Diff(tc.wantUserInboxes, a.Notifier.GetUserInboxes()); diff != "" {
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

func setupApp(t *testing.T,
	mockAlertStore alertstore.Schema) (*app.App, humatest.TestAPI, func() error) {
	var err error
	a := &app.App{}
	_, api := humatest.New(t)
	a.RegisterHumaOperations(api)
	a.Templater, err = templates.NewMockTemplater()
	require.Nil(t, err)
	a.Notifier, err = notifier.NewGomail(alertstore.Testing, notifier.GomailOpts{})
	require.Nil(t, err)
	a.AlertStore, err = alertstore.NewGorm(alertstore.Testing, "testing")
	require.Nil(t, err)
	err = a.AlertStore.PrepareDb(mockAlertStore)
	require.Nil(t, err)
	a.AlertSource, err = alertsource.NewTestingInflux(ConfigPath)
	require.Nil(t, err)
	alertRules := SchemaToAlertRules(mockAlertStore)
	err = a.AlertSource.PrepareDb(alertRules...)
	require.Nil(t, err)
	return a, api, func() error {
		if err := a.Notifier.Close(); err != nil {
			return err
		}
		if err := a.AlertStore.Close(); err != nil {
			return err
		}
		if err := a.AlertSource.Close(); err != nil {
			return err
		}
		return nil
	}
}

func TestCreateAlertRule(t *testing.T) {
	tests := map[string]struct {
		wantErr                        bool
		wantResponse                   alertstore.CreatedRule
		wantStatus                     int
		wantAlertStore, mockAlertStore alertstore.Schema
		wantAlertSourceCount           int
		wantUserInboxes                notifier.Schema
		alertRule                      alertstore.AlertRule
	}{

		"successfully create a less than alert rule": {
			wantErr:              false,
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			mockAlertStore:       alertstore.Schema{},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Id:          AlertId,
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Level:       alertstore.Crit,
					},
				},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				Notifications: map[string][]notifier.AlertNotification{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.LessThan,
							AlertValues: []float32{5},
							Condition:   LessThanFive,
						},
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
				AlertLevel:     alertstore.Crit,
			},
			wantResponse: alertstore.CreatedRule{
				AlertId: AlertId,
			},
		},

		"deal with conflict on alert rule name": {
			wantErr:              false,
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 2,
			mockAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Id:          AlertId,
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Level:       alertstore.Crit,
					},
				},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Id:          AlertId,
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Level:       alertstore.Crit,
					},
					{
						Id:          "alert2",
						Duration:    SomeDuration,
						RangeValues: []float32{10},
						Level:       alertstore.Crit,
					},
				},
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
					{AlertId: "alert2", Email: AnotherUserEmail},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Crit,
					},
					{
						AlertId:     "alert2",
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				Notifications: map[string][]notifier.AlertNotification{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					AnotherUserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.GreaterThan,
							AlertValues: []float32{10},
							Condition:   GreaterThanTen,
						},
					},
				},
			},
			alertRule: alertstore.AlertRule{
				RangeValues:    []float32{10},
				Name:           RegisteredAlertName,
				Duration:       "24h",
				EmailAddresses: []string{AnotherUserEmail},
				DeviceId:       "device1",
				QueryField:     "temperature",
				AlertType:      alertstore.GreaterThan,
				AlertLevel:     alertstore.Crit,
			},
			wantResponse: alertstore.CreatedRule{
				AlertId: "alert2",
			},
		},

		"successfully create an inactive sensor alert rule": {
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
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Duration: SomeDuration,
						Level:    alertstore.Crit,
					},
				},
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
						Name:       RegisteredAlertName,
						AlertId:    AlertId,
						Duration:   "24h",
						AlertType:  alertstore.InactiveSensor,
						AlertLevel: alertstore.Crit,
					},
				},
			},
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				Notifications: map[string][]notifier.AlertNotification{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:       RegisteredAlertName,
							DeviceId:   RegisteredDeviceId,
							QueryField: RegisteredQueryField,
							Duration:   "24h",
							AlertLevel: alertstore.Crit,
							AlertType:  alertstore.InactiveSensor,
							Condition:  InactiveSensorCondition,
						},
					},
				},
			},
			alertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				Duration:       "24h",
				EmailAddresses: []string{UserEmail},
				DeviceId:       "device1",
				QueryField:     "temperature",
				AlertType:      alertstore.InactiveSensor,
				AlertLevel:     alertstore.Crit,
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
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Id:          AlertId,
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Level:       alertstore.Crit,
					},
				},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				Notifications: map[string][]notifier.AlertNotification{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.GreaterThan,
							AlertValues: []float32{5},
							Condition:   GreaterThanFive,
						},
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
				AlertLevel:     alertstore.Crit,
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
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Id:          AlertId,
						Duration:    SomeDuration,
						RangeValues: []float32{10, 20},
						Level:       alertstore.Crit,
					},
				},
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
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   alertstore.Range,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				Notifications: map[string][]notifier.AlertNotification{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
					},
				},
			},
			alertRule: alertstore.AlertRule{
				RangeValues:    []float32{10, 20},
				Name:           RegisteredAlertName,
				Duration:       "24h",
				EmailAddresses: []string{UserEmail},
				DeviceId:       "device1",
				QueryField:     "temperature",
				AlertType:      alertstore.Range,
				AlertLevel:     alertstore.Crit,
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
			wantAlertStore:  EmptyAlertStore,
			wantUserInboxes: EmptyInboxes,
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
			wantAlertStore:  EmptyAlertStore,
			wantUserInboxes: EmptyInboxes,
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

		"identical alert except name and emails, so return existing alert id": {
			wantErr:              false,
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			wantResponse: alertstore.CreatedRule{
				AlertId: AlertId,
			},
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				Notifications: map[string][]notifier.AlertNotification{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					AnotherUserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.LessThan,
							AlertValues: []float32{5},
							Condition:   LessThanFive,
						},
					},
					"user3@alert.com": {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.LessThan,
							AlertValues: []float32{5},
							Condition:   LessThanFive,
						},
					},
				},
			},
			alertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				RangeValues:    []float32{5},
				Duration:       "24h",
				EmailAddresses: []string{AnotherUserEmail, "user3@alert.com"},
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.LessThan,
				AlertLevel:     alertstore.Crit,
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
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Crit,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Id:          AlertId,
						Level:       alertstore.Crit,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
					{AlertId: AlertId, Email: AnotherUserEmail},
					{AlertId: AlertId, Email: "user3@alert.com"},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Crit,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Id:          AlertId,
						Level:       alertstore.Crit,
					},
				},
			},
		},

		"identical alert including user email, so conflict": {
			wantErr:    true,
			wantStatus: http.StatusConflict,
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
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
			wantUserInboxes: EmptyInboxes,
			alertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				RangeValues:    []float32{5},
				Duration:       "24h",
				EmailAddresses: []string{UserEmail},
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.LessThan,
				AlertLevel:     alertstore.Crit,
			},
		},

		"similar alert rule exists, but new one on different queryField": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			wantResponse: alertstore.CreatedRule{
				AlertId: "alert2",
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Id:          AlertId,
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Level:       alertstore.Crit,
					},
					{
						Id:          "alert2",
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Level:       alertstore.Crit,
					},
				},
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
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
						Name:        "Similar Alert",
					},
					{
						AlertId:     "alert2",
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
						Name:        "Similar Alert 2",
					},
				},
			},
			wantAlertSourceCount: 2,
			mockAlertStore: alertstore.Schema{
				AlertStates: []alertstore.AlertState{
					{
						Id:          AlertId,
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Level:       alertstore.Crit,
					},
				},
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
						Name:        "Similar Alert",
						Duration:    "24h",
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				Notifications: map[string][]notifier.AlertNotification{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:        "Similar Alert 2",
							DeviceId:    RegisteredDeviceId,
							QueryField:  "humidity",
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.GreaterThan,
							AlertValues: []float32{5},
							Condition:   GreaterThanFive,
						},
					},
				},
			},
			alertRule: alertstore.AlertRule{
				Name:           "Similar Alert 2",
				RangeValues:    []float32{5},
				EmailAddresses: []string{UserEmail},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     "humidity",
				AlertType:      alertstore.GreaterThan,
				AlertLevel:     alertstore.Crit,
			},
		},

		"allow multiple alerts on deviceId and queryField combination": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			wantResponse: alertstore.CreatedRule{
				AlertId: "alert2",
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Id:          "alert2",
						Duration:    SomeDuration,
						RangeValues: []float32{10, 20},
						Level:       alertstore.Crit,
					},
				},
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
						AlertLevel:  alertstore.Crit,
						Name:        RegisteredAlertName,
					},
					{
						AlertId:     "alert2",
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertType:   alertstore.Range,
						AlertLevel:  alertstore.Crit,
						Name:        "Some Other Name",
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
			wantAlertSourceCount: 2,
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				Notifications: map[string][]notifier.AlertNotification{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:        "Some Other Name",
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
					},
					"user2@alert.com": {
						{
							Name:        "Some Other Name",
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
					},
				},
			},
			alertRule: alertstore.AlertRule{
				Name:           "Some Other Name",
				EmailAddresses: []string{UserEmail, "user2@alert.com"},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.Range,
				AlertLevel:     alertstore.Crit,
			},
		},

		"allow multiple alerts of the same type, on deviceId and queryField combination": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			wantResponse: alertstore.CreatedRule{
				AlertId: "alert2",
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Id:          "alert2",
						Duration:    SomeDuration,
						RangeValues: []float32{10},
						Level:       alertstore.Crit,
					},
				},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Warn,
					},
					{
						AlertId:     "alert2",
						Name:        "Some alert name",
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Crit,
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Warn,
					},
				},
			},
			wantAlertSourceCount: 2,
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				Notifications: map[string][]notifier.AlertNotification{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:        "Some alert name",
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.LessThan,
							AlertValues: []float32{10},
							Condition:   LessThanTen,
						},
					},
					"user2@alert.com": {
						{
							Name:        "Some alert name",
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.LessThan,
							AlertValues: []float32{10},
							Condition:   LessThanTen,
						},
					},
				},
			},
			alertRule: alertstore.AlertRule{
				Name:           "Some alert name",
				EmailAddresses: []string{UserEmail, "user2@alert.com"},
				RangeValues:    []float32{10},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.LessThan,
				AlertLevel:     alertstore.Crit,
			},
		},

		"successfully create a warning level alert rule": {
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
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Id:          AlertId,
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Level:       alertstore.Warn,
					},
				},
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
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Warn,
					},
				},
			},
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				Notifications: map[string][]notifier.AlertNotification{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Warn,
							AlertType:   alertstore.LessThan,
							AlertValues: []float32{5},
							Condition:   LessThanFive,
						},
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
				AlertLevel:     alertstore.Warn,
			},
			wantResponse: alertstore.CreatedRule{
				AlertId: AlertId,
			},
		},

		"create a warning level alert rule with a linear calibration": {
			wantErr:              false,
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			mockAlertStore: alertstore.Schema{
				DeviceAlerts:       []alertstore.DeviceAlertRule{},
				AlertRecipients:    []alertstore.AlertRecipient{},
				AlertStates:        []alertstore.AlertState{},
				AlertRules:         []alertstore.StoredAlertRule{},
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
			},
			wantAlertStore: alertstore.Schema{
				AlertStates: []alertstore.AlertState{
					{
						Id:       AlertId,
						Duration: SomeDuration,
						//NOTE: the raw value according to the calibration gets stored
						RangeValues: []float32{8.082474},
						Level:       alertstore.Warn,
					},
				},
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: "pressure",
						AlertId:    AlertId,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId: AlertId,
						//NOTE: the raw value according to the calibration gets stored
						RangeValues: []float32{8.082474},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Warn,
					},
				},
				LinearCalibrations: []alertstore.StoredLinearCalibration{
					{
						AlertId: AlertId,
						LinearCalibration: &alertstore.LinearCalibration{
							RawMin: 4,
							RawMax: 10.6,
							CalMax: 97000,
						},
					},
				},
			},
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				Notifications: map[string][]notifier.AlertNotification{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  "pressure",
							Duration:    "24h",
							AlertLevel:  alertstore.Warn,
							AlertType:   alertstore.LessThan,
							AlertValues: []float32{60000},
							Condition:   "< 60000.00",
							LinearCalibration: &alertstore.LinearCalibration{
								RawMin: 4,
								RawMax: 10.6,
								CalMax: 97000,
							},
						},
					},
				},
			},
			alertRule: alertstore.AlertRule{
				RangeValues:    []float32{60000},
				Name:           RegisteredAlertName,
				Duration:       "24h",
				EmailAddresses: []string{UserEmail},
				DeviceId:       "device1",
				QueryField:     "pressure",
				AlertType:      alertstore.LessThan,
				AlertLevel:     alertstore.Warn,
				LinearCalibration: &alertstore.LinearCalibration{
					RawMin: 4,
					RawMax: 10.6,
					CalMax: 97000,
				},
			},
			wantResponse: alertstore.CreatedRule{
				AlertId: AlertId,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a, api, closeFunc := setupApp(t, tc.mockAlertStore)
			defer closeFunc()
			alertRules := SchemaToAlertRules(tc.mockAlertStore)
			err := a.AlertSource.PrepareDb(alertRules...)
			require.Nil(t, err)
			resp := api.Post("/alert", tc.alertRule)
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatusCode: %d, response code: %d", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				var got alertstore.CreatedRule
				err = json.Unmarshal(resp.Body.Bytes(), &got)
				require.Nil(t, err)
				gotAlertSourceCount := a.AlertSource.GetSnapshot()
				if tc.wantAlertSourceCount != gotAlertSourceCount {
					t.Fatalf(
						"wantAlertSourceCount: %d, got alertsCount: %d",
						tc.wantAlertSourceCount, gotAlertSourceCount)
				}
				if diff := cmp.Diff(tc.wantUserInboxes, a.Notifier.GetUserInboxes()); diff != "" {
					t.Fatalf("user inboxes mismatch (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(tc.wantAlertStore, a.AlertStore.GetSnapshot()); diff != "" {
					t.Fatalf("alertstore mismatch (-want +got):\n%s", diff)
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
		wantUserInboxes                notifier.Schema
		updatedAlertRule               alertstore.AlertRule
		mockAlertStore, wantAlertStore alertstore.Schema
		wantAlertSourceCount           int
	}{

		"update the range values of an alert": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.Range,
				AlertLevel:     alertstore.Crit,
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
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
						RangeValues: []float32{43, 90},
						Duration:    "24h",
						AlertType:   alertstore.Range,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10, 20},
						Id:          AlertId,
						Level:       alertstore.Crit,
					},
				},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertType:   alertstore.Range,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
		},

		"incoming alert is identical to what already exists, so send OK": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{12, 24},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.Range,
				AlertLevel:     alertstore.Crit,
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{12, 24},
						Duration:    "24h",
						AlertType:   alertstore.Range,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates:        []alertstore.AlertState{},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{12, 24},
						Duration:    "24h",
						AlertType:   alertstore.Range,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
		},

		"update alert to become identical to an existing alert, still keep both alerts": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 2,
			alertId:              "alert2",
			updatedAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				EmailAddresses: []string{"user2@alerts.com"},
				RangeValues:    []float32{10},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.LessThan,
				AlertLevel:     alertstore.Crit,
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					"user2@alerts.com": {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.LessThan,
							AlertValues: []float32{10},
							Condition:   LessThanTen,
						},
					},
				},
			},
			mockAlertStore: alertstore.Schema{
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
					{AlertId: "alert2", Email: "user2@alerts.com"},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Crit,
					},
					{
						AlertId:     "alert2",
						Name:        "Some other alert name",
						RangeValues: []float32{43, 90},
						Duration:    "24h",
						AlertType:   alertstore.Range,
						AlertLevel:  alertstore.Crit,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10},
						Id:          AlertId,
						Level:       alertstore.Crit,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10},
						Id:          AlertId,
						Level:       alertstore.Crit,
					},
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10},
						Id:          "alert2",
						Level:       alertstore.Crit,
					},
				},
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
					{AlertId: "alert2", Email: "user2@alerts.com"},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Crit,
					},
					{
						AlertId:     "alert2",
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
		},

		"update the last state of an alert": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.Range,
				AlertLevel:     alertstore.Crit,
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{43, 90},
						Duration:    "24h",
						AlertType:   alertstore.Range,
						AlertLevel:  alertstore.Crit,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Time:        AlertReceived,
						Duration:    SomeDuration,
						RangeValues: []float32{43, 90},
						Id:          AlertId,
						Level:       alertstore.Ok,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertType:   alertstore.Range,
						AlertLevel:  alertstore.Crit,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10, 20},
						Id:          AlertId,
						Level:       alertstore.Crit,
					},
				},
			},
		},

		"update alert type from less than to greater than": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.GreaterThan,
				AlertLevel:     alertstore.Crit,
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.GreaterThan,
							AlertValues: []float32{10},
							Condition:   GreaterThanTen,
						},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10},
						Id:          AlertId,
						Level:       alertstore.Crit,
					},
				},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
		},

		"update alert type from greater than to less than": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.LessThan,
				AlertLevel:     alertstore.Crit,
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.LessThan,
							AlertValues: []float32{10},
							Condition:   LessThanTen,
						},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10},
						Id:          AlertId,
						Level:       alertstore.Crit,
					},
				},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
		},

		"update alert to include a linear calibration": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: alertstore.AlertRule{
				Name:              RegisteredAlertName,
				AlertId:           AlertId,
				EmailAddresses:    []string{UserEmail},
				RangeValues:       []float32{60000},
				Duration:          "24h",
				DeviceId:          RegisteredDeviceId,
				QueryField:        RegisteredQueryField,
				AlertType:         alertstore.GreaterThan,
				AlertLevel:        alertstore.Crit,
				LinearCalibration: &SixtyThousandLitres,
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:              RegisteredAlertName,
							DeviceId:          RegisteredDeviceId,
							QueryField:        RegisteredQueryField,
							Duration:          "24h",
							AlertLevel:        alertstore.Crit,
							AlertType:         alertstore.GreaterThan,
							AlertValues:       []float32{60000},
							Condition:         "60000.00 <",
							LinearCalibration: &SixtyThousandLitres,
						},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				AlertStates: []alertstore.AlertState{
					{
						Duration: SomeDuration,
						RangeValues: []float32{
							SixtyThousandLitres.ToRaw(60000).F32(),
						},
						Id:    AlertId,
						Level: alertstore.Crit,
					},
				},
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
						AlertId: AlertId,
						Name:    RegisteredAlertName,
						RangeValues: []float32{
							SixtyThousandLitres.ToRaw(60000).F32(),
						},
						Duration:   "24h",
						AlertType:  alertstore.GreaterThan,
						AlertLevel: alertstore.Crit,
					},
				},
				LinearCalibrations: []alertstore.StoredLinearCalibration{
					{
						AlertId:           AlertId,
						LinearCalibration: &SixtyThousandLitres,
					},
				},
			},
		},

		"change range values back to calibrated if existing": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: alertstore.AlertRule{
				Name:              RegisteredAlertName,
				AlertId:           AlertId,
				EmailAddresses:    []string{UserEmail},
				RangeValues:       []float32{60000},
				Duration:          "24h",
				DeviceId:          RegisteredDeviceId,
				QueryField:        RegisteredQueryField,
				AlertType:         alertstore.GreaterThan,
				AlertLevel:        alertstore.Crit,
				LinearCalibration: &SixtyThousandLitres,
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{},
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
						AlertId: AlertId,
						Name:    RegisteredAlertName,
						RangeValues: []float32{
							SixtyThousandLitres.ToRaw(60000).F32(),
						},
						Duration:   "24h",
						AlertType:  alertstore.GreaterThan,
						AlertLevel: alertstore.Crit,
					},
				},
				LinearCalibrations: []alertstore.StoredLinearCalibration{
					{
						AlertId:           AlertId,
						LinearCalibration: &SixtyThousandLitres,
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
						AlertId: AlertId,
						Name:    RegisteredAlertName,
						RangeValues: []float32{
							SixtyThousandLitres.ToRaw(60000).F32(),
						},
						Duration:   "24h",
						AlertType:  alertstore.GreaterThan,
						AlertLevel: alertstore.Crit,
					},
				},
				LinearCalibrations: []alertstore.StoredLinearCalibration{
					{
						AlertId:           AlertId,
						LinearCalibration: &SixtyThousandLitres,
					},
				},
			},
		},

		"update alert type from less than to range": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.Range,
				AlertLevel:     alertstore.Crit,
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10, 20},
						Id:          AlertId,
						Level:       alertstore.Crit,
					},
				},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertType:   alertstore.Range,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
		},

		"can't update the queryField of an alert. Constitutes a new alert entirely": {
			wantStatus:           http.StatusNotFound,
			wantErr:              true,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     UnregisteredQueryField,
				AlertLevel:     alertstore.Crit,
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertLevel:  alertstore.Crit,
						AlertType:   alertstore.LessThan,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertLevel:  alertstore.Crit,
						AlertType:   alertstore.LessThan,
					},
				},
			},
		},

		"can't update the deviceId of an alert. Constitutes a new alert entirely": {
			wantStatus:           http.StatusNotFound,
			wantErr:              true,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       UnregisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     alertstore.Crit,
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertLevel:  alertstore.Crit,
						AlertType:   alertstore.LessThan,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertLevel:  alertstore.Crit,
						AlertType:   alertstore.LessThan,
					},
				},
			},
		},

		"can't update from threshold to inactive sensor alert type": {
			wantStatus:           http.StatusBadRequest,
			wantErr:              true,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     alertstore.Crit,
				AlertType:      alertstore.InactiveSensor,
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertLevel:  alertstore.Crit,
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertLevel:  alertstore.Crit,
						AlertType:   alertstore.LessThan,
					},
				},
			},
		},

		"update inactive sensor alert duration": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				Duration:       "48h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     alertstore.Crit,
				AlertType:      alertstore.InactiveSensor,
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:       RegisteredAlertName,
							DeviceId:   RegisteredDeviceId,
							QueryField: RegisteredQueryField,
							Duration:   "48h",
							AlertLevel: alertstore.Crit,
							AlertType:  alertstore.InactiveSensor,
							Condition:  InactiveSensorCondition,
						},
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
						AlertId:    AlertId,
						Name:       RegisteredAlertName,
						Duration:   "24h",
						AlertLevel: alertstore.Crit,
						AlertType:  alertstore.InactiveSensor,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Duration: 48 * time.Hour,
						Id:       AlertId,
						Level:    alertstore.Crit,
					},
				},
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
						AlertId:    AlertId,
						Name:       RegisteredAlertName,
						Duration:   "48h",
						AlertLevel: alertstore.Crit,
						AlertType:  alertstore.InactiveSensor,
					},
				},
			},
		},

		"add more emails to the recipient list of an alert rule": {
			wantStatus:           http.StatusOK,
			alertId:              AlertId,
			wantAlertSourceCount: 1,
			updatedAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail, "user2@alert.com"},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     alertstore.Crit,
				AlertType:      alertstore.Range,
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
					},
					"user2@alert.com": {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertLevel:  alertstore.Crit,
						AlertType:   alertstore.Range,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10, 20},
						Id:          AlertId,
						Level:       alertstore.Crit,
					},
				},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertLevel:  alertstore.Crit,
						AlertType:   alertstore.Range,
					},
				},
			},
		},

		"ignore duplicate emails in recipient list": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail, UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     alertstore.Crit,
				AlertType:      alertstore.Range,
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertLevel:  alertstore.Warn,
						AlertType:   alertstore.LessThan,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10, 20},
						Id:          AlertId,
						Level:       alertstore.Crit,
					},
				},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertLevel:  alertstore.Crit,
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
			updatedAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     alertstore.Crit,
				AlertType:      alertstore.Range,
			},
		},

		"invalid: 1 range value on a range type alert check": {
			wantStatus:     http.StatusBadRequest,
			wantErr:        true,
			mockAlertStore: alertstore.Schema{},
			alertId:        AlertId,
			updatedAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     alertstore.Crit,
				AlertType:      alertstore.Range,
			},
		},

		"invalid: 2 range values on a less than type alert check": {
			wantStatus:     http.StatusBadRequest,
			wantErr:        true,
			mockAlertStore: alertstore.Schema{},
			alertId:        AlertId,
			updatedAlertRule: alertstore.AlertRule{
				EmailAddresses: []string{UserEmail},
				Name:           RegisteredAlertName,
				RangeValues:    []float32{50, 90},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     alertstore.Crit,
				AlertType:      alertstore.LessThan,
			},
		},

		"update alert level from crit to warn": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: alertstore.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.GreaterThan,
				AlertLevel:     alertstore.Warn,
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Warn,
							AlertType:   alertstore.GreaterThan,
							AlertValues: []float32{10},
							Condition:   GreaterThanTen,
						},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Crit,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				AlertStates: []alertstore.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10},
						Id:          AlertId,
						Level:       alertstore.Warn,
					},
				},
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
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   alertstore.GreaterThan,
						AlertLevel:  alertstore.Warn,
					},
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a, api, closeFunc := setupApp(t, tc.mockAlertStore)
			defer closeFunc()
			resp := api.Put(fmt.Sprintf("/alert/%s", tc.alertId), tc.updatedAlertRule)
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatusCode: %d, response code: %d", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				var got alertstore.AlertRule
				err := json.Unmarshal(resp.Body.Bytes(), &got)
				require.Nil(t, err)
				tc.updatedAlertRule.AlertId = tc.alertId
				if diff := cmp.Diff(tc.updatedAlertRule, got); diff != "" {
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
				if diff := cmp.Diff(tc.wantUserInboxes, a.Notifier.GetUserInboxes()); diff != "" {
					t.Fatalf("user inboxes mismatch (-want +got):\n%s", diff)
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

		"successfully get a stored threshold alert": {
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
						Name:        RegisteredAlertName,
						Duration:    "24h",
						RangeValues: []float32{10, 20},
						AlertType:   alertstore.Range,
						AlertLevel:  alertstore.Warn,
					},
				},
			},
			want: alertstore.AlertRule{
				AlertId:        AlertId,
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.Range,
				AlertLevel:     alertstore.Warn,
			},
		},

		"successfully get a stored inactive sensor alert": {
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
						AlertId:    AlertId,
						Name:       RegisteredAlertName,
						Duration:   "24h",
						AlertType:  alertstore.InactiveSensor,
						AlertLevel: alertstore.Crit,
					},
				},
			},
			want: alertstore.AlertRule{
				AlertId:        AlertId,
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.InactiveSensor,
				AlertLevel:     alertstore.Crit,
			},
		},

		"alert doesn't exist": {
			wantErr:        true,
			want:           alertstore.AlertRule{},
			wantStatus:     404,
			alertId:        "alert2",
			mockAlertStore: alertstore.Schema{},
		},

		"successfully get a stored threshold alert with a linear calibration": {
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
						Name:        RegisteredAlertName,
						Duration:    "24h",
						RangeValues: []float32{10, 20},
						AlertType:   alertstore.Range,
						AlertLevel:  alertstore.Warn,
					},
				},
				LinearCalibrations: []alertstore.StoredLinearCalibration{
					{
						AlertId: AlertId,
						LinearCalibration: &alertstore.LinearCalibration{
							RawMin: 0,
							RawMax: 1,
							CalMin: 0,
							CalMax: 1,
						},
					},
				},
			},
			want: alertstore.AlertRule{
				AlertId:        AlertId,
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      alertstore.Range,
				AlertLevel:     alertstore.Warn,
				LinearCalibration: &alertstore.LinearCalibration{
					RawMin: 0,
					RawMax: 1,
					CalMin: 0,
					CalMax: 1,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, api, closeFunc := setupApp(t, tc.mockAlertStore)
			defer closeFunc()
			resp := api.Get(fmt.Sprintf("/alert/%s", tc.alertId))
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatusCode: %d, response code: %d", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				var got alertstore.AlertRule
				err := json.Unmarshal(resp.Body.Bytes(), &got)
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
		wantErr                        bool
		wantStatus                     int
		alertId                        string
		wantAlertRule                  alertstore.AlertRule
		wantUserInboxes                notifier.Schema
		mockAlertStore, wantAlertStore alertstore.Schema
		wantAlertSourceCount           int
	}{

		"delete an alert": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			alertId:    AlertId,
			wantUserInboxes: notifier.Schema{
				CreatedAlerts: map[string][]notifier.AlertCreated{},
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
					},
				},
			},
			wantAlertSourceCount: 0,
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
						Name:        RegisteredAlertName,
						AlertId:     AlertId,
						Duration:    "24h",
						RangeValues: []float32{10, 20},
						AlertLevel:  alertstore.Crit,
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
				DeviceAlerts:       []alertstore.DeviceAlertRule{},
				AlertRecipients:    []alertstore.AlertRecipient{},
				AlertStates:        []alertstore.AlertState{},
				AlertRules:         []alertstore.StoredAlertRule{},
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
			},
			wantAlertRule: alertstore.AlertRule{
				AlertId:        AlertId,
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     alertstore.Crit,
				AlertType:      alertstore.Range,
			},
		},

		"delete an alert which includes a linear calibration": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			alertId:    AlertId,
			wantUserInboxes: notifier.Schema{
				CreatedAlerts: map[string][]notifier.AlertCreated{},
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Crit,
							AlertType:   alertstore.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
							LinearCalibration: &alertstore.LinearCalibration{
								RawMin: 0,
								RawMax: 1,
								CalMin: 0,
								CalMax: 5,
							},
						},
					},
				},
			},
			wantAlertSourceCount: 0,
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
						Name:        RegisteredAlertName,
						AlertId:     AlertId,
						Duration:    "24h",
						RangeValues: []float32{10, 20},
						AlertLevel:  alertstore.Crit,
						AlertType:   alertstore.Range,
					},
				},
				LinearCalibrations: []alertstore.StoredLinearCalibration{
					{
						AlertId: AlertId,
						LinearCalibration: &alertstore.LinearCalibration{
							RawMax: 1,
							CalMax: 5,
						},
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id: AlertId,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				DeviceAlerts:       []alertstore.DeviceAlertRule{},
				AlertRecipients:    []alertstore.AlertRecipient{},
				AlertStates:        []alertstore.AlertState{},
				AlertRules:         []alertstore.StoredAlertRule{},
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
			},
			wantAlertRule: alertstore.AlertRule{
				AlertId:        AlertId,
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     alertstore.Crit,
				AlertType:      alertstore.Range,
				LinearCalibration: &alertstore.LinearCalibration{
					RawMax: 1,
					CalMax: 5,
				},
			},
		},

		"delete a specific alert": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			alertId:    "alert2",
			wantUserInboxes: notifier.Schema{
				CreatedAlerts: map[string][]notifier.AlertCreated{},
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{
					UserEmail: {
						{
							Name:        "Some other alert name",
							DeviceId:    "device2",
							QueryField:  "humidity",
							Duration:    "48h",
							AlertLevel:  alertstore.Warn,
							AlertType:   alertstore.LessThan,
							AlertValues: []float32{10},
							Condition:   LessThanTen,
						},
					},
				},
			},
			wantAlertSourceCount: 1,
			mockAlertStore: alertstore.Schema{
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
						DeviceId:   "device2",
						QueryField: "humidity",
						AlertId:    "alert2",
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						Duration:    "24h",
						RangeValues: []float32{10, 20},
						AlertLevel:  alertstore.Crit,
						AlertType:   alertstore.Range,
					},
					{
						AlertId:     "alert2",
						Name:        "Some other alert name",
						Duration:    "48h",
						RangeValues: []float32{10},
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Warn,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id: AlertId,
					},
					{
						Id: "alert2",
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
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
						Name:        RegisteredAlertName,
						Duration:    "24h",
						RangeValues: []float32{10, 20},
						AlertLevel:  alertstore.Crit,
						AlertType:   alertstore.Range,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id: AlertId,
					},
				},
			},
			wantAlertRule: alertstore.AlertRule{
				AlertId:        "alert2",
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10},
				Duration:       "48h",
				DeviceId:       "device2",
				QueryField:     "humidity",
				AlertType:      alertstore.LessThan,
				AlertLevel:     alertstore.Warn,
			},
		},

		"delete an inactive sensor alert": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			alertId:    AlertId,
			wantUserInboxes: notifier.Schema{
				CreatedAlerts: map[string][]notifier.AlertCreated{},
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{
					UserEmail: {
						{
							Name:       RegisteredAlertName,
							DeviceId:   RegisteredDeviceId,
							QueryField: RegisteredQueryField,
							Duration:   "24h",
							AlertLevel: alertstore.Crit,
							AlertType:  alertstore.InactiveSensor,
							Condition:  InactiveSensorCondition,
						},
					},
				},
			},
			wantAlertSourceCount: 0,
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
						AlertId:    AlertId,
						Name:       RegisteredAlertName,
						Duration:   "24h",
						AlertLevel: alertstore.Crit,
						AlertType:  alertstore.InactiveSensor,
					},
				},
				AlertStates: []alertstore.AlertState{
					{
						Id: AlertId,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				DeviceAlerts:       []alertstore.DeviceAlertRule{},
				AlertRecipients:    []alertstore.AlertRecipient{},
				AlertStates:        []alertstore.AlertState{},
				AlertRules:         []alertstore.StoredAlertRule{},
			},
			wantAlertRule: alertstore.AlertRule{
				AlertId:        AlertId,
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     alertstore.Crit,
				AlertType:      alertstore.InactiveSensor,
			},
		},

		"try to delete non existent alert": {
			wantErr:         true,
			wantStatus:      http.StatusNotFound,
			wantUserInboxes: EmptyInboxes,
			alertId:         "alert2",
			mockAlertStore:  alertstore.Schema{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a, api, closeFunc := setupApp(t, tc.mockAlertStore)
			defer closeFunc()
			resp := api.Delete(fmt.Sprintf("/alert/%s", tc.alertId))
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatus: %d, response Code: %v", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				var got alertstore.AlertRule
				err := json.Unmarshal(resp.Body.Bytes(), &got)
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
				if diff := cmp.Diff(tc.wantUserInboxes, a.Notifier.GetUserInboxes()); diff != "" {
					t.Fatalf("user inboxes mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestRemoveUsersFromAlertRule(t *testing.T) {
	tests := map[string]struct {
		wantErr                        bool
		mockAlertStore, wantAlertStore alertstore.Schema
		wantStatus                     int
		wantUserInboxes                notifier.Schema
		emailsToRemove                 []string
		alertId                        string
		wantAlertSourceCount           int
	}{

		"remove 1 email from alert rule": {
			mockAlertStore: alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []alertstore.AlertRecipient{
					{
						AlertId: AlertId,
						Email:   UserEmail,
					},
					{
						AlertId: AlertId,
						Email:   AnotherUserEmail,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Warn,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []alertstore.AlertRecipient{
					{
						AlertId: AlertId,
						Email:   UserEmail,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Warn,
					},
				},
				AlertStates: []alertstore.AlertState{},
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				CreatedAlerts: map[string][]notifier.AlertCreated{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{
					AnotherUserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Warn,
							AlertType:   alertstore.LessThan,
							AlertValues: []float32{OldValue},
							Condition:   LessThanTen,
						},
					},
				},
			},
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			emailsToRemove:       []string{AnotherUserEmail},
			alertId:              AlertId,
		},

		"try to remove email that isnt attached to the alert": {
			mockAlertStore: alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Warn,
					},
				},
				AlertStates: []alertstore.AlertState{},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Warn,
					},
				},
				AlertStates: []alertstore.AlertState{},
			},
			wantUserInboxes:      EmptyInboxes,
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			emailsToRemove:       []string{AnotherUserEmail},
			alertId:              AlertId,
		},

		"remove last email from alert, so delete alert": {
			mockAlertStore: alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Warn,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				DeviceAlerts:       []alertstore.DeviceAlertRule{},
				AlertRecipients:    []alertstore.AlertRecipient{},
				AlertRules:         []alertstore.StoredAlertRule{},
				AlertStates:        []alertstore.AlertState{},
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				CreatedAlerts: map[string][]notifier.AlertCreated{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Warn,
							AlertType:   alertstore.LessThan,
							AlertValues: []float32{OldValue},
							Condition:   LessThanTen,
						},
					},
				},
			},
			wantStatus:     http.StatusNoContent,
			emailsToRemove: []string{UserEmail},
			alertId:        AlertId,
		},

		"remove last email from alert among other unknown emails, so delete alert": {
			mockAlertStore: alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []alertstore.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Warn,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				DeviceAlerts:       []alertstore.DeviceAlertRule{},
				AlertRecipients:    []alertstore.AlertRecipient{},
				AlertRules:         []alertstore.StoredAlertRule{},
				AlertStates:        []alertstore.AlertState{},
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				CreatedAlerts: map[string][]notifier.AlertCreated{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Warn,
							AlertType:   alertstore.LessThan,
							AlertValues: []float32{OldValue},
							Condition:   LessThanTen,
						},
					},
				},
			},
			wantStatus:     http.StatusNoContent,
			emailsToRemove: []string{UserEmail, AnotherUserEmail},
			alertId:        AlertId,
		},

		"alert rule not found": {
			wantErr: true,
			mockAlertStore: alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []alertstore.AlertRecipient{
					{
						AlertId: AlertId,
						Email:   UserEmail,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Warn,
					},
				},
			},
			wantAlertStore:       alertstore.Schema{},
			wantStatus:           http.StatusNotFound,
			emailsToRemove:       []string{AnotherUserEmail},
			alertId:              UnregisteredAlertId,
			wantAlertSourceCount: 1,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a, api, closeFunc := setupApp(t, tc.mockAlertStore)
			defer closeFunc()
			resp := api.Patch(fmt.Sprintf("/alert/%s/emailaddresses", tc.alertId),
				tc.emailsToRemove)
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatus: %d, response Code: %v", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				if diff := cmp.Diff(tc.wantAlertStore, a.AlertStore.GetSnapshot()); diff != "" {
					t.Fatalf("alertStore mismatch (-want +got):\n%s", diff)
				}
				if tc.wantStatus != http.StatusNoContent {
					var mu alertstore.MutateAlertUsersResponse
					body := resp.Body.Bytes()
					err := json.Unmarshal(body, &mu)
					require.Nil(t, err)
					if diff := cmp.Diff(tc.emailsToRemove, mu.EmailAddresses); diff != "" {
						t.Fatalf("emails mismatch (-want +got):\n%s", diff)
					}
				}
				alertsCount := a.AlertSource.GetSnapshot()
				if tc.wantAlertSourceCount != alertsCount {
					t.Fatalf("wantAlertSourceCount: %d, got alertsCount: %d",
						tc.wantAlertSourceCount, alertsCount)
				}
				if diff := cmp.Diff(tc.wantUserInboxes, a.Notifier.GetUserInboxes()); diff != "" {
					t.Fatalf("user inboxes mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestAddUsersToAlertRule(t *testing.T) {
	tests := map[string]struct {
		wantErr                        bool
		mockAlertStore, wantAlertStore alertstore.Schema
		wantUserInboxes                notifier.Schema
		wantStatus                     int
		emailsToAdd                    []string
		alertId                        string
		wantAlertSourceCount           int
	}{

		"add 1 email to alert rule": {
			wantErr: false,
			mockAlertStore: alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []alertstore.AlertRecipient{
					{
						AlertId: AlertId,
						Email:   UserEmail,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Warn,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []alertstore.AlertRecipient{
					{
						AlertId: AlertId,
						Email:   UserEmail,
					},
					{
						AlertId: AlertId,
						Email:   AnotherUserEmail,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Warn,
					},
				},
				AlertStates: []alertstore.AlertState{},
			},
			wantStatus: http.StatusOK,
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				Notifications: map[string][]notifier.AlertNotification{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					AnotherUserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Warn,
							AlertType:   alertstore.LessThan,
							AlertValues: []float32{OldValue},
							Condition:   LessThanTen,
						},
					},
				},
			},
			emailsToAdd:          []string{AnotherUserEmail},
			alertId:              AlertId,
			wantAlertSourceCount: 1,
		},

		"some new emails, some old emails": {
			wantErr: false,
			mockAlertStore: alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []alertstore.AlertRecipient{
					{
						AlertId: AlertId,
						Email:   UserEmail,
					},
					{
						AlertId: AlertId,
						Email:   AnotherUserEmail,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Warn,
					},
				},
			},
			wantAlertStore: alertstore.Schema{
				LinearCalibrations: []alertstore.StoredLinearCalibration{},
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []alertstore.AlertRecipient{
					{
						AlertId: AlertId,
						Email:   UserEmail,
					},
					{
						AlertId: AlertId,
						Email:   AnotherUserEmail,
					},
					{
						AlertId: AlertId,
						Email:   "user3@alert.com",
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Warn,
					},
				},
				AlertStates: []alertstore.AlertState{},
			},
			wantUserInboxes: notifier.Schema{
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				Notifications: map[string][]notifier.AlertNotification{},
				CreatedAlerts: map[string][]notifier.AlertCreated{
					UserEmail: {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Warn,
							AlertType:   alertstore.LessThan,
							AlertValues: []float32{OldValue},
							Condition:   LessThanTen,
						},
					},
					"user3@alert.com": {
						{
							Name:        RegisteredAlertName,
							DeviceId:    RegisteredDeviceId,
							QueryField:  RegisteredQueryField,
							Duration:    "24h",
							AlertLevel:  alertstore.Warn,
							AlertType:   alertstore.LessThan,
							AlertValues: []float32{OldValue},
							Condition:   LessThanTen,
						},
					},
				},
			},
			wantStatus:           http.StatusOK,
			emailsToAdd:          []string{UserEmail, "user3@alert.com"},
			alertId:              AlertId,
			wantAlertSourceCount: 1,
		},

		"alert rule not found": {
			wantErr: true,
			mockAlertStore: alertstore.Schema{
				DeviceAlerts: []alertstore.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []alertstore.AlertRecipient{
					{
						AlertId: AlertId,
						Email:   UserEmail,
					},
				},
				AlertRules: []alertstore.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   alertstore.LessThan,
						AlertLevel:  alertstore.Warn,
					},
				},
			},
			wantAlertStore:       alertstore.Schema{},
			wantStatus:           http.StatusNotFound,
			emailsToAdd:          []string{AnotherUserEmail},
			alertId:              UnregisteredAlertId,
			wantAlertSourceCount: 1,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a, api, closeFunc := setupApp(t, tc.mockAlertStore)
			defer closeFunc()
			resp := api.Put(fmt.Sprintf("/alert/%s/emailaddresses", tc.alertId),
				tc.emailsToAdd)
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatus: %d, response Code: %v", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				if diff := cmp.Diff(tc.wantAlertStore, a.AlertStore.GetSnapshot()); diff != "" {
					t.Fatalf("alertStore mismatch (-want +got):\n%s", diff)
				}
				var mu alertstore.MutateAlertUsersResponse
				body := resp.Body.Bytes()
				err := json.Unmarshal(body, &mu)
				require.Nil(t, err)
				if diff := cmp.Diff(tc.emailsToAdd, mu.EmailAddresses); diff != "" {
					t.Fatalf("emails mismatch (-want +got):\n%s", diff)
				}
				alertsCount := a.AlertSource.GetSnapshot()
				if tc.wantAlertSourceCount != alertsCount {
					t.Fatalf("wantAlertSourceCount: %d, got alertsCount: %d",
						tc.wantAlertSourceCount, alertsCount)
				}
				if diff := cmp.Diff(tc.wantUserInboxes, a.Notifier.GetUserInboxes()); diff != "" {
					t.Fatalf("user inboxes mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func SchemaToAlertRules(dbRecord alertstore.Schema) []alertstore.AlertRule {
	var ar alertstore.AlertRule
	rules := make([]alertstore.AlertRule, 0, len(dbRecord.DeviceAlerts))
	rulesMap := make(map[string]alertstore.AlertRule, len(dbRecord.DeviceAlerts))
	for _, da := range dbRecord.DeviceAlerts {
		ar.AlertId = da.AlertId
		ar.DeviceId = da.DeviceId
		ar.QueryField = da.QueryField
		rulesMap[da.AlertId] = ar
	}
	for i, rule := range dbRecord.AlertRules {
		ar = rulesMap[rule.AlertId]
		ar.RangeValues = rule.RangeValues
		ar.Duration = rule.Duration
		ar.AlertType = rule.AlertType
		ar.AlertLevel = rule.AlertLevel
		if rule.Name == "" {
			rule.Name = fmt.Sprintf("DeviceId: %s, QueryField: %s %d", ar.DeviceId, ar.QueryField, i)
		}
		ar.Name = rule.Name
		rulesMap[rule.AlertId] = ar
	}
	for _, r := range rulesMap {
		rules = append(rules, r)
	}
	return rules
}
