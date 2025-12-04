package alertstore

import (
	"fmt"

	"github.com/geraud22/sensordata-alerts/alertsource"
)

type MockAlertStore struct {
	values map[any]any
}

func NewMockAlertStore(values map[any]any) *MockAlertStore {
	if values == nil {
		values = make(map[any]any)
	}
	if _, ok := values["alertRecipients"].(map[string][]string); !ok {
		values["alertRecipients"] = make(map[string][]string)
	}
	if _, ok := values["alertStore"].(map[string]map[string]alertsource.Alert); !ok {
		values["alertStore"] = make(map[string]map[string]alertsource.Alert)
	}
	return &MockAlertStore{
		values: values,
	}
}

func (m *MockAlertStore) GetValues() map[any]any {
	return m.values
}

func (m *MockAlertStore) GetPreviousAlert(deviceId, queryField string) (
	*alertsource.Alert, error) {
	alerts, ok := m.values["alertStore"].(map[string]map[string]alertsource.Alert)
	if !ok {
		return nil, fmt.Errorf("no alertstore found")
	}
	deviceAlerts, ok := alerts[deviceId]
	if !ok {
		return nil, fmt.Errorf("no device alerts for: %s", deviceId)
	}
	previousAlert, ok := deviceAlerts[queryField]
	if !ok {
		return nil, fmt.Errorf(
			"no previous alert for device: %s, querField: %s", deviceId, queryField)
	}
	return &previousAlert, nil
}

func (m *MockAlertStore) GetAlertRecipients(id string) ([]string, error) {
	recipientStore, ok := m.values["alertRecipients"].(map[string][]string)
	if !ok {
		return nil, fmt.Errorf("no recipient store")
	}
	recipients, ok := recipientStore[id]
	if !ok {
		return nil, fmt.Errorf("no recipients for: %s", id)
	}
	return recipients, nil
}

func (m *MockAlertStore) Update(alert *alertsource.Alert) error {
	alerts, ok := m.values["alertStore"].(map[string]map[string]alertsource.Alert)
	if !ok {
		return fmt.Errorf("no alertstore found")
	}
	deviceAlerts, ok := alerts[alert.DeviceId]
	if !ok {
		return fmt.Errorf("no device alerts for: %s", alert.DeviceId)
	}
	deviceAlerts[alert.QueryField] = *alert
	alerts[alert.DeviceId] = deviceAlerts
	m.values["alertStore"] = alerts
	return nil
}

func (m *MockAlertStore) StoreAlert(alertId string, ar alertsource.AlertRule) error {
	alerts, ok := m.values["alertStore"].(map[string]map[string]alertsource.Alert)
	if !ok {
		return fmt.Errorf("no alertstore found")
	}
	deviceAlerts, ok := alerts[ar.DeviceId]
	if !ok {
		deviceAlerts = make(map[string]alertsource.Alert)
	}
	duration, err := ParseDuration(ar.Duration)
	if err != nil {
		return fmt.Errorf("parsing duration: %v", err)
	}
	deviceAlerts[ar.QueryField] = alertsource.Alert{
		Duration:    duration,
		DeviceId:    ar.DeviceId,
		QueryField:  ar.QueryField,
		Level:       alertsource.Ok,
		RangeValues: ar.RangeValues,
		Id:          alertId,
	}
	alerts[ar.DeviceId] = deviceAlerts
	m.values["alertStore"] = alerts
	return nil
}

func (m *MockAlertStore) AddUsersToNotifyList(alertId string, usernames []string) error {
	recipientStore, ok := m.values["alertRecipients"].(map[string][]string)
	if !ok {
		return fmt.Errorf("no recipient store")
	}
	recipients := recipientStore[alertId]
	recipients = append(recipients, usernames...)
	recipientStore[alertId] = recipients
	m.values["alertRecipients"] = recipientStore
	return nil
}

func (m *MockAlertStore) CheckAlertExists(ar alertsource.AlertRule) (string, error) {
	var alertId string
	alerts, ok := m.values["alertStore"].(map[string]map[string]alertsource.Alert)
	if !ok {
		return alertId, fmt.Errorf("no alertstore found")
	}
	deviceAlerts, ok := alerts[ar.DeviceId]
	if !ok {
		deviceAlerts = make(map[string]alertsource.Alert)
		return alertId, nil
	}
	existingAlert, ok := deviceAlerts[ar.QueryField]
	if !ok {
		return alertId, nil
	}
	currentAlert := alertsource.Alert{
		DeviceId:   ar.DeviceId,
		QueryField: ar.QueryField,
	}
	if AlertsMatch(existingAlert, currentAlert) {
		alertId = existingAlert.Id
		return alertId, AlertExists
	}
	return "", nil
}
