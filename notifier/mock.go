package notifier

import (
	"fmt"
	"log"
)

type MockNotifier struct {
	inboxes Schema
}

func NewMockNotifier() (Notifier, error) {
	return &MockNotifier{
		inboxes: Schema{
			Notifications: make(map[string][]AlertNotification),
			CreatedAlerts: make(map[string][]AlertCreated),
		},
	}, nil
}

func (m *MockNotifier) Close() error {
	return nil
}

func (m *MockNotifier) AlertNotify(message AlertNotification, emails ...string) error {
	for _, e := range emails {
		m.inboxes.Notifications[e] = append(m.inboxes.Notifications[e], message)
	}
	log.Printf("message:\n %v", message)
	return nil
}

func (m *MockNotifier) AlertCreatedNotify(message AlertCreated, emails ...string) error {
	return fmt.Errorf("not implemented")
}

func (m *MockNotifier) AlertDeletedNotify(message AlertDeleted, emails ...string) error {
	return fmt.Errorf("not implemented")
}

func (m *MockNotifier) GetUserInboxes() Schema {
	return m.inboxes
}
