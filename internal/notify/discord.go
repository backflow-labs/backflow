package notify

import "github.com/rs/zerolog/log"

// DiscordNotifier is a placeholder that will be replaced with real Discord
// message delivery in a future issue. It logs events but does not send them.
type DiscordNotifier struct {
	events map[EventType]bool
}

func NewDiscordNotifier(filterEvents []string) *DiscordNotifier {
	d := &DiscordNotifier{}
	if len(filterEvents) > 0 {
		d.events = make(map[EventType]bool, len(filterEvents))
		for _, e := range filterEvents {
			d.events[EventType(e)] = true
		}
	}
	return d
}

func (d *DiscordNotifier) Notify(event Event) error {
	if d.events != nil && !d.events[event.Type] {
		return nil
	}
	log.Debug().Str("event", string(event.Type)).Str("task_id", event.TaskID).Msg("discord: notification stub (not yet delivered)")
	return nil
}

func (d *DiscordNotifier) Name() string { return "discord" }
