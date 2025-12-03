package alertsource

type MockAlertSource struct {
	values map[any]any
}

func NewMockAlertSource(values map[any]any) *MockAlertSource {
	return &MockAlertSource{values: values}
}

func (m *MockAlertSource) GetAlert() *Alert {
	return nil
}

func (m *MockAlertSource) RegisterAlert(ar AlertRule) (string, error) {
	return "alert1", nil
}
