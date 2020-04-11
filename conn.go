package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/gofrs/uuid"
	"github.com/gorilla/websocket"
)

//GenerateSubscriptionID is a function that returns unique IDs used to track subscriptions.
//By default UUIDv4's are used
var GenerateSubscriptionID func() string = func() string {
	return uuid.Must(uuid.NewV4()).String()
}

//Conn is a connection to a GraphQL WebSocket endpoint
type Conn struct {
	conn  *websocket.Conn
	debug bool

	subscriptions map[string]func(message *Message)
	mu            *sync.RWMutex
}

func (c *Conn) reader() {
	for {
		msg := new(Message)
		if err := c.conn.ReadJSON(msg); err != nil && c.debug {
			log.Println("DEBUG: Unable to parse Message:", err)
			continue
		}

		if msg.Type == MessageTypeConnectionKeepAlive {
			continue
		}

		c.mu.RLock()
		if f, ok := c.subscriptions[msg.ID]; !ok {
			if c.debug {
				fmt.Println("DEBUG: Message received for unknown subscription:", msg.ID)
			}
		} else {
			go f(msg)
		}
		c.mu.RUnlock()

		if msg.Type == MessageTypeComplete && msg.ID != "" {
			c.mu.Lock()
			delete(c.subscriptions, msg.ID)
			c.mu.Unlock()
		}

		if msg.Type != MessageTypeComplete && msg.Type != MessageTypeData && c.debug {
			fmt.Println("DEBUG: Received unexpected Message with type:", msg.Type)
		}
	}
}

func (c *Conn) init(connectionParams *MessagePayloadConnectionInit) error {
	msg := &Message{Type: MessageTypeConnectionInit}
	if err := msg.SetPayload(connectionParams); err != nil {
		return fmt.Errorf("Unable to marshal connectionParams: %v", err)
	}

	err := c.conn.WriteJSON(msg)
	if err != nil {
		return fmt.Errorf("Unable to write %s message: %v", MessageTypeConnectionInit, err)
	}

	for {
		msg := new(Message)
		err = c.conn.ReadJSON(msg)
		if err != nil {
			return fmt.Errorf("Unable to parse message: %v", err)
		}
		switch msg.Type {
		case MessageTypeConnectionAck:
			return nil
		case MessageTypeConnectionKeepAlive:
			continue
		case MessageTypeConnectionError:
			return ParseError(msg.Payload)
		default:
			return fmt.Errorf("Unexpected message type: %s", msg.Type)
		}
	}
}

//Close closes the Conn or returns an error if one occurred
func (c *Conn) Close() error {
	err := c.conn.WriteJSON(&Message{Type: MessageTypeConnectionTerminate})
	if err != nil {
		return fmt.Errorf("Unable to write %s message: %v", MessageTypeConnectionTerminate, err)
	}

	err = c.conn.Close()
	if err != nil {
		return fmt.Errorf("Unable to close websocket connection: %v", err)
	}

	return nil
}

//Subscribe creates a GraphQL subscription with the given payload and returns its ID, or returns an error if one occurred.
//Subscription Messages are passed to the given function handler as they are received
func (c *Conn) Subscribe(payload *MessagePayloadStart, f func(message *Message)) (id string, err error) {
	id = GenerateSubscriptionID()

	m := &Message{Type: MessageTypeStart, ID: id}
	if err := m.SetPayload(payload); err != nil {
		return "", fmt.Errorf("Unable to marshal payload: %v", err)
	}

	c.mu.Lock()
	c.subscriptions[id] = f
	c.mu.Unlock()

	if err := c.conn.WriteJSON(m); err != nil {
		c.mu.Lock()
		delete(c.subscriptions, id)
		c.mu.Unlock()
		return "", fmt.Errorf("Unable to write %s message: %v", MessageTypeStart, err)
	}

	return id, nil
}

//Unsubscribe stops the subscription with the given ID or returns an error if one occurred
func (c *Conn) Unsubscribe(id string) error {
	m := &Message{Type: MessageTypeStop, ID: id}

	if err := c.conn.WriteJSON(m); err != nil {
		return fmt.Errorf("Unable to write %s message: %v", MessageTypeStop, err)
	}

	c.mu.Lock()
	delete(c.subscriptions, id)
	c.mu.Unlock()

	return nil
}

//Execute executes the given payload and returns the result or an error if one occurred
//The given context can be used to cancel the request
func (c *Conn) Execute(ctx context.Context, payload *MessagePayloadStart) (data *MessagePayloadData, err error) {
	ch := make(chan *Message)
	id, err := c.Subscribe(payload, func(message *Message) {
		ch <- message
	})
	if err != nil {
		return nil, fmt.Errorf("Unable to subscribe: %v", err)
	}

	defer func() {
		if uErr := c.Unsubscribe(id); err == nil && uErr != nil {
			err = fmt.Errorf("Unable to unsubscribe: %v", uErr)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case msg := <-ch:
			switch msg.Type {
			case MessageTypeComplete:
				continue
			case MessageTypeData:
				d := new(MessagePayloadData)
				if err = json.Unmarshal(msg.Payload, d); err != nil {
					return nil, fmt.Errorf("Unable to unmarshal %s message payload: %v", MessageTypeData, err)
				}
				return d, nil
			case MessageTypeError:
				return nil, ParseError(msg.Payload)
			default:
				return nil, fmt.Errorf("Unexpected message type: %s", msg.Type)
			}
		}
	}
}