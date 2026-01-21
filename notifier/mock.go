package notifier

type MockNotifier struct {
	inboxes map[string][]AlertMessage
}

func NewMockNotifier() *MockNotifier {
	return &MockNotifier{
		inboxes: make(map[string][]AlertMessage),
	}
}

func (m *MockNotifier) Notify(message AlertMessage, emails ...string) error {
	for _, e := range emails {
		m.inboxes[e] = append(m.inboxes[e], message)
	}
	return nil
}

func (m *MockNotifier) GetUserInboxes() map[string][]AlertMessage {
	return m.inboxes
}
