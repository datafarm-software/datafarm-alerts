package notifier

import (
	"fmt"
)

type MockNotifier struct {
	values map[any]any
}

func NewMockNotifier(values map[any]any) *MockNotifier {
	if values == nil {
		values = make(map[any]any)
	}
	if _, ok := values["userInboxes"].(map[string][]AlertMessage); !ok {
		values["userInboxes"] = make(map[string][]AlertMessage)
	}
	return &MockNotifier{
		values: values,
	}
}

func (m *MockNotifier) Notify(message AlertMessage, emails ...string) error {
	userInboxes, ok := m.values["userInboxes"].(map[string][]AlertMessage)
	if !ok {
		return fmt.Errorf("found no userInboxes")
	}
	for _, e := range emails {
		userInboxes[e] = append(userInboxes[e], message)
	}
	return nil
}

func (m *MockNotifier) GetUserInboxes() map[string][]AlertMessage {
	userInboxes, _ := m.values["userInboxes"].(map[string][]AlertMessage)
	return userInboxes
}
