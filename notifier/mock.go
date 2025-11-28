package notifier

import "fmt"

type MockNotifier struct {
	values map[any]any
}

func NewMockNotifier(values map[any]any) *MockNotifier {
	if values == nil {
		values = make(map[any]any)
	}
	if _, ok := values["userInboxes"].(map[string][]string); !ok {
		values["userInboxes"] = make(map[string][]string)
	}
	return &MockNotifier{
		values: values,
	}
}

func (m *MockNotifier) Notify(users []string, message string) error {
	userInboxes, ok := m.values["userInboxes"].(map[string][]string)
	if !ok {
		return fmt.Errorf("found no userInboxes")
	}
	for _, user := range users {
		userInboxes[user] = append(userInboxes[user], message)
	}
	return nil
}

func (m *MockNotifier) GetUserInboxes() map[string][]string {
	userInboxes, _ := m.values["userInboxes"].(map[string][]string)
	return userInboxes
}
