package notifier

type Notifier interface {
	Notify(users []string, message string) error
}
