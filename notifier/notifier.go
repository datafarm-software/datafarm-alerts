package notifier

import (
	"fmt"
	"time"

	"github.com/geraud22/sensordata-alerts/alertstore"
)

const AlertMessageString = `
	Hi %s,

	This email to notify alert level %s.
	Details:
	- Name: %s
	- DeviceId: %s
	- QueryField: %s
	- Time Alert Started: %s
`

type AlertMessage struct {
	Name       string
	DeviceId   string
	QueryField string
	Level      alertstore.AlertLevel
	Timestamp  time.Time
}

func (a AlertMessage) ToHtml(username string) string {
	return fmt.Sprintf(
		AlertMessageString,
		username, alertstore.AlertLevelString[a.Level],
		a.Name, a.DeviceId, a.QueryField, a.Timestamp.String())
}

type Notifier interface {
	Notify(message AlertMessage, emails ...string) error
	//NOTE: used in testing
	GetUserInboxes() map[string][]AlertMessage
}
