package alertstore

import (
	"fmt"

	"github.com/google/go-cmp/cmp"
)

type MockAlertStore struct {
	Db *Schema
}

func NewMockAlertStore(db *Schema) *MockAlertStore {
	if db == nil {
		db = &Schema{}
	}
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
	ar := AlertRule{
		AlertId: alertId,
	}
	for _, da := range m.Db.DeviceAlerts {
		if ar.AlertId != da.AlertId {
			continue
		}
		ar.DeviceId = da.DeviceId
		ar.QueryField = da.QueryField
	}
	for _, recipient := range m.Db.AlertRecipients {
		if ar.AlertId != recipient.AlertId {
			continue
		}
		ar.EmailAddresses = append(ar.EmailAddresses, recipient.Email)
	}
	for _, rule := range m.Db.AlertRules {
		if ar.AlertId != rule.AlertId {
			continue
		}
		ar.RangeValues = append(ar.RangeValues, rule.RangeValues...)
		ar.AlertRange = rule.AlertRange
		ar.Duration = rule.Duration
	}
	var err error
	if ar.DeviceId == "" {
		err = NoAlert
	}
	return ar, err
}

func (m *MockAlertStore) GetAlertRecipients(alertId string) ([]string, error) {
	recipients := make([]string, 0, len(m.Db.AlertRecipients))
	for _, ar := range m.Db.AlertRecipients {
		if ar.AlertId == alertId {
			recipients = append(recipients, ar.Email)
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

func (m *MockAlertStore) StoreAlertRule(alertId string, dbRecord Schema) error {
	//TODO: deal with uniqueness constraints
	m.Db.DeviceAlerts = append(m.Db.DeviceAlerts, dbRecord.DeviceAlerts...)
	m.Db.AlertRecipients = append(m.Db.AlertRecipients, dbRecord.AlertRecipients...)
	m.Db.AlertRules = append(m.Db.AlertRules, dbRecord.AlertRules...)
	return nil
}

func (m *MockAlertStore) UpdateAlertRule(alertId string, dbRecord Schema) error {
	return nil
}

func (m *MockAlertStore) CheckAlertRuleExists(ar AlertRule) (string, error) {
	//TODO: should this function just return a bool
	var exists bool
	for _, deviceAlert := range m.Db.DeviceAlerts {
		if deviceAlert.DeviceId == ar.DeviceId &&
			deviceAlert.QueryField == ar.QueryField {
			exists = true
		}
	}
	if exists {
		storedAlert := StoredAlertRule{
			Duration:   ar.Duration,
			AlertRange: ar.AlertRange,
		}
		for _, rule := range m.Db.AlertRules {
			rule.RangeValues = nil
			rule.AlertId = ""
			if diff := cmp.Diff(rule, storedAlert); diff != "" {
				exists = false
			}
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
