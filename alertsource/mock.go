package alertsource

import (
	"fmt"
	"slices"

	"github.com/geraud22/sensordata-alerts/alertstore"
)

type MockAlertSource struct {
	alerts []alertstore.AlertRule
}

func NewMockAlertSource() (*MockAlertSource, error) {
	return &MockAlertSource{}, nil
}

func (m *MockAlertSource) Close() error {
	return nil
}

func (m *MockAlertSource) PrepareDb(alerts []alertstore.AlertRule) error {
	m.alerts = alerts
	return nil
}

func (m *MockAlertSource) GetSnapshot() []alertstore.AlertRule {
	return m.alerts
}

func (m *MockAlertSource) UpdateAlertRule(ar alertstore.AlertRule) error {
	return fmt.Errorf("not implemented")
}

func (m *MockAlertSource) CreateAlertRule(ar alertstore.AlertRule) (string, error) {
	alertCount := len(m.alerts) + 1
	ar.AlertId = fmt.Sprintf("alert%d", alertCount)
	m.alerts = append(m.alerts, ar)
	return ar.AlertId, nil
}

func (m *MockAlertSource) DeleteAlertRule(alertId string) error {
	for i, alert := range m.alerts {
		if alert.AlertId == alertId {
			m.alerts = slices.Delete(m.alerts, i, i+1)
		}
	}
	return nil
}
