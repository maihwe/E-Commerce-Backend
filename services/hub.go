package services

import (
	"encoding/json"
	"sync"

	"e-commerce-backend/models"

	"github.com/gorilla/websocket"
)

// Hub keeps track of who is listening to which order's
// chat room.
//
// Each order has its own room, so a message on one order
// never reaches people watching a different one. The map
// is keyed by order ID.
type Hub struct {

	// rooms maps an order ID to the clients watching it.
	//
	// A set (map to struct{}) is used rather than a slice
	// so that joining twice or leaving twice cannot leave
	// a duplicate behind.
	rooms map[int]map[*Client]bool

	// mu protects rooms. HTTP handlers and WebSocket
	// reader goroutines all run concurrently, and a Go
	// map cannot be read and written at the same time.
	mu sync.RWMutex
}

// Client is one connected WebSocket.
type Client struct {

	// Conn is the underlying connection.
	Conn *websocket.Conn

	// OrderID is the room this client is watching.
	OrderID int

	// UserID is who is connected, used to label
	// messages.
	UserID int

	// send carries outgoing frames to the writer
	// goroutine.
	//
	// A channel is used so that only one goroutine ever
	// writes to a connection. A WebSocket takes one
	// writer at a time, and having several broadcasts
	// write directly would corrupt the stream.
	send chan []byte
}

// NewClient creates a client for a connection.
func NewClient(
	conn *websocket.Conn,
	orderID int,
	userID int,
) *Client {

	return &Client{
		Conn: conn,

		OrderID: orderID,

		UserID: userID,

		// The buffer is generous enough to hold a short
		// burst of messages if the reader stalls for a
		// moment.
		send: make(chan []byte, 32),
	}
}

// Send returns the channel of outgoing frames.
//
// The chat handler's write loop reads from it. It is
// exposed through a method rather than as a public field so
// that only the hub can put anything into it.
func (client *Client) Send() <-chan []byte {

	return client.send
}

// NewHub creates an empty hub.
func NewHub() *Hub {

	return &Hub{
		rooms: make(map[int]map[*Client]bool),
	}
}

// Join adds a client to an order's room.
func (hub *Hub) Join(client *Client) {

	hub.mu.Lock()
	defer hub.mu.Unlock()

	if hub.rooms[client.OrderID] == nil {
		hub.rooms[client.OrderID] = make(
			map[*Client]bool,
		)
	}

	hub.rooms[client.OrderID][client] = true
}

// Leave removes a client from its room.
//
// When the last client leaves, the room itself is deleted,
// so a long-running server does not slowly accumulate
// empty maps.
func (hub *Hub) Leave(client *Client) {

	hub.mu.Lock()
	defer hub.mu.Unlock()

	room, exists := hub.rooms[client.OrderID]

	if !exists {
		return
	}

	delete(room, client)

	if len(room) == 0 {
		delete(hub.rooms, client.OrderID)
	}
}

// Broadcast sends a payload to everyone in an order's
// room.
//
// If a client's buffer is full, because it is reading
// slowly or has stopped reading, the message is dropped
// rather than blocking. A chat message that cannot be
// delivered promptly is not worth stalling every other
// listener for, and because every message is also in the
// database, a client that reconnects still receives it.
func (hub *Hub) Broadcast(orderID int, payload []byte) {

	hub.mu.RLock()
	defer hub.mu.RUnlock()

	for client := range hub.rooms[orderID] {

		select {

		case client.send <- payload:

		default:
			// Buffer full, skip this client.
		}
	}
}

// BroadcastOrderStatus tells everyone in a room that the
// order changed status.
//
// This is what makes the chat live order-status chat
// rather than a plain message box: a buyer watching the
// conversation sees "shipped" appear the moment the seller
// ships it.
func (hub *Hub) BroadcastOrderStatus(
	orderID int,
	status string,
	note string,
) {

	envelope := models.ChatEnvelope{
		Type: models.ChatEnvelopeOrderStatus,

		Status: status,

		Note: note,
	}

	payload, err := json.Marshal(envelope)

	if err != nil {
		return
	}

	hub.Broadcast(orderID, payload)
}

// BroadcastMessage sends a chat message to a room.
func (hub *Hub) BroadcastMessage(
	orderID int,
	message models.OrderMessage,
) {

	envelope := models.ChatEnvelope{
		Type: models.ChatEnvelopeMessage,

		Message: &message,
	}

	payload, err := json.Marshal(envelope)

	if err != nil {
		return
	}

	hub.Broadcast(orderID, payload)
}

// RoomSize reports how many clients are watching an order.
//
// It is used by tests to wait for a connection to be
// registered before sending anything to it.
func (hub *Hub) RoomSize(orderID int) int {

	hub.mu.RLock()
	defer hub.mu.RUnlock()

	return len(hub.rooms[orderID])
}
