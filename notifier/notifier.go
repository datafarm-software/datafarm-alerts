package notifier

import (
	gomail "github.com/fawwazid/go-mail"
)

type Notifier interface {
	gomail.Mailer
	Notify(emails []string, message string) error
}
