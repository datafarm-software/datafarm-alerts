package notifier

import "log"

type MockNotifier struct {
	inboxes map[string][]AlertMessage
}

func NewMockNotifier() (*MockNotifier, error) {
	return &MockNotifier{
		inboxes: make(map[string][]AlertMessage),
	}, nil
}

func (m *MockNotifier) Notify(message AlertMessage, emails ...string) error {
	for _, e := range emails {
		m.inboxes[e] = append(m.inboxes[e], message)
	}
	log.Printf("message:\n %v", message)
	return nil
}

func (m *MockNotifier) GetUserInboxes() map[string][]AlertMessage {
	return m.inboxes
}
