package messaging

import "context"

// ChannelType identifies the messaging transport.
type ChannelType string

const (
	ChannelSMS ChannelType = "sms"
)

// UnsubscribeFooter is appended on a new line to every outbound SMS — both
// REST-API sends and TwiML auto-replies — to satisfy 10DLC/TCPA compliance.
const UnsubscribeFooter = "Reply STOP to unsubscribe"

// Channel identifies a specific endpoint on a transport.
type Channel struct {
	Type    ChannelType `json:"type"`
	Address string      `json:"address"` // e.g. "+15551234567"
}

// OutboundMessage is a message to send via a channel.
type OutboundMessage struct {
	Channel Channel
	Body    string
}

// Messenger sends outbound messages.
type Messenger interface {
	Send(ctx context.Context, msg OutboundMessage) error
}

// NoopMessenger discards all messages.
type NoopMessenger struct{}

func (NoopMessenger) Send(context.Context, OutboundMessage) error { return nil }
