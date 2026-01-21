package notifier

import (
	"io"
	"strings"

	"gopkg.in/gomail.v2"
)

type TestingGomail struct {
	gomail.Sender
	testingInboxes map[string][]AlertMessage
}

func NewTestingGoMail() (Notifier, error) {
	return &TestingGomail{
		testingInboxes: make(map[string][]AlertMessage),
	}, nil
}

func (g *TestingGomail) Notify(message AlertMessage, emails ...string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", "tester@alerts.com")
	m.SetHeader("Subject", "DataFarm Alert Notification")
	var emailParts []string
	var username string
	var err error
	var mockSender gomail.SendFunc
	for _, email := range emails {
		emailParts = strings.Split(email, "@")
		username = emailParts[0]
		m.SetBody("text/html", message.ToHtml(username))
		mockSender = gomail.SendFunc(g.Send)
		if err = mockSender.Send("tester@alerts.com", []string{email}, m); err != nil {
			break
		}
		g.testingInboxes[email] = append(g.testingInboxes[email], message)
	}
	return err
}

func (g *TestingGomail) Send(from string, to []string, msg io.WriterTo) error {
	return nil
}

func (g *TestingGomail) GetUserInboxes() map[string][]AlertMessage {
	return g.testingInboxes
}
