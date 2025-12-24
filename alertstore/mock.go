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
	if _, ok := values["alertStore"].(map[string]alertsource.AlertState); !ok {
		values["alertStore"] = make(map[string]alertsource.AlertState)
	}
	if _, ok := values["deviceAlerts"].(map[string]map[string]string); !ok {
		values["deviceAlerts"] = make(map[string]map[string]string)
	}
	return &MockAlertStore{
		values: values,
	}
}

func (m *MockAlertStore) GetValues() map[any]any {
	return m.values
}

func (m *MockAlertStore) GetAlertState(alertId string) (
	*alertsource.AlertState, error) {
	alertStore, ok := m.values["alertStore"].(map[string]alertsource.AlertState)
	if !ok {
		return nil, fmt.Errorf("no alertstore found")
	}
	previousAlert, ok := alertStore[alertId]
	if !ok {
		return nil, fmt.Errorf(
			"no found alert in store for id: %s", alertId)
	}
	return &previousAlert, nil
}

func (m *MockAlertStore) GetAlertRule(alertId string) (alertsource.AlertRule, error) {
	return alertsource.AlertRule{}, nil
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

func (m *MockAlertStore) UpdateAlertLastSeen(alert *alertsource.AlertState) error {
	alerts, ok := m.values["alertStore"].(map[string]alertsource.AlertState)
	if !ok {
		return fmt.Errorf("no alertstore found")
	}
	alerts[alert.Id] = *alert
	m.values["alertStore"] = alerts
	return nil
}

func (m *MockAlertStore) StoreAlertRule(alertId string, ar alertsource.AlertRule) error {
	deviceAlerts, ok := m.values["deviceAlerts"].(map[string]map[string]string)
	if !ok {
		deviceAlerts = make(map[string]map[string]string)
	}
	deviceQueryFieldAlerts, ok := deviceAlerts[ar.DeviceId]
	if !ok {
		deviceQueryFieldAlerts = make(map[string]string)
	}
	deviceQueryFieldAlerts[ar.QueryField] = alertId
	deviceAlerts[ar.DeviceId] = deviceQueryFieldAlerts
	m.values["deviceAlerts"] = deviceAlerts

	alerts, ok := m.values["alertStore"].(map[string]alertsource.AlertState)
	if !ok {
		alerts = make(map[string]alertsource.AlertState)
	}
	duration, err := ParseDuration(ar.Duration)
	if err != nil {
		return fmt.Errorf("parsing duration: %v", err)
	}
	alerts[alertId] = alertsource.AlertState{
		Duration:    duration,
		Level:       alertsource.Ok,
		RangeValues: ar.RangeValues,
		Id:          alertId,
	}
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

func (m *MockAlertStore) CheckAlertRuleExists(ar alertsource.AlertRule) (string, error) {
	deviceAlerts, ok := m.values["deviceAlerts"].(map[string]map[string]string)
	if !ok {
		m.values["deviceAlerts"] = make(map[string]map[string]string)
		return "", nil
	}
	deviceQueryFieldAlerts, ok := deviceAlerts[ar.DeviceId]
	if !ok {
		return "", nil
	}
	alertId, ok := deviceQueryFieldAlerts[ar.QueryField]
	if !ok {
		return "", nil
	}
	alertStore, ok := m.values["alertStore"].(map[string]alertsource.AlertState)
	if !ok {
		m.values["alertStore"] = make(map[string]alertsource.AlertState)
		return "", fmt.Errorf("alertstore is empty")
	}
	existingAlert, ok := alertStore[alertId]
	if ok {
		return existingAlert.Id, AlertExists
	}
	return "", nil
}

func (m *MockAlertStore) UpdateAlertState(alert *alertsource.AlertState) error {
	return fmt.Errorf("not implemented")
}
