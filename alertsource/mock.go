package alertsource

type MockAlertSource struct {
	values map[any]any
}

func NewMockAlertSource(values map[any]any) *MockAlertSource {
	return &MockAlertSource{values: values}
}
