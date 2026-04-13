package notifier

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/geraud22/sensordata-alerts/alertstore"
	"github.com/geraud22/sensordata-alerts/auth"
	"github.com/geraud22/sensordata-alerts/templates"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gopkg.in/gomail.v2"
)

const logoCID = "email-logo.png"

func newMessage(from, to string, email Email) *gomail.Message {
	m := gomail.NewMessage()
	m.SetHeader("From", from)
	m.SetHeader("To", to)
	m.SetHeader("Subject", email.subject)
	m.SetBody("text/html", email.html)
	m.Embed("templates/assets/email-logo.png",
		gomail.SetHeader(map[string][]string{
			"Content-ID": {"<" + logoCID + ">"},
		}),
	)
	return m
}

type GomailOpts struct {
	SmtpServer string `mapstructure:"smtpServer" validate:"required"`
	SmtpPort   int    `mapstructure:"smtpPort"`
	Username   string `mapstructure:"username"`
	Password   string `mapstructure:"password"`
}

type Gomail struct {
	dialer   *gomail.Dialer
	mode     alertstore.Mode
	inboxes  Schema
	username string
}

func NewGomail(mode alertstore.Mode, opts GomailOpts) (Notifier, error) {
	d := gomail.NewDialer(opts.SmtpServer, opts.SmtpPort, opts.Username, opts.Password)
	g := Gomail{
		dialer:   d,
		username: opts.Username,
		mode:     mode,
	}
	if mode == alertstore.Testing {
		g.inboxes = Schema{
			Notifications: make(map[string][]AlertNotification),
			CreatedAlerts: make(map[string][]AlertCreated),
			DeletedAlerts: make(map[string][]AlertDeleted),
		}
	}
	return &g, nil
}

func (g *Gomail) Close() error {
	return nil
}

type Email struct {
	subject string
	html    string
}

func (g *Gomail) generateEmails(
	message Message, templater templates.Templater, users ...auth.UserInfo) (
	emails map[string]Email, err error) {
	emails = make(map[string]Email, len(users))
	var tmplName templates.TemplateName
	var tmplContent any
	var emailParts []string
	var email Email
	for _, u := range users {
		emailParts = strings.Split(u.EmailAddress, "@")
		u.Username = cases.Title(language.English).String(emailParts[0])
		switch t := message.(type) {
		case AlertNotification:
			email.subject = fmt.Sprintf("%s, Level: %s",
				t.Name, alertstore.AlertLevelString[t.AlertLevel])
			tmplName = templates.AlertNotificationTempl
			t.Condition, err = formatAlertCondition(t.AlertType, t.AlertValues)
			if err != nil {
				break
			}
			tmplContent = struct {
				Message  AlertNotification
				UserInfo auth.UserInfo
			}{Message: t, UserInfo: u}
			if g.mode == alertstore.Testing {
				g.inboxes.Notifications[u.EmailAddress] = append(
					g.inboxes.Notifications[u.EmailAddress], t)
			}
		case AlertCreated:
			email.subject = "DataFarm Alert Created"
			tmplName = templates.AlertCreatedTempl
			t.Condition, err = formatAlertCondition(t.AlertType, t.AlertValues)
			if err != nil {
				break
			}
			tmplContent = struct {
				Message  AlertCreated
				UserInfo auth.UserInfo
			}{Message: t, UserInfo: u}
			if g.mode == alertstore.Testing {
				g.inboxes.CreatedAlerts[u.EmailAddress] = append(
					g.inboxes.CreatedAlerts[u.EmailAddress], t)
			}
		case AlertUpdated:
			email.subject = "DataFarm Alert Updated"
			tmplName = templates.AlertUpdatedTempl
			t.Condition, err = formatAlertCondition(t.AlertType, t.AlertValues)
			if err != nil {
				break
			}
			tmplContent = struct {
				Message  AlertUpdated
				UserInfo auth.UserInfo
			}{Message: t, UserInfo: u}
			if g.mode == alertstore.Testing {
				g.inboxes.CreatedAlerts[u.EmailAddress] = append(
					g.inboxes.CreatedAlerts[u.EmailAddress], AlertCreated(t))
			}
		case AlertDeleted:
			email.subject = "DataFarm Alert Deleted"
			tmplName = templates.AlertDeletedTempl
			t.Condition, err = formatAlertCondition(t.AlertType, t.AlertValues)
			if err != nil {
				break
			}
			tmplContent = struct {
				Message  AlertDeleted
				UserInfo auth.UserInfo
			}{Message: t, UserInfo: u}
			if g.mode == alertstore.Testing {
				g.inboxes.DeletedAlerts[u.EmailAddress] = append(
					g.inboxes.DeletedAlerts[u.EmailAddress], t)
			}
		default:
			err = fmt.Errorf("unknown message type: %v", t)
		}
		if err != nil {
			break
		}
		email.html, err = templater.Render(tmplName, tmplContent)
		if err != nil {
			err = fmt.Errorf("rendering: %s, error: %v", tmplName, err)
			break
		}
		emails[u.EmailAddress] = email
	}
	return emails, err
}

type mockSender struct{}

func (m *mockSender) Send(from string, to []string, msg io.WriterTo) error {
	return nil
}

func (m *mockSender) Close() error { return nil }

func (g *Gomail) Notify(message Message, templater templates.Templater,
	users ...auth.UserInfo) (err error) {
	if templater == nil {
		return fmt.Errorf("received nil templater")
	}
	if len(users) < 1 {
		return fmt.Errorf("no users to notify")
	}
	var sender gomail.SendCloser
	if g.mode == alertstore.Testing {
		sender = &mockSender{}
	} else {
		sender, err = g.dialer.Dial()
		if err != nil {
			return err
		}
	}
	defer sender.Close()
	emails, err := g.generateEmails(message, templater, users...)
	if err != nil {
		return fmt.Errorf("generating emails: %v", err)
	}
	var m *gomail.Message
	var v AlertNotification
	var ok bool
	for recipient, email := range emails {
		m = newMessage(g.username, recipient, email)
		if v, ok = message.(AlertNotification); ok {
			switch v.AlertLevel {
			case alertstore.Crit:
				m.SetHeader("X-Priority", "1")
				m.SetHeader("Priority", "urgent")
				m.SetHeader("Importance", "high")
			}
		}
		if err = sender.Send(g.username, []string{recipient}, m); err != nil {
			err = fmt.Errorf("mockSender send: %v", err)
			break
		}
		if g.mode == alertstore.Testing {
			if err = g.toFile(email.html); err != nil {
				err = fmt.Errorf("toHtml: %v", err)
				break
			}
		}
	}
	if err != nil {
		return fmt.Errorf("messaging loop: %v", err)
	}
	return nil
}

func (g *Gomail) toFile(html string) error {
	if html == "" {
		return nil
	}
	return os.WriteFile("example-email.html", []byte(html), 0644)
}

func (g *Gomail) GetUserInboxes() Schema {
	return g.inboxes
}
