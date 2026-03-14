package notify

// MultiNotifier fans out events to multiple notifiers.
type MultiNotifier struct {
	notifiers []Notifier
}

func NewMultiNotifier(notifiers ...Notifier) *MultiNotifier {
	return &MultiNotifier{notifiers: notifiers}
}

func (m *MultiNotifier) Notify(event Event) error {
	for _, n := range m.notifiers {
		if err := n.Notify(event); err != nil {
			return err
		}
	}
	return nil
}
