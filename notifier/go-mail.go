package notifier

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/geraud22/sensordata-alerts/alertstore"
	"github.com/geraud22/sensordata-alerts/auth"
	"github.com/geraud22/sensordata-alerts/templates"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gopkg.in/gomail.v2"
)

type TestingGomail struct {
	inboxes Schema
}

func NewTestingGomail() (Notifier, error) {
	return &TestingGomail{
		inboxes: Schema{
			Notifications: make(map[string][]AlertNotification),
			CreatedAlerts: make(map[string][]AlertCreated),
			DeletedAlerts: make(map[string][]AlertDeleted),
		},
	}, nil
}

func (g *TestingGomail) Close() error {
	return nil
}

func (g *TestingGomail) Notify(
	message Message, tmpl templates.Templater, users ...auth.UserInfo) error {
	log.Printf("notifying message %T: %v", message, message)
	mockSender := gomail.SendFunc(
		func(from string, to []string, msg io.WriterTo) error {
			return nil
		})
	emails, err := generateEmails(message, tmpl, users...)
	if err != nil {
		return fmt.Errorf("generating emails: %v", err)
	}
	var m *gomail.Message
	var v AlertNotification
	var ok bool
	for email, html := range emails {
		m = newMessage("tester@alerts.com", email, "Test Alert Notification", html)
		if err = mockSender.Send("tester@alerts.com", []string{email}, m); err != nil {
			err = fmt.Errorf("mockSender send: %v", err)
			break
		}
		if v, ok = message.(AlertNotification); ok {
			switch v.AlertLevel {
			case alertstore.Crit:
				m.SetHeader("X-Priority", "1")
				m.SetHeader("Priority", "urgent")
				m.SetHeader("Importance", "high")
			default:
			}
		}
		if err = g.toFile(html); err != nil {
			err = fmt.Errorf("toHtml: %v", err)
			break
		}
	}
	if err != nil {
		return err
	}
	return nil
}

func (g *TestingGomail) toFile(html string) error {
	if html == "" {
		return nil
	}
	return os.WriteFile("example-email.html", []byte(html), 0644)
}

const logoCID = "email-logo.png"

func newMessage(from, to, subject, html string) *gomail.Message {
	m := gomail.NewMessage()
	m.SetHeader("From", from)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", html)
	m.Embed("templates/assets/email-logo.png",
		gomail.SetHeader(map[string][]string{
			"Content-ID": {"<" + logoCID + ">"},
		}),
	)
	return m
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

func generateEmails(
	message Message, templater templates.Templater, users ...auth.UserInfo) (
	emails map[string]string, err error) {
	emails = make(map[string]string, len(users))
	var tmplName templates.TemplateName
	var tmplContent any
	var emailParts []string
	var html string
	for _, u := range users {
		emailParts = strings.Split(u.EmailAddress, "@")
		u.Username = cases.Title(language.English).String(emailParts[0])
		switch t := message.(type) {
		case AlertNotification:
			tmplName = templates.AlertNotificationTempl
			tmplContent = struct {
				Message  AlertNotification
				UserInfo auth.UserInfo
			}{Message: t, UserInfo: u}
		case AlertCreated:
			tmplName = templates.AlertCreatedTempl
			t.Condition, err = formatAlertCondition(t.AlertType, t.AlertValues)
			if err != nil {
				break
			}
			tmplContent = struct {
				Message  AlertCreated
				UserInfo auth.UserInfo
			}{Message: t, UserInfo: u}
		case AlertUpdated:
			tmplName = templates.AlertUpdatedTempl
			t.Condition, err = formatAlertCondition(t.AlertType, t.AlertValues)
			if err != nil {
				break
			}
			tmplContent = struct {
				Message  AlertUpdated
				UserInfo auth.UserInfo
			}{Message: t, UserInfo: u}
		case AlertDeleted:
			tmplName = templates.AlertDeletedTempl
			t.Condition, err = formatAlertCondition(t.AlertType, t.AlertValues)
			if err != nil {
				break
			}
			tmplContent = struct {
				Message  AlertDeleted
				UserInfo auth.UserInfo
			}{Message: t, UserInfo: u}
		default:
			err = fmt.Errorf("unknown message type: %v", t)
		}
		if err != nil {
			break
		}
		html, err = templater.Render(tmplName, tmplContent)
		if err != nil {
			err = fmt.Errorf("rendering: %s, error: %v", tmplName, err)
			break
		}
		emails[u.EmailAddress] = html
	}
	return emails, err
}

func (g *Gomail) Notify(message Message, templater templates.Templater,
	users ...auth.UserInfo) error {
	if templater == nil {
		return fmt.Errorf("received nil templater")
	}
	if len(users) < 1 {
		return fmt.Errorf("no users to notify")
	}
	sender, err := g.dialer.Dial()
	if err != nil {
		return err
	}
	defer sender.Close()
	emails, err := generateEmails(message, templater, users...)
	if err != nil {
		return fmt.Errorf("generating emails: %v", err)
	}
	var m *gomail.Message
	var v AlertNotification
	var ok bool
	for email, html := range emails {
		m = newMessage(g.username, email, "DataFarm Alert Notification", html)
		if err = sender.Send(g.username, []string{email}, m); err != nil {
			err = fmt.Errorf("mockSender send: %v", err)
			break
		}
		if v, ok = message.(AlertNotification); ok {
			switch v.AlertLevel {
			case alertstore.Crit:
				m.SetHeader("X-Priority", "1")
				m.SetHeader("Priority", "urgent")
				m.SetHeader("Importance", "high")
			default:
			}
		}
	}
	if err != nil {
		return fmt.Errorf("messaging loop: %v", err)
	}
	return nil
}

func (g *Gomail) GetUserInboxes() Schema {
	return Schema{}
}
