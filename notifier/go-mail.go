package notifier

import (
	"fmt"

	"github.com/geraud22/sensordata-alerts/alertstore"
)

type Gomail struct {
	testingInboxes map[string][]AlertMessage
	mode           alertstore.Mode
}

func NewGoMail() (*Gomail, error) {
	return &Gomail{}, nil
}

func (g *Gomail) Notify(message AlertMessage, emails ...string) error {
	return fmt.Errorf("not implemented")
}

func (g *Gomail) GetUserInboxes() map[string][]AlertMessage {
	return g.testingInboxes
}
