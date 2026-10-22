package templates

type MockTemplater struct{}

func NewMockTemplater() (*MockTemplater, error) {
	return &MockTemplater{}, nil
}

func (m *MockTemplater) Render(name TemplateName, data any) (string, error) {
	return "", nil
}
