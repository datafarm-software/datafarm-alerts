package alertsource

import (
	"fmt"

	"github.com/geraud22/datafarm-alerts/alertstore"
)

type MockAlertSource struct {
	alertsCount int
}

func NewMockAlertSource() (*MockAlertSource, error) {
	return &MockAlertSource{}, nil
}

func (m *MockAlertSource) Close() error {
	return nil
}

func (m *MockAlertSource) PrepareDb(alerts ...alertstore.AlertRule) error {
	m.alertsCount = len(alerts)
	return nil
}

func (m *MockAlertSource) GetSnapshot() int {
	return m.alertsCount
}

func (m *MockAlertSource) UpdateAlertRule(ar alertstore.AlertRule) error {
	return nil
}

func (m *MockAlertSource) CreateAlertRule(ar alertstore.AlertRule) (string, error) {
	alertCount := m.alertsCount + 1
	ar.AlertId = fmt.Sprintf("alert%d", alertCount)
	m.alertsCount++
	return ar.AlertId, nil
}

func (m *MockAlertSource) DeleteAlertRule(alertId string) error {
	m.alertsCount--
	return nil
}
