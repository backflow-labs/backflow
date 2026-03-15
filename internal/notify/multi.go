package notify

import "errors"

// MultiNotifier fans out events to multiple notifiers.
// All notifiers are called even if some fail; errors are joined.
type MultiNotifier struct {
	notifiers []Notifier
}

func NewMultiNotifier(notifiers ...Notifier) *MultiNotifier {
	return &MultiNotifier{notifiers: notifiers}
}

func (m *MultiNotifier) Notify(event Event) error {
	var errs []error
	for _, n := range m.notifiers {
		if err := n.Notify(event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
