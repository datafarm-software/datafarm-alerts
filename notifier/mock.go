package notifier

type MockNotifier struct{}

func (m *MockNotifier) Notify(users []string, message string) error {
	return nil
}
