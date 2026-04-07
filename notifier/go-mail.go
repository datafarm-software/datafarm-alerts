package notifier

import (
	"fmt"
	"io"
	"strings"

	"gopkg.in/gomail.v2"
)

type TestingGomail struct {
	inboxes Schema
}

func NewTestingGoMail() (Notifier, error) {
	return &TestingGomail{
		inboxes: Schema{
			Notifications: make(map[string][]AlertNotification),
			CreatedAlerts: make(map[string][]AlertCreated),
		},
	}, nil
}

func (g *TestingGomail) Close() error {
	return nil
}

func (g *TestingGomail) AlertNotify(message AlertNotification, emails ...string) error {
	var err error
	for _, email := range emails {
		err = g.sendEmail(message, email)
		if err != nil {
			break
		}
		g.inboxes.Notifications[email] = append(g.inboxes.Notifications[email],
			message)
	}
	if err != nil {
		return err
	}
	return nil
}

func (g *TestingGomail) AlertCreatedNotify(message AlertCreated, emails ...string) error {
	var err error
	for _, email := range emails {
		err = g.sendEmail(message, email)
		if err != nil {
			break
		}
		g.inboxes.CreatedAlerts[email] = append(g.inboxes.CreatedAlerts[email],
			message)
	}
	if err != nil {
		return err
	}
	return nil
}

func (g *TestingGomail) AlertDeletedNotify(message AlertDeleted, emails ...string) error {
	var err error
	for _, email := range emails {
		err = g.sendEmail(message, email)
		if err != nil {
			break
		}
		g.inboxes.DeletedAlerts[email] = append(g.inboxes.DeletedAlerts[email],
			message)
	}
	if err != nil {
		return err
	}
	return nil
}

func (g *TestingGomail) sendEmail(message Message, email string) error {
	emailParts := strings.Split(email, "@")
	username := emailParts[0]
	m := gomail.NewMessage()
	messageStr, err := message.String(username)
	if err != nil {
		return fmt.Errorf("message to string: %v", err)
	}
	m.SetHeader("From", "tester@alerts.com")
	m.SetHeader("Subject", "DataFarm Alert Notification")
	m.SetBody("text/html", messageStr)
	mockSender := gomail.SendFunc(g.Send)
	if err = mockSender.Send("tester@alerts.com", []string{email}, m); err != nil {
		return fmt.Errorf("mockSender send: %v", err)
	}
	return nil
}

func (g *TestingGomail) Send(from string, to []string, msg io.WriterTo) error {
	return nil
}

func (g *TestingGomail) GetUserInboxes() Schema {
	return g.inboxes
}

type GomailOpts struct {
	SmtpServer string `mapstructure:"smtpServer" validate:"required"`
	SmtpPort   int    `mapstructure:"smtpPort"`
	Username   string `mapstructure:"username"`
	Password   string `mapstructure:"password"`
}

type Gomail struct {
	dialer   *gomail.Dialer
	username string
}

func NewGomail(opts GomailOpts) (Notifier, error) {
	d := gomail.NewDialer(opts.SmtpServer, opts.SmtpPort, opts.Username, opts.Password)
	return &Gomail{
		dialer:   d,
		username: opts.Username,
	}, nil
}

func (g *Gomail) Close() error {
	return nil
}

func (g *Gomail) sendEmails(message Message, emails ...string) error {
	var emailParts []string
	var username, messageStr string
	sender, err := g.dialer.Dial()
	if err != nil {
		return err
	}
	defer sender.Close()
	var m *gomail.Message
	for _, email := range emails {
		emailParts = strings.Split(email, "@")
		username = emailParts[0]
		m = gomail.NewMessage()
		m.SetHeader("From", g.username)
		m.SetHeader("To", email)
		m.SetHeader("Subject", "DataFarm Alert Notification")
		messageStr, err = message.String(username)
		if err != nil {
			break
		}
		m.SetBody("text/html", messageStr)
		if err = sender.Send(g.username, []string{email}, m); err != nil {
			break
		}
	}
	return err
}

func (g *Gomail) AlertNotify(message AlertNotification, emails ...string) error {
	return g.sendEmails(message, emails...)
}

func (g *Gomail) AlertCreatedNotify(message AlertCreated, emails ...string) error {
	return g.sendEmails(message, emails...)
}

func (g *Gomail) AlertDeletedNotify(message AlertDeleted, emails ...string) error {
	return g.sendEmails(message, emails...)
}

func (g *Gomail) GetUserInboxes() Schema {
	return Schema{}
}
