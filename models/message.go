package models

import "time"

// OrderMessage is one chat message on an order.
//
// Messages are stored in the database and also pushed live
// over a WebSocket. The database copy is what makes the
// conversation survive a dropped connection.
type OrderMessage struct {

	ID int `json:"id"`

	OrderID int `json:"order_id"`

	SenderID int `json:"sender_id"`

	// SenderName is joined in so a chat window can label
	// each message without another lookup.
	SenderName string `json:"sender_name"`

	// SenderRole is useful in a chat window, because a
	// message from the seller reads differently from one
	// from the buyer.
	SenderRole string `json:"sender_role"`

	Body string `json:"body"`

	CreatedAt time.Time `json:"created_at"`
}

// ChatInboundMessage is what a client sends over the
// socket.
type ChatInboundMessage struct {

	// Body is the text of the message.
	Body string `json:"body"`
}

// ChatEnvelope is what the server pushes to everyone in a
// chat room.
//
// It carries a type so that a client can tell an ordinary
// message apart from a notice that the order changed
// status, which is what makes this live order-status chat
// rather than a plain message box.
type ChatEnvelope struct {

	// Type is "message" or "order_status".
	Type string `json:"type"`

	// Message is set when Type is "message".
	Message *OrderMessage `json:"message,omitempty"`

	// Status is set when Type is "order_status".
	Status string `json:"status,omitempty"`

	// Note explains a status change.
	Note string `json:"note,omitempty"`
}

// The kinds of envelope the chat room broadcasts.
const (
	ChatEnvelopeMessage     = "message"
	ChatEnvelopeOrderStatus = "order_status"
)
