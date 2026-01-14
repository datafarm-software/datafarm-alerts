package notifier

import "fmt"

type Gomail struct{}

func NewGoMail() *Gomail {
	return &Gomail{}
}

func (g *Gomail) Notify(emails []string, message string) error {
	return fmt.Errorf("not implemented")
}
