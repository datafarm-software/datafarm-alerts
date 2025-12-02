package alertstore

import (
	"fmt"
	"time"

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

func (m *MockAlertStore) StoreAlert(ca alertsource.CreateAlert) error {
	alerts, ok := m.values["alertStore"].(map[string]map[string]alertsource.Alert)
	if !ok {
		return fmt.Errorf("no alertstore found")
	}
	deviceAlerts, ok := alerts[ca.DeviceId]
	if !ok {
		deviceAlerts = make(map[string]alertsource.Alert)
	}
	deviceAlerts[ca.QueryField] = alertsource.Alert{
		Time:       time.Now(),
		Duration:   ParseDuration(ca.Duration),
		DeviceId:   ca.DeviceId,
		QueryField: ca.QueryField,
		Id:         ca.Id,
		Level:      alertsource.Ok,
	}
	alerts[ca.DeviceId] = deviceAlerts
	m.values["alertStore"] = alerts
	return nil
}
