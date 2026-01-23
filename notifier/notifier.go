package notifier

import (
	"fmt"
	"time"

	"github.com/geraud22/sensordata-alerts/alertstore"
)

const AlertMessageString = "Hi %s,\nThis email to notify alert level %s - %.2f.\nDetails:\n- Name: %s\n- N- DeviceId: %s\n- N- QueryField: %s\n- N- Time Alert Triggered: %s"

type AlertMessage struct {
	Name       string
	DeviceId   string
	QueryField string
	Value      float64
	Level      alertstore.AlertLevel
	Timestamp  time.Time
}

func (a AlertMessage) ToHtml(username string) string {
	return fmt.Sprintf(
		AlertMessageString,
		username, alertstore.AlertLevelString[a.Level], a.Value,
		a.Name, a.DeviceId, a.QueryField, a.Timestamp.String())
}

type TestingNotifier interface {
	GetUserInboxes() map[string][]AlertMessage
}

type Notifier interface {
	TestingNotifier
	Notify(message AlertMessage, emails ...string) error
}
