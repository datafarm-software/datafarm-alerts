package notifier

type Notifier interface {
	Notify(emails []string, message string) error
}
