package notifier

import (
	"fmt"
	"time"

	"github.com/geraud22/sensordata-alerts/alertstore"
)

type Schema struct {
	Notifications map[string][]AlertNotification
	CreatedAlerts map[string][]AlertCreated
	DeletedAlerts map[string][]AlertDeleted
}

const Notification = "Hi %s,\nThis email to notify alert level %s - %.2f.\nDetails:\n- Name: %s\n- N- DeviceId: %s\n- N- QueryField: %s\n- N- Time Alert Triggered: %s"

const AlertCreatedNotification = "Hi %s, \nThis email to notify alert created with the following details. Alert Name: %s, DeviceId: %s, QueryField: %s, Duration: %s. Type: %s, Condition: %s"

const AlertDeletedNotification = "Hi %s,\n This email to notify that you will no longer receive alerts for alert: %s. Details: DeviceId: %s, QueryField: %s, Duration: %s, Type: %s, Condition: %s."

type AlertNotification struct {
	Name       string
	DeviceId   string
	QueryField string
	Value      float64
	Level      alertstore.AlertLevel
	Timestamp  time.Time
}

func (a AlertNotification) String(username string) string {
	return fmt.Sprintf(
		Notification,
		username, alertstore.AlertLevelString[a.Level], a.Value,
		a.Name, a.DeviceId, a.QueryField, a.Timestamp.String())
}

type AlertCreated struct {
	Name        string
	DeviceId    string
	QueryField  string
	Duration    string
	AlertLevel  alertstore.AlertLevel
	AlertType   alertstore.AlertType
	AlertValues []float32
}

func (a AlertCreated) String(username string) (string, error) {
	condition, err := formatAlertCondition(a.AlertType, a.AlertValues)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(AlertCreatedNotification, username, a.Name, a.DeviceId,
		a.QueryField, a.Duration, alertstore.AlertLevelString[a.AlertLevel],
		condition), nil
}

func formatAlertCondition(alertType alertstore.AlertType, alertValues []float32) (string, error) {
	var condition string
	switch alertType {
	case alertstore.LessThan:
		if len(alertValues) != 1 {
			return "", fmt.Errorf("received len(%d) alertValues slice on LessThan type",
				len(alertValues))
		}
		condition = fmt.Sprintf("< %.2f", alertValues[0])
	case alertstore.GreaterThan:
		if len(alertValues) != 1 {
			return "", fmt.Errorf("received len(%d) alertValues slice on GreaterThan type",
				len(alertValues))
		}
		condition = fmt.Sprintf("< %.2f", alertValues[0])
	case alertstore.Range:
		if len(alertValues) != 2 {
			return "", fmt.Errorf("received len(%d) alertValues slice on Range type",
				len(alertValues))
		}
		condition = fmt.Sprintf("> %.2f, %.2f <", alertValues[0], alertValues[1])
	case alertstore.InactiveSensor:
		condition = "Inactive Sensor"
	default:
		return "", fmt.Errorf("unknown alert type: %d", alertType)
	}
	return condition, nil
}

type AlertDeleted AlertCreated

func (a AlertDeleted) String(username string) (string, error) {
	condition, err := formatAlertCondition(a.AlertType, a.AlertValues)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(AlertDeletedNotification, username, a.Name, a.DeviceId,
		a.QueryField, a.Duration, alertstore.AlertLevelString[a.AlertLevel],
		condition), nil
}

type TestingNotifier interface {
	GetUserInboxes() Schema
}

type Notifier interface {
	TestingNotifier
	Close() error
	AlertNotify(message AlertNotification, emails ...string) error
	AlertCreatedNotify(message AlertCreated, emails ...string) error
	AlertDeletedNotify(message AlertDeleted, emails ...string) error
}
