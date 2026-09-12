package storage

import (
	"context"

	"e-commerce-backend/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CreateOrderMessageInDB saves one chat message.
//
// The message is written to the database before it is
// pushed to anybody's socket. That order matters: if the
// broadcast happened first and the write failed, people
// would see a message that does not exist, and it would
// vanish the moment they refreshed.
func CreateOrderMessageInDB(
	pool *pgxpool.Pool,
	orderID int,
	senderID int,
	body string,
) (models.OrderMessage, error) {

	ctx := context.Background()

	var message models.OrderMessage

	// The sender's name and role are joined from the
	// users table so a chat window can label the message
	// straight away.
	err := pool.QueryRow(
		ctx,
		`
		WITH inserted AS (
			INSERT INTO order_messages
				(order_id, sender_id, body)
			VALUES
				($1, $2, $3)
			RETURNING
				id,
				order_id,
				sender_id,
				body,
				created_at
		)
		SELECT
			i.id,
			i.order_id,
			i.sender_id,
			u.name AS sender_name,
			u.role AS sender_role,
			i.body,
			i.created_at
		FROM inserted i
		JOIN users u ON u.id = i.sender_id
		`,
		orderID,
		senderID,
		body,
	).Scan(
		&message.ID,
		&message.OrderID,
		&message.SenderID,
		&message.SenderName,
		&message.SenderRole,
		&message.Body,
		&message.CreatedAt,
	)

	if err != nil {
		return models.OrderMessage{}, err
	}

	return message, nil
}

// ListOrderMessagesFromDB returns the full conversation on
// one order, oldest first.
//
// This is what a client receives when it connects, so that
// reopening a chat shows everything that was said while it
// was closed.
func ListOrderMessagesFromDB(
	pool *pgxpool.Pool,
	orderID int,
) ([]models.OrderMessage, error) {

	rows, err := pool.Query(
		context.Background(),
		`
		SELECT
			m.id,
			m.order_id,
			m.sender_id,
			u.name AS sender_name,
			u.role AS sender_role,
			m.body,
			m.created_at
		FROM order_messages m
		JOIN users u ON u.id = m.sender_id
		WHERE m.order_id = $1
		ORDER BY m.created_at, m.id
		`,
		orderID,
	)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	messages := make([]models.OrderMessage, 0)

	for rows.Next() {

		var message models.OrderMessage

		err := rows.Scan(
			&message.ID,
			&message.OrderID,
			&message.SenderID,
			&message.SenderName,
			&message.SenderRole,
			&message.Body,
			&message.CreatedAt,
		)

		if err != nil {
			return nil, err
		}

		messages = append(messages, message)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return messages, nil
}

// CanUserAccessOrderChatFromDB reports whether a user is
// allowed into an order's chat room.
//
// Only three kinds of people may join: the buyer, a seller
// who has an item on the order, and an admin. Anyone else
// is a stranger, and an order conversation is private.
//
// It returns the user's role as well, because the chat
// room uses it to label messages.
func CanUserAccessOrderChatFromDB(
	pool *pgxpool.Pool,
	orderID int,
	userID int,
) (bool, error) {

	var allowed bool

	err := pool.QueryRow(
		context.Background(),
		`
		SELECT EXISTS (
			SELECT 1
			FROM orders o
			WHERE o.id = $1
			AND (
				o.buyer_id = $2
				OR EXISTS (
					SELECT 1
					FROM order_items oi
					WHERE oi.order_id = o.id
					AND oi.seller_id = $2
				)
				OR EXISTS (
					SELECT 1
					FROM users u
					WHERE u.id = $2
					AND u.role = 'admin'
				)
			)
		)
		`,
		orderID,
		userID,
	).Scan(&allowed)

	if err != nil {
		return false, err
	}

	return allowed, nil
}
