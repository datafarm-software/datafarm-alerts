package alertsource

import "fmt"

type MockAlertSource struct {
	values map[any]any
}

func NewMockAlertSource(values map[any]any) *MockAlertSource {
	return &MockAlertSource{values: values}
}

func (m *MockAlertSource) GetAlertState() *AlertState {
	return nil
}

func (m *MockAlertSource) GetAlertLevel() AlertLevel {
	return Ok
}

func (m *MockAlertSource) UpdateAlertRule(ar AlertRule) error {
	return fmt.Errorf("not implemented")
}

func (m *MockAlertSource) RegisterAlertRule(ar AlertRule) (string, error) {
	return "alert1", nil
}
