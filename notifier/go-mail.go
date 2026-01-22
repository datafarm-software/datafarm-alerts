package notifier

import (
	"io"
	"log"
	"strings"

	"gopkg.in/gomail.v2"
)

type TestingGomail struct {
	testingInboxes map[string][]AlertMessage
}

func NewTestingGoMail() (Notifier, error) {
	return &TestingGomail{
		testingInboxes: make(map[string][]AlertMessage),
	}, nil
}

func (g *TestingGomail) Notify(message AlertMessage, emails ...string) error {
	var emailParts []string
	var username string
	var err error
	var mockSender gomail.SendFunc
	var m *gomail.Message
	for _, email := range emails {
		emailParts = strings.Split(email, "@")
		username = emailParts[0]
		m = gomail.NewMessage()
		m.SetHeader("From", "tester@alerts.com")
		m.SetHeader("Subject", "DataFarm Alert Notification")
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

type GomailOpts struct {
	SmtpServer string `mapstructure:"smtpServer" validate:"required"`
	SmtpPort   int    `mapstructure:"stmpPort"`
	Username   string `mapstructure:"username"`
	Password   string `mapstructure:"port"`
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

func (g *Gomail) Notify(message AlertMessage, emails ...string) error {
	var emailParts []string
	var username string
	var err error
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
		m.SetBody("text/html", message.ToHtml(username))
		if err = sender.Send(g.username, []string{email}, m); err != nil {
			break
		}
		log.Println("sent to: ", email)
	}
	return err
}

func (g *Gomail) GetUserInboxes() map[string][]AlertMessage {
	return nil
}
