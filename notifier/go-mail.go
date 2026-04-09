package notifier

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/geraud22/sensordata-alerts/auth"
	"github.com/geraud22/sensordata-alerts/templates"
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
			DeletedAlerts: make(map[string][]AlertDeleted),
		},
	}, nil
}

func (g *TestingGomail) Close() error {
	return nil
}

func (g *TestingGomail) Notify(
	message Message, tmpl templates.Templater, users ...auth.UserInfo) error {
	var err error
	var emailParts []string
	var username, html string
	var userInfo auth.UserInfo
	for _, u := range users {
		emailParts = strings.Split(u.EmailAddress, "@")
		username = emailParts[0]
		userInfo = auth.UserInfo{Username: username, EmailAddress: u.EmailAddress}
		switch t := message.(type) {
		case AlertNotification:
			g.inboxes.Notifications[u.EmailAddress] = append(
				g.inboxes.Notifications[u.EmailAddress], t)
			log.Printf("received notification:\n %v", t)
			html, err = tmpl.Render(templates.AlertNotificationTempl, struct {
				Message  AlertNotification
				UserInfo auth.UserInfo
			}{Message: t, UserInfo: userInfo})
		case AlertCreated:
			g.inboxes.CreatedAlerts[u.EmailAddress] = append(
				g.inboxes.CreatedAlerts[u.EmailAddress], t)
			log.Printf("created alert: %v\n", t)
			html, err = tmpl.Render(templates.AlertCreatedTempl, struct {
				Message  AlertCreated
				UserInfo auth.UserInfo
			}{Message: t, UserInfo: userInfo})
		case AlertUpdated:
			g.inboxes.CreatedAlerts[u.EmailAddress] = append(
				g.inboxes.CreatedAlerts[u.EmailAddress], AlertCreated(t))
			log.Printf("updated alert: %v\n", t)
			html, err = tmpl.Render(templates.AlertUpdatedTempl, struct {
				Message  AlertUpdated
				UserInfo auth.UserInfo
			}{Message: t, UserInfo: userInfo})
		case AlertDeleted:
			g.inboxes.DeletedAlerts[u.EmailAddress] = append(
				g.inboxes.DeletedAlerts[u.EmailAddress], t)
			log.Printf("deleted alert: %v\n", t)
			html, err = tmpl.Render(templates.AlertDeletedTempl, struct {
				Message  AlertDeleted
				UserInfo auth.UserInfo
			}{Message: t, UserInfo: userInfo})
		default:
			err = fmt.Errorf("message is unknown type: %T", t)
		}
		if err != nil {
			break
		}
		if err = g.sendEmail(html, u.EmailAddress); err != nil {
			break
		}
		if err = g.toFile(html); err != nil {
			break
		}
	}
	return err
}

func (g *TestingGomail) toFile(html string) error {
	if html == "" {
		return nil
	}
	return os.WriteFile("example-email.html", []byte(html), 0644)
}

func (g *TestingGomail) sendEmail(html, email string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", "tester@alerts.com")
	m.SetHeader("Subject", "DataFarm Alert Notification")
	m.SetBody("text/html", html)
	mockSender := gomail.SendFunc(g.Send)
	if err := mockSender.Send("tester@alerts.com", []string{email}, m); err != nil {
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

func (g *Gomail) sendEmails(message Message, templater templates.Templater,
	users ...auth.UserInfo) error {
	sender, err := g.dialer.Dial()
	if err != nil {
		return err
	}
	defer sender.Close()
	var tmplName templates.TemplateName
	var tmplContent any
	var emailParts []string
	var html string
	var m *gomail.Message
	for _, u := range users {
		emailParts = strings.Split(u.EmailAddress, "@")
		u.Username = emailParts[0]
		switch t := message.(type) {
		case AlertNotification:
			tmplName = templates.AlertNotificationTempl
			tmplContent = struct {
				Message  AlertNotification
				UserInfo auth.UserInfo
			}{Message: t, UserInfo: u}
		case AlertCreated:
			tmplName = templates.AlertCreatedTempl
			tmplContent = struct {
				Message  AlertCreated
				UserInfo auth.UserInfo
			}{Message: t, UserInfo: u}
		case AlertUpdated:
			tmplName = templates.AlertUpdatedTempl
			tmplContent = struct {
				Message  AlertUpdated
				UserInfo auth.UserInfo
			}{Message: t, UserInfo: u}
		case AlertDeleted:
			tmplName = templates.AlertDeletedTempl
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
		m = gomail.NewMessage()
		m.SetHeader("From", g.username)
		m.SetHeader("To", u.EmailAddress)
		m.SetHeader("Subject", "DataFarm Alert Notification")
		m.SetBody("text/html", html)
		if err = sender.Send(g.username, []string{u.EmailAddress}, m); err != nil {
			break
		}
	}
	return err
}

func (g *Gomail) Notify(message Message, templater templates.Templater,
	users ...auth.UserInfo) error {
	if templater == nil {
		return fmt.Errorf("received nil templater")
	}
	if len(users) < 1 {
		return fmt.Errorf("no users to notify")
	}
	return g.sendEmails(message, templater, users...)
}

func (g *Gomail) GetUserInboxes() Schema {
	return Schema{}
}
