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
	return &MockAlertStore{
		values: values,
	}
}

func (m *MockAlertStore) GetPreviousAlert(deviceId, queryField string) (
	*alertsource.AlertState, error) {
	alerts, ok := m.values["alerts"].(map[string]map[string]alertsource.AlertState)
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
