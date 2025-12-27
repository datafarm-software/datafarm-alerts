package alertstore

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
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
			state = as
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

func (m *MockAlertStore) AddUsersToNotifyList(alertId string, userIds []string) error {
	//TODO: deal with uniqueness constraints
	for _, id := range userIds {
		m.Db.AlertRecipients = append(m.Db.AlertRecipients, AlertRecipient{
			AlertId: alertId, UserId: id,
		})
	}
	return nil
}

func (m *MockAlertStore) CheckAlertRuleExists(ar AlertRule) (string, error) {
	//TODO: should this function just return a bool
	var exists bool
	for _, rule := range m.Db.AlertRules {
		if diff := cmp.Diff(rule, ar); diff == "" {
			exists = true
		}
	}
	var id string
	var err error
	if exists {
		id = ar.AlertId
		err = AlertExists
	}
	return id, err
}

func (m *MockAlertStore) UpdateAlertState(alert *AlertState) error {
	return fmt.Errorf("not implemented")
}
