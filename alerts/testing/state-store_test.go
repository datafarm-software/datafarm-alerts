package testing

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/datafarm-software/telemetry"
	"github.com/datafarm-software/telemetry/logging"
	"github.com/datafarm-software/telemetry/metering"
	"github.com/datafarm-software/telemetry/tracing"
	"github.com/gorilla/mux"
	"github.com/segmentio/encoding/json"

	"github.com/stretchr/testify/require"

	"github.com/danielgtaylor/huma/v2/adapters/humamux"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/datafarm-software/datafarm-alerts/alerts"
	"github.com/datafarm-software/datafarm-alerts/alerts/notifier"
	"github.com/datafarm-software/datafarm-alerts/alerts/source"
	"github.com/datafarm-software/datafarm-alerts/alerts/store"
	"github.com/datafarm-software/datafarm-alerts/alerts/templates"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var EmptyInboxes = notifier.Schema{
	Notifications: map[string][]notifier.AlertNotification{},
	CreatedAlerts: map[string][]notifier.AlertCreated{},
	DeletedAlerts: map[string][]notifier.AlertDeleted{},
}

const (
	ConfigPath              = "../../config.yml"
	UnregisteredQueryField  = "humidity"
	RegisteredQueryField    = "temperature"
	UnregisteredDeviceId    = "device2"
	RegisteredDeviceId      = "device1"
	SomeDuration            = (24 * time.Hour)
	AlertId                 = "alert1"
	UnregisteredAlertId     = "alertUnregistered"
	UserEmail               = "user1@alert.com"
	AnotherUserEmail        = "another@email.com"
	RegisteredAlertName     = "Temperature Check"
	NewValue                = 20
	OldValue                = 10
	GreaterThanTen          = "10.00 <"
	LessThanTen             = "< 10.00"
	GreaterThanFive         = "5.00 <"
	LessThanFive            = "< 5.00"
	InactiveSensorCondition = "Inactive Sensor"
	RangeCondition          = "> 10.00, 20.00 <"
)

var (
	AlertReceived       = time.Date(1994, 6, 24, 6, 0, 0, 0, time.UTC)
	BeforeDuration      = AlertReceived.Add(-1 * time.Hour)
	AfterDuration       = AlertReceived.Add(25 * time.Hour)
	OnDuration          = AlertReceived.Add(24 * time.Hour)
	WithinDuration      = AlertReceived.Add(6 * time.Hour)
	SixtyThousandLitres = store.LinearCalibration{
		RawMin: 4,
		RawMax: 10.6,
		CalMin: 0,
		CalMax: 97000,
	}
)

func TestShouldNotify(t *testing.T) {
	var got bool
	tests := map[string]struct {
		want            bool
		alert, previous store.AlertState
	}{
		"no change within duration, so do nothing": {
			want: false,
			alert: store.AlertState{
				Level:    store.Crit,
				Time:     WithinDuration,
				Duration: SomeDuration,
			},
			previous: store.AlertState{
				Level:    store.Crit,
				Time:     AlertReceived,
				Duration: SomeDuration,
			},
		},

		"still crit after duration, so notify": {
			want: true,
			alert: store.AlertState{
				Level:    store.Crit,
				Time:     AfterDuration,
				Duration: SomeDuration,
			},
			previous: store.AlertState{
				Level:    store.Crit,
				Time:     AlertReceived,
				Duration: SomeDuration,
			},
		},

		"crit exactly on duration, so notify": {
			want: true,
			alert: store.AlertState{
				Level:    store.Crit,
				Time:     OnDuration,
				Duration: SomeDuration,
			},
			previous: store.AlertState{
				Level:    store.Crit,
				Time:     AlertReceived,
				Duration: SomeDuration,
			},
		},

		"notify on first state received, not due to alert rule registration": {
			want: true,
			alert: store.AlertState{
				Level:       store.Crit,
				Time:        AlertReceived,
				Duration:    SomeDuration,
				RangeValues: []float32{10, 20},
				Id:          AlertId,
			},
			previous: store.AlertState{
				Duration:    SomeDuration,
				RangeValues: []float32{10, 20},
				Id:          AlertId,
			},
		},

		"change to crit, so notify": {
			want: true,
			alert: store.AlertState{
				Level: store.Crit,
			},
			previous: store.AlertState{
				Level: store.Ok,
			},
		},

		"change from crit to ok, so do nothing": {
			want: false,
			alert: store.AlertState{
				Level:    store.Ok,
				Time:     WithinDuration,
				Duration: SomeDuration,
			},
			previous: store.AlertState{
				Level: store.Crit,
				Time:  WithinDuration,
			},
		},

		"change to warn, so notify": {
			want: true,
			alert: store.AlertState{
				Level: store.Warn,
			},
			previous: store.AlertState{
				Level: store.Ok,
			},
		},

		"change from warn to ok, so do nothing": {
			want: false,
			alert: store.AlertState{
				Level: store.Ok,
			},
			previous: store.AlertState{
				Level: store.Warn,
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a, _, closeFunc := setupApp(t, name+"-testing", &store.Schema{})
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
		mockAlertStore, wantAlertStore *store.Schema
		alert                          source.InfluxAlert
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
							AlertLevel:  store.Crit,
							Timestamp:   AfterDuration,
							AlertType:   store.GreaterThan,
							AlertValues: []float32{OldValue},
							Duration:    "24h",
							Condition:   GreaterThanTen,
						},
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Duration: SomeDuration,
						Value:    OldValue,
						Level:    store.Crit,
					},
				},
			},
			alert: source.InfluxAlert{
				Id:              AlertId,
				Level:           "crit",
				Value:           NewValue,
				SensorTimestamp: AfterDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Time:     AfterDuration,
						Value:    NewValue,
						Duration: SomeDuration,
						Level:    store.Crit,
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
							AlertLevel:  store.Warn,
							Timestamp:   AfterDuration,
							AlertType:   store.LessThan,
							AlertValues: []float32{60000},
							Duration:    "24h",
							Condition:   "< 60000.00",
						},
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: "pressure",
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:  AlertId,
						Duration: "24h",
						RangeValues: []float32{
							SixtyThousandLitres.ToRaw(60000).F32(),
						},
						Name:       RegisteredAlertName,
						AlertType:  store.LessThan,
						AlertLevel: store.Warn,
					},
				},
				LinearCalibrations: []store.StoredLinearCalibration{
					{
						AlertId: AlertId,
						LinearCalibration: &store.LinearCalibration{
							RawMin: 4,
							RawMax: 10.6,
							CalMax: 97000,
						},
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Duration: SomeDuration,
						Value:    OldValue,
						Level:    store.Crit,
					},
				},
			},
			alert: source.InfluxAlert{
				Id:              AlertId,
				Level:           "warn",
				Value:           8.071,
				SensorTimestamp: AfterDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      "pressure",
			},
			wantAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: "pressure",
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:  AlertId,
						Duration: "24h",
						RangeValues: []float32{
							SixtyThousandLitres.ToRaw(60000).F32(),
						},
						Name:       RegisteredAlertName,
						AlertType:  store.LessThan,
						AlertLevel: store.Warn,
					},
				},
				LinearCalibrations: []store.StoredLinearCalibration{
					{
						AlertId: AlertId,
						LinearCalibration: &store.LinearCalibration{
							RawMin: 4,
							RawMax: 10.6,
							CalMax: 97000,
						},
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Time:     AfterDuration,
						Value:    8.071,
						Duration: SomeDuration,
						Level:    store.Warn,
					},
				},
			},
		},

		"no notification because duration has not elapsed": {
			wantStatus:      http.StatusNoContent,
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						AlertLevel:  store.Warn,
						RangeValues: []float32{OldValue},
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Duration: SomeDuration,
						Value:    OldValue,
						Level:    store.Crit,
					},
				},
			},
			alert: source.InfluxAlert{
				Id:              AlertId,
				Level:           "crit",
				Value:           NewValue,
				SensorTimestamp: WithinDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertRecipients:    []store.AlertRecipient{},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						AlertLevel:  store.Warn,
						RangeValues: []float32{OldValue},
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Value:    OldValue,
						Duration: SomeDuration,
						Level:    store.Crit,
					},
				},
			},
		},

		"no notification because going from crit to warn": {
			wantStatus:      http.StatusNoContent,
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						AlertLevel:  store.Warn,
						RangeValues: []float32{OldValue},
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Duration: SomeDuration,
						Value:    OldValue,
						Level:    store.Crit,
					},
				},
			},
			alert: source.InfluxAlert{
				Id:              AlertId,
				Level:           "warn",
				Value:           NewValue,
				SensorTimestamp: WithinDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertRecipients:    []store.AlertRecipient{},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						AlertLevel:  store.Warn,
						RangeValues: []float32{OldValue},
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Time:     time.Unix(0, WithinDuration.UnixNano()),
						Value:    NewValue,
						Duration: SomeDuration,
						Level:    store.Warn,
					},
				},
			},
		},

		"no notification because going from crit to ok": {
			wantStatus:      http.StatusNoContent,
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						AlertLevel:  store.Warn,
						RangeValues: []float32{OldValue},
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Duration: SomeDuration,
						Value:    OldValue,
						Level:    store.Crit,
					},
				},
			},
			alert: source.InfluxAlert{
				Id:              AlertId,
				Level:           "ok",
				Value:           NewValue,
				SensorTimestamp: WithinDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertRecipients:    []store.AlertRecipient{},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						AlertLevel:  store.Warn,
						RangeValues: []float32{OldValue},
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Time:     time.Unix(0, WithinDuration.UnixNano()),
						Value:    NewValue,
						Duration: SomeDuration,
						Level:    store.Ok,
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
							AlertLevel:  store.Crit,
							Timestamp:   WithinDuration,
							AlertType:   store.GreaterThan,
							AlertValues: []float32{OldValue},
							Duration:    "24h",
							Condition:   GreaterThanTen,
						},
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Value:    OldValue,
						Duration: SomeDuration,
						Level:    store.Ok,
					},
				},
			},
			alert: source.InfluxAlert{
				Id:              AlertId,
				Level:           "crit",
				Value:           NewValue,
				SensorTimestamp: WithinDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Value:    NewValue,
						Time:     WithinDuration,
						Duration: SomeDuration,
						Level:    store.Crit,
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
							AlertLevel:  store.Warn,
							Timestamp:   WithinDuration,
							AlertType:   store.GreaterThan,
							AlertValues: []float32{OldValue},
							Duration:    "24h",
							Condition:   GreaterThanTen,
						},
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Value:    OldValue,
						Duration: SomeDuration,
						Level:    store.Ok,
					},
				},
			},
			alert: source.InfluxAlert{
				Id:              AlertId,
				Level:           "warn",
				Value:           NewValue,
				SensorTimestamp: WithinDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Value:    NewValue,
						Time:     WithinDuration,
						Duration: SomeDuration,
						Level:    store.Warn,
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
							AlertLevel:  store.Crit,
							Timestamp:   WithinDuration,
							AlertType:   store.GreaterThan,
							AlertValues: []float32{OldValue},
							Duration:    "24h",
							Condition:   GreaterThanTen,
						},
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Time:     AlertReceived,
						Value:    OldValue,
						Duration: SomeDuration,
						Level:    store.Warn,
					},
				},
			},
			alert: source.InfluxAlert{
				Id:              AlertId,
				Level:           "crit",
				Value:           NewValue,
				SensorTimestamp: WithinDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Value:    NewValue,
						Time:     WithinDuration,
						Duration: SomeDuration,
						Level:    store.Crit,
					},
				},
			},
		},

		"no notification because alert on unregistered queryField": {
			wantStatus:      http.StatusNotFound,
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: &store.Schema{
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						AlertLevel:  store.Warn,
						RangeValues: []float32{OldValue},
					},
				},
				AlertStates: []store.AlertState{
					{
						Time:     AlertReceived,
						Duration: SomeDuration,
						Value:    OldValue,
						Id:       AlertId,
						Level:    store.Ok,
					},
				},
			},
			alert: source.InfluxAlert{
				Id:         AlertId,
				Level:      "crit",
				Value:      NewValue,
				DeviceId:   RegisteredDeviceId,
				QueryField: UnregisteredQueryField,
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						AlertLevel:  store.Warn,
						RangeValues: []float32{OldValue},
					},
				},
				AlertStates: []store.AlertState{
					{
						Time:     AlertReceived,
						Duration: SomeDuration,
						Id:       AlertId,
						Value:    OldValue,
						Level:    store.Ok,
					},
				},
			},
		},

		"no notification because alert on unregistered deviceId": {
			wantStatus:      http.StatusNotFound,
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: &store.Schema{
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						AlertLevel:  store.Warn,
						RangeValues: []float32{OldValue},
					},
				},
				AlertStates: []store.AlertState{
					{
						Time:     AlertReceived,
						Duration: SomeDuration,
						Value:    OldValue,
						Id:       AlertId,
						Level:    store.Ok,
					},
				},
			},
			alert: source.InfluxAlert{
				Id:         AlertId,
				Value:      NewValue,
				Level:      "crit",
				DeviceId:   UnregisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						AlertLevel:  store.Warn,
						Duration:    "24h",
						RangeValues: []float32{OldValue},
					},
				},
				AlertStates: []store.AlertState{
					{
						Time:     AlertReceived,
						Value:    NewValue,
						Duration: SomeDuration,
						Id:       AlertId,
						Level:    store.Ok,
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
							AlertLevel:  store.Crit,
							Timestamp:   AfterDuration,
							AlertType:   store.GreaterThan,
							AlertValues: []float32{OldValue},
							Duration:    "24h",
							Condition:   GreaterThanTen,
						},
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
					},
				},
				AlertStates: []store.AlertState{},
			},
			alert: source.InfluxAlert{
				Id:              AlertId,
				Level:           "crit",
				Value:           NewValue,
				SensorTimestamp: AfterDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Value:    NewValue,
						Time:     AfterDuration,
						Duration: SomeDuration,
						Level:    store.Crit,
					},
				},
			},
		},

		"no notification first alert": {
			wantStatus:      http.StatusNoContent,
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
					},
				},
				AlertStates: []store.AlertState{},
			},
			alert: source.InfluxAlert{
				Id:              AlertId,
				Level:           "ok",
				Value:           NewValue,
				SensorTimestamp: AfterDuration,
				DeviceId:        RegisteredDeviceId,
				QueryField:      RegisteredQueryField,
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
					},
				},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Value:    NewValue,
						Time:     AfterDuration,
						Duration: SomeDuration,
						Level:    store.Ok,
					},
				},
			},
		},

		"no notification because alert status ok": {
			wantStatus:      http.StatusNoContent,
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: &store.Schema{
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
						RangeValues: []float32{10},
					},
				},
				AlertStates: []store.AlertState{
					{
						Time:     AlertReceived,
						Duration: SomeDuration,
						Value:    11,
						Id:       AlertId,
						Level:    store.Crit,
					},
				},
			},
			alert: source.InfluxAlert{
				Id:         AlertId,
				Level:      "ok",
				Value:      9,
				DeviceId:   RegisteredDeviceId,
				QueryField: RegisteredQueryField,
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []store.AlertRecipient{},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
						RangeValues: []float32{10},
					},
				},
				AlertStates: []store.AlertState{
					{
						Duration: SomeDuration,
						Id:       AlertId,
						Value:    9,
						Level:    store.Ok,
					},
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a, api, closeFunc := setupApp(t, name+"-testing", tc.mockAlertStore)
			defer closeFunc()
			resp := api.Post("/alert/notify", tc.alert)
			if resp.Code != tc.wantStatus {
				t.Fatalf("wantStatus: %d, response status: %d", tc.wantStatus, resp.Code)
			}
			if tc.wantStatus < 300 {
				if diff := cmp.Diff(tc.wantUserInboxes, a.Notifier.GetUserInboxes()); diff != "" {
					t.Fatalf("user inboxes mismatch (-want +got):%s\n", diff)
				}
				if tc.wantAlertStore != nil {
					if diff := cmp.Diff(*tc.wantAlertStore, a.AlertStore.GetSnapshot(),
						cmpopts.EquateApproxTime(1*time.Second)); diff != "" {
						t.Fatalf("store mismatch (-want +got):%s\n", diff)
					}
				}
			}
		})
	}
}

func setupApp(t *testing.T,
	dbName string, mockAlertStore *store.Schema,
) (*alerts.App, humatest.TestAPI, func() error) {
	var err error
	a := &alerts.App{
		Telemetry: &telemetry.Telemetry{
			Meter:  metering.MockMeter(),
			Tracer: tracing.MockTracer(),
			Logger: logging.MockLogger(),
		},
	}
	config := alerts.Config()
	router := mux.NewRouter()
	humaApi := humamux.New(router, config)
	api := humatest.Wrap(t, humaApi)
	a.RegisterHumaOperations(api)

	var closers []func() error
	defer func() {
		if err != nil {
			for _, c := range closers {
				c()
			}
		}
	}()

	a.Templater, err = templates.NewMockTemplater()
	require.Nilf(t, err, "templater: %v", err)
	a.Notifier, err = notifier.NewGomail(store.Testing, notifier.GomailOpts{})
	require.Nilf(t, err, "notifier: %v", err)
	closers = append(closers, a.Notifier.Close)
	a.AlertStore, err = store.NewGorm(store.Testing, dbName)
	require.Nilf(t, err, "store: %v", err)
	closers = append(closers, a.AlertStore.Close)
	a.AlertSource, err = source.NewTestingInflux(ConfigPath)
	require.Nilf(t, err, "source: %v", err)
	closers = append(closers, a.AlertSource.Close)
	if mockAlertStore != nil {
		err = a.AlertStore.PrepareDb(*mockAlertStore)
		require.Nilf(t, err, "store prepare: %v", err)
		alertRules := SchemaToAlertRules(*mockAlertStore)
		err = a.AlertSource.PrepareDb(alertRules...)
		require.Nilf(t, err, "source prepare: %v", err)
	}
	return a, api, func() error {
		if err := a.Notifier.Close(); err != nil {
			t.Logf("closing notifier: %v", err)
		}
		if err := a.AlertStore.Close(); err != nil {
			t.Logf("closing store: %v", err)
		}
		if err := a.AlertSource.Close(); err != nil {
			t.Logf("closing source: %v", err)
		}
		return nil
	}
}

func TestCreateAlertRule(t *testing.T) {
	tests := map[string]struct {
		wantErr                        bool
		wantResponse                   store.CreatedRule
		wantStatus                     int
		wantAlertStore, mockAlertStore *store.Schema
		wantAlertSourceCount           int
		wantUserInboxes                notifier.Schema
		alertRule                      store.AlertRule
	}{
		"successfully create a less than alert rule": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			mockAlertStore: &store.Schema{
				DeviceAlerts:       []store.DeviceAlertRule{},
				AlertRecipients:    []store.AlertRecipient{},
				AlertStates:        []store.AlertState{},
				AlertRules:         []store.StoredAlertRule{},
				LinearCalibrations: []store.StoredLinearCalibration{},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Id:          AlertId,
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Level:       store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Crit,
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
							AlertLevel:  store.Crit,
							AlertType:   store.LessThan,
							AlertValues: []float32{5},
							Condition:   LessThanFive,
						},
					},
				},
			},
			alertRule: store.AlertRule{
				RangeValues:    []float32{5},
				Name:           RegisteredAlertName,
				Duration:       "24h",
				EmailAddresses: []string{UserEmail},
				DeviceId:       "device1",
				QueryField:     "temperature",
				AlertType:      store.LessThan,
				AlertLevel:     store.Crit,
			},
			wantResponse: store.CreatedRule{
				AlertId: AlertId,
			},
		},

		"deal with conflict on alert rule name": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 2,
			mockAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Id:          AlertId,
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Level:       store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Crit,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Id:          AlertId,
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Level:       store.Crit,
					},
					{
						Id:          "alert2",
						Duration:    SomeDuration,
						RangeValues: []float32{10},
						Level:       store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
					{AlertId: "alert2", Email: AnotherUserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
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
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Crit,
					},
					{
						AlertId:     "alert2",
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
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
							AlertLevel:  store.Crit,
							AlertType:   store.GreaterThan,
							AlertValues: []float32{10},
							Condition:   GreaterThanTen,
						},
					},
				},
			},
			alertRule: store.AlertRule{
				RangeValues:    []float32{10},
				Name:           RegisteredAlertName,
				Duration:       "24h",
				EmailAddresses: []string{AnotherUserEmail},
				DeviceId:       "device1",
				QueryField:     "temperature",
				AlertType:      store.GreaterThan,
				AlertLevel:     store.Crit,
			},
			wantResponse: store.CreatedRule{
				AlertId: "alert2",
			},
		},

		"successfully create an inactive sensor alert rule": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			mockAlertStore: &store.Schema{
				DeviceAlerts:    []store.DeviceAlertRule{},
				AlertRecipients: []store.AlertRecipient{},
				AlertStates:     []store.AlertState{},
				AlertRules:      []store.StoredAlertRule{},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Duration: SomeDuration,
						Level:    store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						Name:       RegisteredAlertName,
						AlertId:    AlertId,
						Duration:   "24h",
						AlertType:  store.InactiveSensor,
						AlertLevel: store.Crit,
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
							AlertLevel: store.Crit,
							AlertType:  store.InactiveSensor,
							Condition:  InactiveSensorCondition,
						},
					},
				},
			},
			alertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				Duration:       "24h",
				EmailAddresses: []string{UserEmail},
				DeviceId:       "device1",
				QueryField:     "temperature",
				AlertType:      store.InactiveSensor,
				AlertLevel:     store.Crit,
			},
			wantResponse: store.CreatedRule{
				AlertId: AlertId,
			},
		},

		"successfully create a greater than alert rule": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			mockAlertStore: &store.Schema{
				DeviceAlerts:    []store.DeviceAlertRule{},
				AlertRecipients: []store.AlertRecipient{},
				AlertStates:     []store.AlertState{},
				AlertRules:      []store.StoredAlertRule{},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Id:          AlertId,
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Level:       store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
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
							AlertLevel:  store.Crit,
							AlertType:   store.GreaterThan,
							AlertValues: []float32{5},
							Condition:   GreaterThanFive,
						},
					},
				},
			},
			alertRule: store.AlertRule{
				RangeValues:    []float32{5},
				Name:           RegisteredAlertName,
				Duration:       "24h",
				EmailAddresses: []string{UserEmail},
				DeviceId:       "device1",
				QueryField:     "temperature",
				AlertType:      store.GreaterThan,
				AlertLevel:     store.Crit,
			},
			wantResponse: store.CreatedRule{
				AlertId: AlertId,
			},
		},

		"successfully create a range alert rule": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			mockAlertStore: &store.Schema{
				DeviceAlerts:    []store.DeviceAlertRule{},
				AlertRecipients: []store.AlertRecipient{},
				AlertStates:     []store.AlertState{},
				AlertRules:      []store.StoredAlertRule{},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Id:          AlertId,
						Duration:    SomeDuration,
						RangeValues: []float32{10, 20},
						Level:       store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{10, 20},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   store.Range,
						AlertLevel:  store.Crit,
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
							AlertLevel:  store.Crit,
							AlertType:   store.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
					},
				},
			},
			alertRule: store.AlertRule{
				RangeValues:    []float32{10, 20},
				Name:           RegisteredAlertName,
				Duration:       "24h",
				EmailAddresses: []string{UserEmail},
				DeviceId:       "device1",
				QueryField:     "temperature",
				AlertType:      store.Range,
				AlertLevel:     store.Crit,
			},
			wantResponse: store.CreatedRule{
				AlertId: AlertId,
			},
		},

		"invalid range on less than alert": {
			wantStatus:      http.StatusUnprocessableEntity,
			wantUserInboxes: EmptyInboxes,
			mockAlertStore: &store.Schema{
				DeviceAlerts:       []store.DeviceAlertRule{},
				AlertRecipients:    []store.AlertRecipient{},
				AlertStates:        []store.AlertState{},
				AlertRules:         []store.StoredAlertRule{},
				LinearCalibrations: []store.StoredLinearCalibration{},
			},
			alertRule: store.AlertRule{
				Name:        RegisteredAlertName,
				RangeValues: []float32{5, 10},
				AlertType:   store.LessThan,
				AlertLevel:  store.Warn,
			},
		},

		"invalid range on range alert": {
			wantErr:         true,
			wantStatus:      http.StatusUnprocessableEntity,
			wantUserInboxes: EmptyInboxes,
			alertRule: store.AlertRule{
				Name:        RegisteredAlertName,
				RangeValues: []float32{5},
				AlertType:   store.Range,
				AlertLevel:  store.Warn,
			},
			mockAlertStore: &store.Schema{
				DeviceAlerts:       []store.DeviceAlertRule{},
				AlertRecipients:    []store.AlertRecipient{},
				AlertStates:        []store.AlertState{},
				AlertRules:         []store.StoredAlertRule{},
				LinearCalibrations: []store.StoredLinearCalibration{},
			},
		},

		"invalid alert rule": {
			wantErr:    true,
			wantStatus: http.StatusUnprocessableEntity,
			alertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				EmailAddresses: []string{},
				Duration:       "",
				AlertType:      store.Range,
				AlertLevel:     store.Warn,
			},
			mockAlertStore: &store.Schema{
				DeviceAlerts:       []store.DeviceAlertRule{},
				AlertRecipients:    []store.AlertRecipient{},
				AlertStates:        []store.AlertState{},
				AlertRules:         []store.StoredAlertRule{},
				LinearCalibrations: []store.StoredLinearCalibration{},
			},
		},

		"identical alert except name and emails, so return existing alert id": {
			wantErr:              false,
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			wantResponse: store.CreatedRule{
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
							AlertLevel:  store.Crit,
							AlertType:   store.LessThan,
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
							AlertLevel:  store.Crit,
							AlertType:   store.LessThan,
							AlertValues: []float32{5},
							Condition:   LessThanFive,
						},
					},
				},
			},
			alertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				RangeValues:    []float32{5},
				Duration:       "24h",
				EmailAddresses: []string{AnotherUserEmail, "user3@alert.com"},
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      store.LessThan,
				AlertLevel:     store.Crit,
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{5},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Crit,
					},
				},
				AlertStates: []store.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Id:          AlertId,
						Level:       store.Crit,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
					{AlertId: AlertId, Email: AnotherUserEmail},
					{AlertId: AlertId, Email: "user3@alert.com"},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Crit,
					},
				},
				AlertStates: []store.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Id:          AlertId,
						Level:       store.Crit,
					},
				},
			},
		},

		"identical alert including user email, so conflict": {
			wantErr:    true,
			wantStatus: http.StatusConflict,
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{5},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Crit,
					},
				},
			},
			wantUserInboxes: EmptyInboxes,
			alertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				RangeValues:    []float32{5},
				Duration:       "24h",
				EmailAddresses: []string{UserEmail},
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      store.LessThan,
				AlertLevel:     store.Crit,
			},
		},

		"similar alert rule exists, but new one on different queryField": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			wantResponse: store.CreatedRule{
				AlertId: "alert2",
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Id:          AlertId,
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Level:       store.Crit,
					},
					{
						Id:          "alert2",
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Level:       store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
					{AlertId: "alert2", Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
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
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
						Name:        "Similar Alert",
					},
					{
						AlertId:     "alert2",
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
						Name:        "Similar Alert 2",
					},
				},
			},
			wantAlertSourceCount: 2,
			mockAlertStore: &store.Schema{
				AlertStates: []store.AlertState{
					{
						Id:          AlertId,
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Level:       store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{5},
						Name:        "Similar Alert",
						Duration:    "24h",
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
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
							AlertLevel:  store.Crit,
							AlertType:   store.GreaterThan,
							AlertValues: []float32{5},
							Condition:   GreaterThanFive,
						},
					},
				},
			},
			alertRule: store.AlertRule{
				Name:           "Similar Alert 2",
				RangeValues:    []float32{5},
				EmailAddresses: []string{UserEmail},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     "humidity",
				AlertType:      store.GreaterThan,
				AlertLevel:     store.Crit,
			},
		},

		"allow multiple alerts on deviceId and queryField combination": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			wantResponse: store.CreatedRule{
				AlertId: "alert2",
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Id:          "alert2",
						Duration:    SomeDuration,
						RangeValues: []float32{10, 20},
						Level:       store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
					{AlertId: "alert2", Email: UserEmail},
					{AlertId: "alert2", Email: "user2@alert.com"},
				},
				DeviceAlerts: []store.DeviceAlertRule{
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
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Crit,
						Name:        RegisteredAlertName,
					},
					{
						AlertId:     "alert2",
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertType:   store.Range,
						AlertLevel:  store.Crit,
						Name:        "Some Other Name",
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Crit,
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
							AlertLevel:  store.Crit,
							AlertType:   store.Range,
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
							AlertLevel:  store.Crit,
							AlertType:   store.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
					},
				},
			},
			alertRule: store.AlertRule{
				Name:           "Some Other Name",
				EmailAddresses: []string{UserEmail, "user2@alert.com"},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      store.Range,
				AlertLevel:     store.Crit,
			},
		},

		"allow multiple alerts of the same type, on deviceId and queryField combination": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			wantResponse: store.CreatedRule{
				AlertId: "alert2",
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Id:          "alert2",
						Duration:    SomeDuration,
						RangeValues: []float32{10},
						Level:       store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
					{AlertId: "alert2", Email: UserEmail},
					{AlertId: "alert2", Email: "user2@alert.com"},
				},
				DeviceAlerts: []store.DeviceAlertRule{
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
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
					},
					{
						AlertId:     "alert2",
						Name:        "Some alert name",
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Crit,
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{5},
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
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
							AlertLevel:  store.Crit,
							AlertType:   store.LessThan,
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
							AlertLevel:  store.Crit,
							AlertType:   store.LessThan,
							AlertValues: []float32{10},
							Condition:   LessThanTen,
						},
					},
				},
			},
			alertRule: store.AlertRule{
				Name:           "Some alert name",
				EmailAddresses: []string{UserEmail, "user2@alert.com"},
				RangeValues:    []float32{10},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      store.LessThan,
				AlertLevel:     store.Crit,
			},
		},

		"successfully create a warning level alert rule": {
			wantErr:              false,
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			mockAlertStore: &store.Schema{
				DeviceAlerts:       []store.DeviceAlertRule{},
				AlertRecipients:    []store.AlertRecipient{},
				AlertStates:        []store.AlertState{},
				AlertRules:         []store.StoredAlertRule{},
				LinearCalibrations: []store.StoredLinearCalibration{},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Id:          AlertId,
						Duration:    SomeDuration,
						RangeValues: []float32{5},
						Level:       store.Warn,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{5},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
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
							AlertLevel:  store.Warn,
							AlertType:   store.LessThan,
							AlertValues: []float32{5},
							Condition:   LessThanFive,
						},
					},
				},
			},
			alertRule: store.AlertRule{
				RangeValues:    []float32{5},
				Name:           RegisteredAlertName,
				Duration:       "24h",
				EmailAddresses: []string{UserEmail},
				DeviceId:       "device1",
				QueryField:     "temperature",
				AlertType:      store.LessThan,
				AlertLevel:     store.Warn,
			},
			wantResponse: store.CreatedRule{
				AlertId: AlertId,
			},
		},

		"create a warning level alert rule with a linear calibration": {
			wantErr:              false,
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			mockAlertStore: &store.Schema{
				DeviceAlerts:       []store.DeviceAlertRule{},
				AlertRecipients:    []store.AlertRecipient{},
				AlertStates:        []store.AlertState{},
				AlertRules:         []store.StoredAlertRule{},
				LinearCalibrations: []store.StoredLinearCalibration{},
			},
			wantAlertStore: &store.Schema{
				AlertStates: []store.AlertState{
					{
						Id:       AlertId,
						Duration: SomeDuration,
						// NOTE: the raw value according to the calibration gets stored
						RangeValues: []float32{8.082474},
						Level:       store.Warn,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: "pressure",
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId: AlertId,
						// NOTE: the raw value according to the calibration gets stored
						RangeValues: []float32{8.082474},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
					},
				},
				LinearCalibrations: []store.StoredLinearCalibration{
					{
						AlertId: AlertId,
						LinearCalibration: &store.LinearCalibration{
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
							AlertLevel:  store.Warn,
							AlertType:   store.LessThan,
							AlertValues: []float32{60000},
							Condition:   "< 60000.00",
							LinearCalibration: &store.LinearCalibration{
								RawMin: 4,
								RawMax: 10.6,
								CalMax: 97000,
							},
						},
					},
				},
			},
			alertRule: store.AlertRule{
				RangeValues:    []float32{60000},
				Name:           RegisteredAlertName,
				Duration:       "24h",
				EmailAddresses: []string{UserEmail},
				DeviceId:       "device1",
				QueryField:     "pressure",
				AlertType:      store.LessThan,
				AlertLevel:     store.Warn,
				LinearCalibration: &store.LinearCalibration{
					RawMin: 4,
					RawMax: 10.6,
					CalMax: 97000,
				},
			},
			wantResponse: store.CreatedRule{
				AlertId: AlertId,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a, api, closeFunc := setupApp(t, name+"-testing", tc.mockAlertStore)
			defer closeFunc()
			resp := api.Post("/alert", tc.alertRule)
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatusCode: %d, response code: %d", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				var got store.CreatedRule
				err := json.Unmarshal(resp.Body.Bytes(), &got)
				require.Nil(t, err)
				gotAlertSourceCount := a.AlertSource.GetSnapshot()
				if tc.wantAlertSourceCount != gotAlertSourceCount {
					t.Fatalf(
						"wantAlertSourceCount: %d, got alertsCount: %d",
						tc.wantAlertSourceCount, gotAlertSourceCount)
				}
				if diff := cmp.Diff(tc.wantUserInboxes, a.Notifier.GetUserInboxes()); diff != "" {
					t.Fatalf("user inboxes mismatch (-want +got):%s\n", diff)
				}
				if tc.wantAlertStore != nil {
					diff := cmp.Diff(*tc.wantAlertStore, a.AlertStore.GetSnapshot())
					if diff != "" {
						t.Fatalf("store mismatch (-want +got):%s\n", diff)
					}
				}
				if diff := cmp.Diff(tc.wantResponse, got); diff != "" {
					t.Fatalf("response mismatch (-want +got):%s\n", diff)
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
			got, err = store.ParseDuration(tc.input)
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
		updatedAlertRule               store.AlertRule
		mockAlertStore, wantAlertStore *store.Schema
		wantAlertSourceCount           int
	}{
		"update the range values of an alert": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      store.Range,
				AlertLevel:     store.Crit,
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
							AlertLevel:  store.Crit,
							AlertType:   store.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{43, 90},
						Duration:    "24h",
						AlertType:   store.Range,
						AlertLevel:  store.Crit,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10, 20},
						Id:          AlertId,
						Level:       store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertType:   store.Range,
						AlertLevel:  store.Crit,
					},
				},
			},
		},

		"incoming alert is identical to what already exists, so send OK": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{12, 24},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      store.Range,
				AlertLevel:     store.Crit,
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{12, 24},
						Duration:    "24h",
						AlertType:   store.Range,
						AlertLevel:  store.Crit,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates:        []store.AlertState{},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{12, 24},
						Duration:    "24h",
						AlertType:   store.Range,
						AlertLevel:  store.Crit,
					},
				},
			},
		},

		"update alert to become identical to an existing alert, still keep both alerts": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 2,
			alertId:              "alert2",
			updatedAlertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				EmailAddresses: []string{"user2@alerts.com"},
				RangeValues:    []float32{10},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      store.LessThan,
				AlertLevel:     store.Crit,
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
							AlertLevel:  store.Crit,
							AlertType:   store.LessThan,
							AlertValues: []float32{10},
							Condition:   LessThanTen,
						},
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
					{AlertId: "alert2", Email: "user2@alerts.com"},
				},
				DeviceAlerts: []store.DeviceAlertRule{
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
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Crit,
					},
					{
						AlertId:     "alert2",
						Name:        "Some other alert name",
						RangeValues: []float32{43, 90},
						Duration:    "24h",
						AlertType:   store.Range,
						AlertLevel:  store.Crit,
					},
				},
				AlertStates: []store.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10},
						Id:          AlertId,
						Level:       store.Crit,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10},
						Id:          AlertId,
						Level:       store.Crit,
					},
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10},
						Id:          "alert2",
						Level:       store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
					{AlertId: "alert2", Email: "user2@alerts.com"},
				},
				DeviceAlerts: []store.DeviceAlertRule{
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
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Crit,
					},
					{
						AlertId:     "alert2",
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Crit,
					},
				},
			},
		},

		"update the last state of an alert": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      store.Range,
				AlertLevel:     store.Crit,
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
							AlertLevel:  store.Crit,
							AlertType:   store.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{43, 90},
						Duration:    "24h",
						AlertType:   store.Range,
						AlertLevel:  store.Crit,
					},
				},
				AlertStates: []store.AlertState{
					{
						Time:        AlertReceived,
						Duration:    SomeDuration,
						RangeValues: []float32{43, 90},
						Id:          AlertId,
						Level:       store.Ok,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertType:   store.Range,
						AlertLevel:  store.Crit,
					},
				},
				AlertStates: []store.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10, 20},
						Id:          AlertId,
						Level:       store.Crit,
					},
				},
			},
		},

		"update alert type from less than to greater than": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      store.GreaterThan,
				AlertLevel:     store.Crit,
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
							AlertLevel:  store.Crit,
							AlertType:   store.GreaterThan,
							AlertValues: []float32{10},
							Condition:   GreaterThanTen,
						},
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Crit,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10},
						Id:          AlertId,
						Level:       store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
					},
				},
			},
		},

		"update alert type from greater than to less than": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      store.LessThan,
				AlertLevel:     store.Crit,
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
							AlertLevel:  store.Crit,
							AlertType:   store.LessThan,
							AlertValues: []float32{10},
							Condition:   LessThanTen,
						},
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10},
						Id:          AlertId,
						Level:       store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Crit,
					},
				},
			},
		},

		"update alert to include a linear calibration": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: store.AlertRule{
				Name:              RegisteredAlertName,
				AlertId:           AlertId,
				EmailAddresses:    []string{UserEmail},
				RangeValues:       []float32{60000},
				Duration:          "24h",
				DeviceId:          RegisteredDeviceId,
				QueryField:        RegisteredQueryField,
				AlertType:         store.GreaterThan,
				AlertLevel:        store.Crit,
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
							AlertLevel:        store.Crit,
							AlertType:         store.GreaterThan,
							AlertValues:       []float32{60000},
							Condition:         "60000.00 <",
							LinearCalibration: &SixtyThousandLitres,
						},
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Crit,
					},
				},
			},
			wantAlertStore: &store.Schema{
				AlertStates: []store.AlertState{
					{
						Duration: SomeDuration,
						RangeValues: []float32{
							SixtyThousandLitres.ToRaw(60000).F32(),
						},
						Id:    AlertId,
						Level: store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId: AlertId,
						Name:    RegisteredAlertName,
						RangeValues: []float32{
							SixtyThousandLitres.ToRaw(60000).F32(),
						},
						Duration:   "24h",
						AlertType:  store.GreaterThan,
						AlertLevel: store.Crit,
					},
				},
				LinearCalibrations: []store.StoredLinearCalibration{
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
			updatedAlertRule: store.AlertRule{
				Name:              RegisteredAlertName,
				AlertId:           AlertId,
				EmailAddresses:    []string{UserEmail},
				RangeValues:       []float32{60000},
				Duration:          "24h",
				DeviceId:          RegisteredDeviceId,
				QueryField:        RegisteredQueryField,
				AlertType:         store.GreaterThan,
				AlertLevel:        store.Crit,
				LinearCalibration: &SixtyThousandLitres,
			},
			wantUserInboxes: notifier.Schema{
				Notifications: map[string][]notifier.AlertNotification{},
				DeletedAlerts: map[string][]notifier.AlertDeleted{},
				CreatedAlerts: map[string][]notifier.AlertCreated{},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId: AlertId,
						Name:    RegisteredAlertName,
						RangeValues: []float32{
							SixtyThousandLitres.ToRaw(60000).F32(),
						},
						Duration:   "24h",
						AlertType:  store.GreaterThan,
						AlertLevel: store.Crit,
					},
				},
				LinearCalibrations: []store.StoredLinearCalibration{
					{
						AlertId:           AlertId,
						LinearCalibration: &SixtyThousandLitres,
					},
				},
			},
			wantAlertStore: &store.Schema{
				AlertStates: []store.AlertState{},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId: AlertId,
						Name:    RegisteredAlertName,
						RangeValues: []float32{
							SixtyThousandLitres.ToRaw(60000).F32(),
						},
						Duration:   "24h",
						AlertType:  store.GreaterThan,
						AlertLevel: store.Crit,
					},
				},
				LinearCalibrations: []store.StoredLinearCalibration{
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
			updatedAlertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      store.Range,
				AlertLevel:     store.Crit,
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
							AlertLevel:  store.Crit,
							AlertType:   store.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Crit,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10, 20},
						Id:          AlertId,
						Level:       store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertType:   store.Range,
						AlertLevel:  store.Crit,
					},
				},
			},
		},

		"can't update the queryField of an alert. Constitutes a new alert entirely": {
			wantStatus:           http.StatusNotFound,
			wantErr:              true,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     UnregisteredQueryField,
				AlertLevel:     store.Crit,
				AlertType:      store.Range,
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertLevel:  store.Crit,
						AlertType:   store.LessThan,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertLevel:  store.Crit,
						AlertType:   store.LessThan,
					},
				},
			},
		},

		"can't update the deviceId of an alert. Constitutes a new alert entirely": {
			wantStatus:           http.StatusNotFound,
			wantErr:              true,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       UnregisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     store.Crit,
				AlertType:      store.Range,
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertLevel:  store.Crit,
						AlertType:   store.LessThan,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertLevel:  store.Crit,
						AlertType:   store.LessThan,
					},
				},
			},
		},

		"can't update from threshold to inactive sensor alert type": {
			wantStatus:           http.StatusBadRequest,
			wantErr:              true,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     store.Crit,
				AlertType:      store.InactiveSensor,
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertLevel:  store.Crit,
						AlertType:   store.LessThan,
					},
				},
			},
			wantAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertLevel:  store.Crit,
						AlertType:   store.LessThan,
					},
				},
			},
		},

		"update inactive sensor alert duration": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				Duration:       "48h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     store.Crit,
				AlertType:      store.InactiveSensor,
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
							AlertLevel: store.Crit,
							AlertType:  store.InactiveSensor,
							Condition:  InactiveSensorCondition,
						},
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:    AlertId,
						Name:       RegisteredAlertName,
						Duration:   "24h",
						AlertLevel: store.Crit,
						AlertType:  store.InactiveSensor,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Duration: 48 * time.Hour,
						Id:       AlertId,
						Level:    store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:    AlertId,
						Name:       RegisteredAlertName,
						Duration:   "48h",
						AlertLevel: store.Crit,
						AlertType:  store.InactiveSensor,
					},
				},
			},
		},

		"add more emails to the recipient list of an alert rule": {
			wantStatus:           http.StatusOK,
			alertId:              AlertId,
			wantAlertSourceCount: 1,
			updatedAlertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail, "user2@alert.com"},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     store.Crit,
				AlertType:      store.Range,
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
							AlertLevel:  store.Crit,
							AlertType:   store.Range,
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
							AlertLevel:  store.Crit,
							AlertType:   store.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertLevel:  store.Crit,
						AlertType:   store.Range,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10, 20},
						Id:          AlertId,
						Level:       store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
					{AlertId: AlertId, Email: "user2@alert.com"},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertLevel:  store.Crit,
						AlertType:   store.Range,
					},
				},
			},
		},

		"ignore duplicate emails in recipient list": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail, UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     store.Crit,
				AlertType:      store.Range,
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
							AlertLevel:  store.Crit,
							AlertType:   store.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertLevel:  store.Warn,
						AlertType:   store.LessThan,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10, 20},
						Id:          AlertId,
						Level:       store.Crit,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10, 20},
						Duration:    "24h",
						AlertLevel:  store.Crit,
						AlertType:   store.Range,
					},
				},
			},
		},

		"error when trying to update non existent alert rule": {
			wantStatus: http.StatusNotFound,
			wantErr:    true,
			alertId:    AlertId,
			mockAlertStore: &store.Schema{
				DeviceAlerts:       []store.DeviceAlertRule{},
				AlertRecipients:    []store.AlertRecipient{},
				AlertStates:        []store.AlertState{},
				AlertRules:         []store.StoredAlertRule{},
				LinearCalibrations: []store.StoredLinearCalibration{},
			},
			updatedAlertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     store.Crit,
				AlertType:      store.Range,
			},
		},

		"invalid: 1 range value on a range type alert check": {
			wantStatus: http.StatusBadRequest,
			wantErr:    true,
			alertId:    AlertId,
			mockAlertStore: &store.Schema{
				DeviceAlerts:       []store.DeviceAlertRule{},
				AlertRecipients:    []store.AlertRecipient{},
				AlertStates:        []store.AlertState{},
				AlertRules:         []store.StoredAlertRule{},
				LinearCalibrations: []store.StoredLinearCalibration{},
			},
			updatedAlertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     store.Crit,
				AlertType:      store.Range,
			},
		},

		"invalid: 2 range values on a less than type alert check": {
			wantStatus: http.StatusBadRequest,
			wantErr:    true,
			alertId:    AlertId,
			mockAlertStore: &store.Schema{
				DeviceAlerts:       []store.DeviceAlertRule{},
				AlertRecipients:    []store.AlertRecipient{},
				AlertStates:        []store.AlertState{},
				AlertRules:         []store.StoredAlertRule{},
				LinearCalibrations: []store.StoredLinearCalibration{},
			},
			updatedAlertRule: store.AlertRule{
				EmailAddresses: []string{UserEmail},
				Name:           RegisteredAlertName,
				RangeValues:    []float32{50, 90},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     store.Crit,
				AlertType:      store.LessThan,
			},
		},

		"update alert level from crit to warn": {
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			alertId:              AlertId,
			updatedAlertRule: store.AlertRule{
				Name:           RegisteredAlertName,
				AlertId:        AlertId,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      store.GreaterThan,
				AlertLevel:     store.Warn,
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
							AlertLevel:  store.Warn,
							AlertType:   store.GreaterThan,
							AlertValues: []float32{10},
							Condition:   GreaterThanTen,
						},
					},
				},
			},
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Crit,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertStates: []store.AlertState{
					{
						Duration:    SomeDuration,
						RangeValues: []float32{10},
						Id:          AlertId,
						Level:       store.Warn,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						RangeValues: []float32{10},
						Duration:    "24h",
						AlertType:   store.GreaterThan,
						AlertLevel:  store.Warn,
					},
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a, api, closeFunc := setupApp(t, name+"-testing", tc.mockAlertStore)
			defer closeFunc()
			resp := api.Put(fmt.Sprintf("/alert/%s", tc.alertId), tc.updatedAlertRule)
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatusCode: %d, response code: %d", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				var got store.AlertRule
				err := json.Unmarshal(resp.Body.Bytes(), &got)
				require.Nil(t, err)
				tc.updatedAlertRule.AlertId = tc.alertId
				if diff := cmp.Diff(tc.updatedAlertRule, got); diff != "" {
					t.Fatalf("response alertrule mismatch (-want +got):%s\n", diff)
				}
				if tc.wantAlertStore != nil {
					diff := cmp.Diff(*tc.wantAlertStore, a.AlertStore.GetSnapshot())
					if diff != "" {
						t.Fatalf("store mismatch (-want +got):%s\n", diff)
					}
				}
				alertSourceCount := a.AlertSource.GetSnapshot()
				if alertSourceCount != tc.wantAlertSourceCount {
					t.Fatalf("alertSourceCount: %d, wantAlertSourceCount: %d",
						alertSourceCount, tc.wantAlertSourceCount)
				}
				if diff := cmp.Diff(tc.wantUserInboxes, a.Notifier.GetUserInboxes()); diff != "" {
					t.Fatalf("user inboxes mismatch (-want +got):%s\n", diff)
				}
			}
		})
	}
}

func TestGetAlertRule(t *testing.T) {
	tests := map[string]struct {
		wantErr        bool
		want           store.AlertRule
		wantStatus     int
		alertId        string
		mockAlertStore *store.Schema
	}{
		"successfully get a stored threshold alert": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			alertId:    AlertId,
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						Duration:    "24h",
						RangeValues: []float32{10, 20},
						AlertType:   store.Range,
						AlertLevel:  store.Warn,
					},
				},
			},
			want: store.AlertRule{
				AlertId:        AlertId,
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      store.Range,
				AlertLevel:     store.Warn,
			},
		},

		"successfully get a stored inactive sensor alert": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			alertId:    AlertId,
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:    AlertId,
						Name:       RegisteredAlertName,
						Duration:   "24h",
						AlertType:  store.InactiveSensor,
						AlertLevel: store.Crit,
					},
				},
			},
			want: store.AlertRule{
				AlertId:        AlertId,
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      store.InactiveSensor,
				AlertLevel:     store.Crit,
			},
		},

		"alert doesn't exist": {
			wantErr:    true,
			want:       store.AlertRule{},
			wantStatus: 404,
			alertId:    "alert2",
		},

		"successfully get a stored threshold alert with a linear calibration": {
			wantErr:    false,
			wantStatus: http.StatusOK,
			alertId:    AlertId,
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						Duration:    "24h",
						RangeValues: []float32{10, 20},
						AlertType:   store.Range,
						AlertLevel:  store.Warn,
					},
				},
				LinearCalibrations: []store.StoredLinearCalibration{
					{
						AlertId: AlertId,
						LinearCalibration: &store.LinearCalibration{
							RawMin: 0,
							RawMax: 1,
							CalMin: 0,
							CalMax: 1,
						},
					},
				},
			},
			want: store.AlertRule{
				AlertId:        AlertId,
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertType:      store.Range,
				AlertLevel:     store.Warn,
				LinearCalibration: &store.LinearCalibration{
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
			_, api, closeFunc := setupApp(t, name+"-testing", tc.mockAlertStore)
			defer closeFunc()
			resp := api.Get(fmt.Sprintf("/alert/%s", tc.alertId))
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatusCode: %d, response code: %d", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				var got store.AlertRule
				err := json.Unmarshal(resp.Body.Bytes(), &got)
				require.Nil(t, err)
				if diff := cmp.Diff(tc.want, got); diff != "" {
					t.Fatalf("response mismatch (-want +got):%s\n", diff)
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
		wantAlertRule                  store.AlertRule
		wantUserInboxes                notifier.Schema
		mockAlertStore, wantAlertStore *store.Schema
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
							AlertLevel:  store.Crit,
							AlertType:   store.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
						},
					},
				},
			},
			wantAlertSourceCount: 0,
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						Name:        RegisteredAlertName,
						AlertId:     AlertId,
						Duration:    "24h",
						RangeValues: []float32{10, 20},
						AlertLevel:  store.Crit,
						AlertType:   store.Range,
					},
				},
				AlertStates: []store.AlertState{
					{
						Id: AlertId,
					},
				},
			},
			wantAlertRule: store.AlertRule{
				AlertId:        AlertId,
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10, 20},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     store.Crit,
				AlertType:      store.Range,
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
							AlertLevel:  store.Crit,
							AlertType:   store.Range,
							AlertValues: []float32{10, 20},
							Condition:   RangeCondition,
							LinearCalibration: &store.LinearCalibration{
								RawMin: 0,
								RawMax: 1,
								CalMin: 0,
								CalMax: 2,
							},
						},
					},
				},
			},
			wantAlertSourceCount: 0,
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						Name:        RegisteredAlertName,
						AlertId:     AlertId,
						Duration:    "24h",
						RangeValues: []float32{5, 10},
						AlertLevel:  store.Crit,
						AlertType:   store.Range,
					},
				},
				LinearCalibrations: []store.StoredLinearCalibration{
					{
						AlertId: AlertId,
						LinearCalibration: &store.LinearCalibration{
							RawMax: 1,
							CalMax: 2,
						},
					},
				},
				AlertStates: []store.AlertState{
					{
						Id: AlertId,
					},
				},
			},
			wantAlertRule: store.AlertRule{
				AlertId:        AlertId,
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{5, 10},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     store.Crit,
				AlertType:      store.Range,
				LinearCalibration: &store.LinearCalibration{
					RawMax: 1,
					CalMax: 2,
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
							AlertLevel:  store.Warn,
							AlertType:   store.LessThan,
							AlertValues: []float32{10},
							Condition:   LessThanTen,
						},
					},
				},
			},
			wantAlertSourceCount: 1,
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
					{AlertId: "alert2", Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
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
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						Duration:    "24h",
						RangeValues: []float32{10, 20},
						AlertLevel:  store.Crit,
						AlertType:   store.Range,
					},
					{
						AlertId:     "alert2",
						Name:        "Some other alert name",
						Duration:    "48h",
						RangeValues: []float32{10},
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
					},
				},
				AlertStates: []store.AlertState{
					{
						Id: AlertId,
					},
					{
						Id: "alert2",
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						Name:        RegisteredAlertName,
						Duration:    "24h",
						RangeValues: []float32{10, 20},
						AlertLevel:  store.Crit,
						AlertType:   store.Range,
					},
				},
				AlertStates: []store.AlertState{
					{
						Id: AlertId,
					},
				},
			},
			wantAlertRule: store.AlertRule{
				AlertId:        "alert2",
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				RangeValues:    []float32{10},
				Duration:       "48h",
				DeviceId:       "device2",
				QueryField:     "humidity",
				AlertType:      store.LessThan,
				AlertLevel:     store.Warn,
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
							AlertLevel: store.Crit,
							AlertType:  store.InactiveSensor,
							Condition:  InactiveSensorCondition,
						},
					},
				},
			},
			wantAlertSourceCount: 0,
			mockAlertStore: &store.Schema{
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:    AlertId,
						Name:       RegisteredAlertName,
						Duration:   "24h",
						AlertLevel: store.Crit,
						AlertType:  store.InactiveSensor,
					},
				},
				AlertStates: []store.AlertState{
					{
						Id: AlertId,
					},
				},
			},
			wantAlertRule: store.AlertRule{
				AlertId:        AlertId,
				Name:           RegisteredAlertName,
				EmailAddresses: []string{UserEmail},
				Duration:       "24h",
				DeviceId:       RegisteredDeviceId,
				QueryField:     RegisteredQueryField,
				AlertLevel:     store.Crit,
				AlertType:      store.InactiveSensor,
			},
		},

		"try to delete non existent alert": {
			wantErr:         true,
			wantStatus:      http.StatusNotFound,
			wantUserInboxes: EmptyInboxes,
			alertId:         "alert2",
			mockAlertStore: &store.Schema{
				DeviceAlerts:       []store.DeviceAlertRule{},
				AlertRecipients:    []store.AlertRecipient{},
				AlertStates:        []store.AlertState{},
				AlertRules:         []store.StoredAlertRule{},
				LinearCalibrations: []store.StoredLinearCalibration{},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a, api, closeFunc := setupApp(t, name+"-testing", tc.mockAlertStore)
			defer closeFunc()
			resp := api.Delete(fmt.Sprintf("/alert/%s", tc.alertId))
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatus: %d, response Code: %v", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				var got store.AlertRule
				err := json.Unmarshal(resp.Body.Bytes(), &got)
				require.Nil(t, err)
				if diff := cmp.Diff(tc.wantAlertRule, got); err != nil {
					t.Fatalf("alertRule mismatch (-want +got):%s\n", diff)
				}
				if tc.wantAlertStore != nil {
					diff := cmp.Diff(*tc.wantAlertStore, a.AlertStore.GetSnapshot())
					if diff != "" {
						t.Fatalf("alertStore mismatch (-want +got):%s\n", diff)
					}
				}
				alertsCount := a.AlertSource.GetSnapshot()
				if tc.wantAlertSourceCount != alertsCount {
					t.Fatalf("wantAlertSourceCount: %d, got alertsCount: %d",
						tc.wantAlertSourceCount, alertsCount)
				}
				if diff := cmp.Diff(tc.wantUserInboxes, a.Notifier.GetUserInboxes()); diff != "" {
					t.Fatalf("user inboxes mismatch (-want +got):%s\n", diff)
				}
			}
		})
	}
}

func TestRemoveUsersFromAlertRule(t *testing.T) {
	tests := map[string]struct {
		wantErr                        bool
		mockAlertStore, wantAlertStore *store.Schema
		wantStatus                     int
		wantUserInboxes                notifier.Schema
		emailsToRemove                 store.Emails
		alertId                        string
		wantAlertSourceCount           int
	}{
		"remove 1 email from alert rule": {
			mockAlertStore: &store.Schema{
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{
						AlertId: AlertId,
						Email:   UserEmail,
					},
					{
						AlertId: AlertId,
						Email:   AnotherUserEmail,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{
						AlertId: AlertId,
						Email:   UserEmail,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
					},
				},
				AlertStates: []store.AlertState{},
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
							AlertLevel:  store.Warn,
							AlertType:   store.LessThan,
							AlertValues: []float32{OldValue},
							Condition:   LessThanTen,
						},
					},
				},
			},
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			emailsToRemove:       store.Emails{Addresses: []string{AnotherUserEmail}},
			alertId:              AlertId,
		},

		"try to remove email that isnt attached to the alert": {
			mockAlertStore: &store.Schema{
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
					},
				},
				AlertStates: []store.AlertState{},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
					},
				},
				AlertStates: []store.AlertState{},
			},
			wantUserInboxes:      EmptyInboxes,
			wantStatus:           http.StatusOK,
			wantAlertSourceCount: 1,
			emailsToRemove:       store.Emails{Addresses: []string{AnotherUserEmail}},
			alertId:              AlertId,
		},

		"remove last email from alert, so delete alert": {
			mockAlertStore: &store.Schema{
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
					},
				},
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
							AlertLevel:  store.Warn,
							AlertType:   store.LessThan,
							AlertValues: []float32{OldValue},
							Condition:   LessThanTen,
						},
					},
				},
			},
			wantStatus:     http.StatusNoContent,
			emailsToRemove: store.Emails{Addresses: []string{UserEmail}},
			alertId:        AlertId,
		},

		"remove last email from alert among other unknown emails, so delete alert": {
			mockAlertStore: &store.Schema{
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{AlertId: AlertId, Email: UserEmail},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
					},
				},
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
							AlertLevel:  store.Warn,
							AlertType:   store.LessThan,
							AlertValues: []float32{OldValue},
							Condition:   LessThanTen,
						},
					},
				},
			},
			wantStatus:     http.StatusNoContent,
			emailsToRemove: store.Emails{Addresses: []string{UserEmail, AnotherUserEmail}},
			alertId:        AlertId,
		},

		"alert rule not found": {
			wantErr: true,
			mockAlertStore: &store.Schema{
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{
						AlertId: AlertId,
						Email:   UserEmail,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
					},
				},
			},
			wantStatus:           http.StatusNotFound,
			emailsToRemove:       store.Emails{Addresses: []string{AnotherUserEmail}},
			alertId:              UnregisteredAlertId,
			wantAlertSourceCount: 1,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a, api, closeFunc := setupApp(t, name+"-testing", tc.mockAlertStore)
			defer closeFunc()
			resp := api.Patch(fmt.Sprintf("/alert/%s/emailaddresses", tc.alertId),
				tc.emailsToRemove)
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatus: %d, response Code: %v", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				if tc.wantAlertStore != nil {
					diff := cmp.Diff(*tc.wantAlertStore, a.AlertStore.GetSnapshot())
					if diff != "" {
						t.Fatalf("alertStore mismatch (-want +got):%s\n", diff)
					}
				}
				if tc.wantStatus != http.StatusNoContent {
					var mu store.MutateAlertUsersResponse
					body := resp.Body.Bytes()
					err := json.Unmarshal(body, &mu)
					require.Nil(t, err)
					if diff := cmp.Diff(tc.emailsToRemove.Addresses, mu.EmailAddresses); diff != "" {
						t.Fatalf("emails mismatch (-want +got):%s\n", diff)
					}
				}
				alertsCount := a.AlertSource.GetSnapshot()
				if tc.wantAlertSourceCount != alertsCount {
					t.Fatalf("wantAlertSourceCount: %d, got alertsCount: %d",
						tc.wantAlertSourceCount, alertsCount)
				}
				if diff := cmp.Diff(tc.wantUserInboxes, a.Notifier.GetUserInboxes()); diff != "" {
					t.Fatalf("user inboxes mismatch (-want +got):%s\n", diff)
				}
			}
		})
	}
}

func TestAddUsersToAlertRule(t *testing.T) {
	tests := map[string]struct {
		wantErr                        bool
		mockAlertStore, wantAlertStore *store.Schema
		wantUserInboxes                notifier.Schema
		wantStatus                     int
		emailsToAdd                    store.Emails
		alertId                        string
		wantAlertSourceCount           int
	}{
		"add 1 email to alert rule": {
			wantErr: false,
			mockAlertStore: &store.Schema{
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{
						AlertId: AlertId,
						Email:   UserEmail,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{
						AlertId: AlertId,
						Email:   UserEmail,
					},
					{
						AlertId: AlertId,
						Email:   AnotherUserEmail,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
					},
				},
				AlertStates: []store.AlertState{},
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
							AlertLevel:  store.Warn,
							AlertType:   store.LessThan,
							AlertValues: []float32{OldValue},
							Condition:   LessThanTen,
						},
					},
				},
			},
			emailsToAdd:          store.Emails{Addresses: []string{AnotherUserEmail}},
			alertId:              AlertId,
			wantAlertSourceCount: 1,
		},

		"some new emails, some old emails": {
			wantErr: false,
			mockAlertStore: &store.Schema{
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{
						AlertId: AlertId,
						Email:   UserEmail,
					},
					{
						AlertId: AlertId,
						Email:   AnotherUserEmail,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
					},
				},
			},
			wantAlertStore: &store.Schema{
				LinearCalibrations: []store.StoredLinearCalibration{},
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []store.AlertRecipient{
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
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
					},
				},
				AlertStates: []store.AlertState{},
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
							AlertLevel:  store.Warn,
							AlertType:   store.LessThan,
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
							AlertLevel:  store.Warn,
							AlertType:   store.LessThan,
							AlertValues: []float32{OldValue},
							Condition:   LessThanTen,
						},
					},
				},
			},
			wantStatus:           http.StatusOK,
			emailsToAdd:          store.Emails{Addresses: []string{UserEmail, "user3@alert.com"}},
			alertId:              AlertId,
			wantAlertSourceCount: 1,
		},

		"alert rule not found": {
			wantErr: true,
			mockAlertStore: &store.Schema{
				DeviceAlerts: []store.DeviceAlertRule{
					{
						DeviceId:   RegisteredDeviceId,
						QueryField: RegisteredQueryField,
						AlertId:    AlertId,
					},
				},
				AlertRecipients: []store.AlertRecipient{
					{
						AlertId: AlertId,
						Email:   UserEmail,
					},
				},
				AlertRules: []store.StoredAlertRule{
					{
						AlertId:     AlertId,
						RangeValues: []float32{OldValue},
						Name:        RegisteredAlertName,
						Duration:    "24h",
						AlertType:   store.LessThan,
						AlertLevel:  store.Warn,
					},
				},
			},
			wantStatus:           http.StatusNotFound,
			emailsToAdd:          store.Emails{Addresses: []string{AnotherUserEmail}},
			alertId:              UnregisteredAlertId,
			wantAlertSourceCount: 1,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			a, api, closeFunc := setupApp(t, name+"-testing", tc.mockAlertStore)
			defer closeFunc()
			resp := api.Put(fmt.Sprintf("/alert/%s/emailaddresses", tc.alertId),
				tc.emailsToAdd)
			if tc.wantStatus != resp.Code {
				t.Fatalf("wantStatus: %d, response Code: %v", tc.wantStatus, resp.Code)
			}
			if !tc.wantErr {
				if tc.wantAlertStore != nil {
					diff := cmp.Diff(*tc.wantAlertStore, a.AlertStore.GetSnapshot())
					if diff != "" {
						t.Fatalf("alertStore mismatch (-want +got): %s\n", diff)
					}
				}
				var mu store.MutateAlertUsersResponse
				body := resp.Body.Bytes()
				err := json.Unmarshal(body, &mu)
				require.Nil(t, err)
				if diff := cmp.Diff(tc.emailsToAdd.Addresses, mu.EmailAddresses); diff != "" {
					t.Fatalf("emails mismatch (-want +got):%s\n", diff)
				}
				alertsCount := a.AlertSource.GetSnapshot()
				if tc.wantAlertSourceCount != alertsCount {
					t.Fatalf("wantAlertSourceCount: %d, got alertsCount: %d",
						tc.wantAlertSourceCount, alertsCount)
				}
				if diff := cmp.Diff(tc.wantUserInboxes, a.Notifier.GetUserInboxes()); diff != "" {
					t.Fatalf("user inboxes mismatch (-want +got):%s\n", diff)
				}
			}
		})
	}
}

func SchemaToAlertRules(dbRecord store.Schema) []store.AlertRule {
	var ar store.AlertRule
	rules := make([]store.AlertRule, 0, len(dbRecord.DeviceAlerts))
	rulesMap := make(map[string]store.AlertRule, len(dbRecord.DeviceAlerts))
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
