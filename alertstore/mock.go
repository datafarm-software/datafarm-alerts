package alertstore

import (
	"slices"
	"time"

	"github.com/google/go-cmp/cmp"
)

type MockAlertStore struct {
	Db *Schema
}

func NewMockAlertStore(mode Mode, dbName string) (*MockAlertStore, error) {
	return &MockAlertStore{}, nil
}

func (m *MockAlertStore) Close() error {
	return nil
}

func (m *MockAlertStore) PrepareDb(db Schema) error {
	if db.DeviceAlerts == nil {
		db.DeviceAlerts = []DeviceAlertRule{}
	}
	if db.AlertRecipients == nil {
		db.AlertRecipients = []AlertRecipient{}
	}
	if db.AlertRules == nil {
		db.AlertRules = []StoredAlertRule{}
	}
	if db.AlertStates == nil {
		db.AlertStates = []AlertState{}
	}
	m.Db = &db
	return nil
}

func (m *MockAlertStore) GetSnapshot() Schema {
	return *m.Db
}

func (m *MockAlertStore) GetAlertState(alertId string) (AlertState, error) {
	var state, emptyState AlertState
	if len(m.Db.AlertStates) < 1 {
		return AlertState{
			Time: time.Now(),
		}, nil
	}
	for _, as := range m.Db.AlertStates {
		if as.Id == alertId {
			state = as
		}
	}
	var err error
	if diff := cmp.Diff(state, emptyState); diff == "" {
		err = NoAlert
	}
	return state, err
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
		ar.AlertType = rule.AlertType
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

func (m *MockAlertStore) StoreAlertRule(dbRecord Schema) error {
	//TODO: deal with uniqueness constraints
	m.Db.DeviceAlerts = append(m.Db.DeviceAlerts, dbRecord.DeviceAlerts...)
	m.Db.AlertRecipients = append(m.Db.AlertRecipients, dbRecord.AlertRecipients...)
	m.Db.AlertRules = append(m.Db.AlertRules, dbRecord.AlertRules...)
	return nil
}

func (m *MockAlertStore) UpdateAlertRule(alertId string, dbRecord Schema) error {
	for i, storedDa := range m.Db.DeviceAlerts {
		var modified bool
		for _, currentDa := range dbRecord.DeviceAlerts {
			if currentDa.AlertId != storedDa.AlertId {
				continue
			}
			if !modified {
				m.Db.DeviceAlerts[i] = currentDa
				modified = true
			} else {
				m.Db.DeviceAlerts = append(m.Db.DeviceAlerts, currentDa)
			}
		}
	}
	for i, storedRec := range m.Db.AlertRecipients {
		var modified bool
		for _, currentRec := range dbRecord.AlertRecipients {
			if currentRec.AlertId != storedRec.AlertId {
				continue
			}
			if !modified {
				m.Db.AlertRecipients[i] = currentRec
				modified = true
			} else {
				m.Db.AlertRecipients = append(m.Db.AlertRecipients, currentRec)
			}
		}
	}
	for i, storedAr := range m.Db.AlertRules {
		var modified bool
		for _, currentAr := range dbRecord.AlertRules {
			if currentAr.AlertId != storedAr.AlertId {
				continue
			}
			if !modified {
				m.Db.AlertRules[i] = currentAr
				modified = true
			} else {
				m.Db.AlertRules = append(m.Db.AlertRules, currentAr)
			}
		}
	}
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
			Duration:  ar.Duration,
			AlertType: ar.AlertType,
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

func (m *MockAlertStore) UpdateAlertState(alert AlertState) error {
	if len(m.Db.AlertStates) < 1 {
		m.Db.AlertStates = append(m.Db.AlertStates, alert)
		return nil
	}
	for i, state := range m.Db.AlertStates {
		if state.Id != alert.Id {
			continue
		}
		// alert.Time = time.Now()
		m.Db.AlertStates[i] = alert
	}
	return nil
}

func (m *MockAlertStore) DeleteAlertState(alertId string) error {
	var deleted bool
	for i, state := range m.Db.AlertStates {
		if state.Id != alertId {
			continue
		}
		m.Db.AlertStates = slices.Delete(m.Db.AlertStates, i, i+1)
		deleted = true
	}
	var err error
	if !deleted {
		err = NoAlert
	}
	return err
}

func (m *MockAlertStore) DeleteAlertRule(alertId string) (AlertRule, error) {
	var rule AlertRule
	var deleted bool
	var err error
	for i, da := range m.Db.DeviceAlerts {
		if da.AlertId != alertId {
			continue
		}
		rule.DeviceId = da.DeviceId
		rule.QueryField = da.QueryField
		m.Db.DeviceAlerts = slices.Delete(m.Db.DeviceAlerts, i, i+1)
		deleted = true
	}
	if !deleted {
		err = NoAlert
	}

	var emails []string
	for i, ar := range m.Db.AlertRecipients {
		if ar.AlertId != alertId {
			continue
		}
		emails = append(emails, ar.Email)
		m.Db.AlertRecipients = slices.Delete(m.Db.AlertRecipients, i, i+1)
	}
	rule.EmailAddresses = emails

	for i, r := range m.Db.AlertRules {
		if r.AlertId != alertId {
			continue
		}
		rule.RangeValues = r.RangeValues
		rule.Duration = r.Duration
		rule.AlertType = r.AlertType
		m.Db.AlertRules = slices.Delete(m.Db.AlertRules, i, i+1)
	}
	return rule, err
}
