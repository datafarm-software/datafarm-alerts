package alertsource

import (
	"fmt"

	"github.com/geraud22/sensordata-alerts/alertstore"
)

type MockAlertSource struct {
	values map[any]any
}

func NewMockAlertSource(values map[any]any) *MockAlertSource {
	if values == nil {
		values = make(map[any]any)
		values["alertsCount"] = 1
	}
	return &MockAlertSource{values: values}
}

func (m *MockAlertSource) GetAlertState() *alertstore.AlertState {
	return nil
}

func (m *MockAlertSource) GetAlertLevel() alertstore.AlertLevel {
	return alertstore.Ok
}

func (m *MockAlertSource) UpdateAlertRule(ar alertstore.AlertRule) error {
	return fmt.Errorf("not implemented")
}

func (m *MockAlertSource) RegisterAlertRule(ar alertstore.AlertRule) (string, error) {
	alertsCount, _ := m.values["alertsCount"].(int)
	m.values["alertsCount"] = alertsCount + 1
	return fmt.Sprintf("alert%d", alertsCount), nil
}
