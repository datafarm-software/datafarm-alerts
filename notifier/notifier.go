package notifier

import (
	"time"

	"github.com/geraud22/sensordata-alerts/alertstore"
)

type AlertMessage struct {
	Name       string
	DeviceId   string
	QueryField string
	Level      alertstore.AlertLevel
	Timestamp  time.Time
}

type Notifier interface {
	Notify(message AlertMessage, emails ...string) error
}
