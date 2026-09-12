package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"e-commerce-backend/models"
	"e-commerce-backend/services"
	"e-commerce-backend/storage"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

// How long the server waits to hear anything at all from
// a socket before deciding it is dead.
//
// A browser that loses its network connection leaves a
// socket that is open as far as the server can tell, so
// without a deadline these would accumulate. The client is
// expected to answer pings, which is what keeps the
// deadline moving.
const (
	chatPongWait = 60 * time.Second

	chatPingPeriod = 50 * time.Second

	// chatWriteWait bounds how long a single frame may
	// take to write, so one stuck client cannot hold a
	// goroutine forever.
	chatWriteWait = 10 * time.Second

	// chatMaxMessageBytes caps an incoming chat message.
	// A chat message is a sentence or two, not a file.
	chatMaxMessageBytes = 4096
)

// OrderChatHandler upgrades a request into a WebSocket
// connection to an order's chat room.
//
// Only the buyer, a seller with an item on the order, and
// admins may connect, using exactly the same rule as every
// other order endpoint. An order conversation is private.
//
// The room carries both ordinary messages and order status
// changes, which is what makes this live order-status chat
// rather than a plain message box.
func OrderChatHandler(
	pool *pgxpool.Pool,
	hub *services.Hub,
	allowedOrigins []string,
) http.HandlerFunc {

	upgrader := websocket.Upgrader{

		ReadBufferSize: 1024,

		WriteBufferSize: 1024,

		// The origin check is configurable because this
		// project is API only and the browser client
		// does not exist yet. An empty list means "allow
		// any origin", which is fine for local work over
		// curl but must be narrowed before this is
		// exposed to the internet: a WebSocket is not
		// covered by the same-origin policy, so without
		// this check any website could open a socket to
		// this server using a signed-in visitor's
		// cookies.
		CheckOrigin: func(r *http.Request) bool {

			if len(allowedOrigins) == 0 {
				return true
			}

			origin := r.Header.Get("Origin")

			if origin == "" {
				return true
			}

			for _, allowed := range allowedOrigins {

				if strings.EqualFold(origin, allowed) {
					return true
				}
			}

			return false
		},
	}

	return func(w http.ResponseWriter, r *http.Request) {

		user, ok := requireUser(pool, w, r)

		if !ok {
			return
		}

		orderID, ok := pathID(w, r, "id")

		if !ok {
			return
		}

		allowed, err :=
			storage.CanUserAccessOrderChatFromDB(
				pool,
				orderID,
				user.ID,
			)

		if err != nil {

			writeError(
				w,
				http.StatusInternalServerError,
				"Could not check access to this order",
			)

			return
		}

		if !allowed {

			writeError(
				w,
				http.StatusForbidden,
				"You are not part of this order",
			)

			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)

		if err != nil {

			// Upgrade has already written a response by
			// the time it fails, so there is nothing to
			// add here.
			return
		}

		client := services.NewClient(
			conn,
			orderID,
			user.ID,
		)

		// Join before reading the history, so a message
		// that arrives while the history is being read
		// is queued in the client's buffer rather than
		// lost. It is then written after the history,
		// which keeps the conversation in order.
		hub.Join(client)

		defer hub.Leave(client)

		defer conn.Close()

		err = sendChatHistory(
			pool,
			conn,
			orderID,
		)

		if err != nil {
			return
		}

		// Start the writer before the read loop, because
		// the read loop is what blocks this goroutine.
		go chatWriteLoop(client, conn)

		chatReadLoop(pool, hub, client, conn, user)
	}
}

// sendChatHistory writes everything said so far straight
// to the connection.
//
// This runs before the writer goroutine starts, so this
// goroutine is still the only one writing to the socket.
// Only one goroutine may write to a WebSocket at a time,
// and respecting that here is what keeps the frames from
// interleaving.
func sendChatHistory(
	pool *pgxpool.Pool,
	conn *websocket.Conn,
	orderID int,
) error {

	messages, err := storage.ListOrderMessagesFromDB(
		pool,
		orderID,
	)

	if err != nil {
		return err
	}

	conn.SetWriteDeadline(
		time.Now().Add(chatWriteWait),
	)

	for _, message := range messages {

		envelope := models.ChatEnvelope{
			Type: models.ChatEnvelopeMessage,

			Message: &message,
		}

		err := conn.WriteJSON(envelope)

		if err != nil {
			return err
		}
	}

	return nil
}

// chatWriteLoop sends queued frames to one connection.
//
// It is the only goroutine that writes ordinary messages
// to this socket, which is what the single-writer rule
// requires. It ends when the send channel is closed or a
// write fails.
func chatWriteLoop(
	client *services.Client,
	conn *websocket.Conn,
) {

	ping := time.NewTicker(chatPingPeriod)

	defer ping.Stop()

	for {

		select {

		case payload, open := <-client.Send():

			if !open {
				return
			}

			conn.SetWriteDeadline(
				time.Now().Add(chatWriteWait),
			)

			err := conn.WriteMessage(
				websocket.TextMessage,
				payload,
			)

			if err != nil {
				return
			}

		case <-ping.C:

			// WriteControl may be called alongside
			// another write, so this does not break
			// the single-writer rule.
			err := conn.WriteControl(
				websocket.PingMessage,
				nil,
				time.Now().Add(chatWriteWait),
			)

			if err != nil {
				return
			}
		}
	}
}

// chatReadLoop receives messages until the client goes
// away.
//
// Each message is written to the database before it is
// broadcast. That order matters: if the broadcast happened
// first and the write failed, everyone would see a message
// that does not exist and that would vanish on refresh.
func chatReadLoop(
	pool *pgxpool.Pool,
	hub *services.Hub,
	client *services.Client,
	conn *websocket.Conn,
	user models.User,
) {

	conn.SetReadLimit(chatMaxMessageBytes)

	conn.SetReadDeadline(
		time.Now().Add(chatPongWait),
	)

	// Every pong from the client pushes the deadline
	// back, so only a client that has genuinely stopped
	// answering is disconnected.
	conn.SetPongHandler(func(string) error {

		return conn.SetReadDeadline(
			time.Now().Add(chatPongWait),
		)
	})

	for {

		_, payload, err := conn.ReadMessage()

		if err != nil {
			return
		}

		var inbound models.ChatInboundMessage

		err = json.Unmarshal(payload, &inbound)

		if err != nil {

			writeChatNotice(
				conn,
				"That message could not be read",
			)

			continue
		}

		body := strings.TrimSpace(inbound.Body)

		if body == "" {
			continue
		}

		if len(body) > chatMaxMessageBytes {

			writeChatNotice(
				conn,
				"That message is too long",
			)

			continue
		}

		message, err := storage.CreateOrderMessageInDB(
			pool,
			client.OrderID,
			user.ID,
			body,
		)

		if err != nil {

			writeChatNotice(
				conn,
				"Your message could not be saved",
			)

			continue
		}

		hub.BroadcastMessage(
			client.OrderID,
			message,
		)
	}
}

// writeChatNotice sends a single note to one client.
//
// It is used for problems that concern only the sender, so
// it does not go through the hub and does not reach anyone
// else. The write deadline is set because the read loop's
// goroutine is the only writer at this moment.
func writeChatNotice(
	conn *websocket.Conn,
	text string,
) {

	conn.SetWriteDeadline(
		time.Now().Add(chatWriteWait),
	)

	conn.WriteJSON(
		map[string]string{
			"type": "error",

			"message": text,
		},
	)
}
