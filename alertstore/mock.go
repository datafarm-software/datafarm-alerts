package alertstore

import (
	"fmt"

	"github.com/geraud22/sensordata-alerts/alertsource"
)

type MockAlertStore struct {
	Db *Schema
}

func NewMockAlertStore(db *Schema) *MockAlertStore {
	return &MockAlertStore{
		Db: db,
	}
}

func (m *MockAlertStore) GetSnapshot() *Schema {
	return m.Db
}

func (m *MockAlertStore) GetAlertState(alertId string) (*AlertState, error) {
	var state *AlertState
	for _, as := range m.Db.AlertStates {
		if as.Id == alertId {
			state = &as
		}
	}
	if state == nil {
		return nil, NoAlert
	}
	return state, nil
}

func (m *MockAlertStore) GetAlertRule(alertId string) (AlertRule, error) {
	return AlertRule{}, nil
}

func (m *MockAlertStore) GetAlertRecipients(alertId string) ([]string, error) {
	recipients := make([]string, 0, len(m.Db.AlertRecipients))
	for _, ar := range m.Db.AlertRecipients {
		if ar.AlertId == alertId {
			recipients = append(recipients, ar.UserId)
		}
	}
	return recipients, nil
}

func (m *MockAlertStore) UpdateAlertLastSeen(alert *AlertState) error {
	for _, as := range m.Db.AlertStates {
		if as.Id == alert.Id {
			as.Time = alert.Time
		}
	}
	return nil
}

func (m *MockAlertStore) StoreAlertRule(alertId string, rule AlertRule) error {
	//TODO: deal with uniqueness constraints
	m.Db.AlertRules = append(m.Db.AlertRules, rule)
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

func (m *MockAlertStore) CheckAlertRuleExists(ar AlertRule) (string, error) {
	deviceAlerts, ok := m.values["deviceAlerts"].(map[string]map[string][]string)
	if !ok {
		m.values["deviceAlerts"] = make(map[string]map[string][]string)
		return "", nil
	}
	deviceQueryFieldAlerts, ok := deviceAlerts[ar.DeviceId]
	if !ok {
		return "", nil
	}
	alertIds, ok := deviceQueryFieldAlerts[ar.QueryField]
	if !ok {
		return "", nil
	}
	alertRules, ok := m.values["alertRules"].(map[string]alertsource.AlertRule)
	if !ok {
		m.values["alertRules"] = make(map[string]alertsource.AlertRule)
		return "", fmt.Errorf("alertrules was empty")
	}
	for _, alertId := range alertIds {
		if _, ok := alertRules[alertId]; ok {
			return alertId, AlertExists
		}
	}
	return "", nil
}

func (m *MockAlertStore) UpdateAlertState(alert *AlertState) error {
	return fmt.Errorf("not implemented")
}
