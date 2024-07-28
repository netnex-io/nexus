// The room package handles the management of a room within the server.
// Each room manages multiple connections and messages from a base to application-logic level.

package room

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/netnex-io/nexus/error"
	"github.com/netnex-io/nexus/pubsub"
	"github.com/netnex-io/nexus/room/messages"
)

// Manages a single room instance, handling connections and message routing.
type Room struct {
	Id              string                 // Unique identifier for the room
	RoomType        string                 // Type of the room, defined by the matchmaker's RoomType
	Connections     map[string]*Connection // Active connections within the room
	ConnectionLimit int                    // Maximum number of connections allowed to the room
	AutoDispose     int                    // Auto dispose timeout in seconds, 0 for no timeout
	LastActivity    time.Time              // Timestamp of the last activity in the room
	mutex           sync.Mutex             // Mutex for synchronizing access to room data

	// Messages is in charge of direct bidirectional communication, handling connection managers when a client joins/leaves the room
	messages chan RoomMessage
	// The pubsub is in charge of application logic (e.g room specific events, player movement, etc)
	pubsub *pubsub.PubSub

	// Optional hooks for room events
	OnJoin    func(c *Connection)                 // Called when a client joins the room
	OnLeave   func(c *Connection)                 // Called when a client leaves the room
	OnMessage func(c *Connection, message []byte) // Called when a client sends a message to the room

	disposeChan chan struct{} // Channel to signal room disposal
	disposed    bool          // Flag indicating if the room has been disposed
}

// Represents a single client connection within a room. Wraps over the underlying websocket connection.
type Connection struct {
	Id         string          // Unique identifier for the connection
	Connection *websocket.Conn // Underlying websocket connection
	send       chan []byte     // Channel for sending messages to the client
	room       *Room           // Reference to the parent room
	pubsub     *pubsub.PubSub  // PubSub instance for this connection
}

// Queues a message to be sent to the client through the websocket connection.
func (c *Connection) Send(message []byte) {
	c.send <- message
}

// Creates a new room instance with a unique identifer and type.
func NewRoom(id string, roomType string) *Room {
	r := &Room{
		Id:           id,
		RoomType:     roomType,
		Connections:  make(map[string]*Connection),
		messages:     make(chan RoomMessage),
		pubsub:       pubsub.NewPubSub(),
		LastActivity: time.Now(),
		disposeChan:  make(chan struct{}),
	}

	// Start the room's message handling loop
	go r.run()

	return r
}

// Main event loop for the room, handling incoming messages and disposal.
func (r *Room) run() {
	for {
		select {
		case msg, ok := <-r.messages:
			if !ok {
				return
			}
			switch m := msg.(type) {
			case *messages.ConnectionMessage:
				r.handleConnectionMessage(m.ConnectionId, m.Message)
			case *messages.Disconnect:
				r.Disconnect(m.ConnectionId)
			}
		case <-r.disposeChan:
			return
		}
	}
}

// Adds a new connection to the room
func (r *Room) AddConnection(conn *websocket.Conn, connectionId string) {
	if r.ConnectionLimit > 0 && len(r.Connections) >= r.ConnectionLimit {
		error.SendErrorResponse(conn, error.NewErrorResponse(error.RoomFull, "Room is full"))
		return
	}

	connection := &Connection{
		Id:         connectionId,
		Connection: conn,
		send:       make(chan []byte),
		room:       r,
		pubsub:     pubsub.NewPubSub(),
	}

	r.mutex.Lock()
	r.Connections[connectionId] = connection
	r.mutex.Unlock()
	r.LastActivity = time.Now()

	go r.readMessages(connection)
	go r.writeMessages(connection)

	if r.OnJoin != nil {
		r.OnJoin(connection)
	}
}

// Reads messages from a client's websocket connection and processes them.
func (r *Room) readMessages(c *Connection) {
	defer func() {
		r.messages <- messages.Disconnect{ConnectionId: c.Id}
	}()

	for {
		_, message, err := c.Connection.ReadMessage()
		if err != nil {
			log.Println("Read error:", err)
			break
		}
		r.messages <- messages.ConnectionMessage{
			ConnectionId: c.Id,
			Message:      message,
		}
	}
}

// Sends messages to the client's websocket connection.
func (r *Room) writeMessages(c *Connection) {
	defer func() {
		c.Connection.Close()
	}()

	for message := range c.send {
		err := c.Connection.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			log.Println("Write error:", err)
			break
		}
	}
}

// Processes a message from a client connection.
func (r *Room) handleConnectionMessage(connectionId string, message []byte) {
	r.mutex.Lock()
	connection, ok := r.Connections[connectionId]
	r.mutex.Unlock()
	if !ok {
		return
	}

	// Handle direct server-client communication
	if r.OnMessage != nil {
		r.OnMessage(connection, message)
	}

	// Handle application logic through pubsub
	var event map[string]interface{}
	err := json.Unmarshal(message, &event)
	if err != nil {
		log.Println("Failed to unmarshal message:", err)
		return
	}

	eventName, ok := event["event"].(string)
	if !ok {
		log.Println("Message missing 'event' field")
		return
	}

	switch eventName {
	case "nexus:room-leave":
		r.Disconnect(connectionId)
	default:
		r.pubsub.Publish(eventName, event["data"])
	}
}

// Removes a connection from the room and performs any necessary cleanup.
func (r *Room) Disconnect(connectionId string) {
	r.mutex.Lock()
	connection, ok := r.Connections[connectionId]
	if ok {
		disconnectMessage := map[string]interface{}{
			"event":   "nexus:room-leave",
			"room_id": r.Id,
		}
		message, _ := json.Marshal(disconnectMessage)
		connection.Connection.WriteMessage(websocket.TextMessage, message)

		close(connection.send)
		delete(r.Connections, connectionId)
	}
	r.mutex.Unlock()

	if ok && r.OnLeave != nil {
		r.OnLeave(connection)
	}

	r.LastActivity = time.Now()
}

// On subscribes an event handler to a specific event name within the room.
func (r *Room) On(event string, handler pubsub.EventHandler) {
	r.pubsub.Subscribe(event, handler)
}

// Emit broadcasts an event to all connections in the room.
func (r *Room) Emit(event string, payload interface{}) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	message := map[string]interface{}{
		"event": event,
		"data":  payload,
	}
	jsonMessage, err := json.Marshal(message)
	if err != nil {
		log.Println("Failed to marshal message:", err)
		return
	}

	for _, conn := range r.Connections {
		conn.Send(jsonMessage)
	}
}

// EmitTo broadcasts an event to a specific connection within the room.
func (r *Room) EmitTo(connectionId string, event string, payload interface{}) {
	r.mutex.Lock()
	conn, ok := r.Connections[connectionId]
	r.mutex.Unlock()

	if !ok {
		return
	}

	message := map[string]interface{}{
		"event": event,
		"data":  payload,
	}
	jsonMessage, err := json.Marshal(message)
	if err != nil {
		log.Println("Failed to marshal message:", err)
		return
	}

	conn.Send(jsonMessage)
}

// IsInactive checks if the room is inactive and should be disposed of.
func (r *Room) IsInactive() bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return len(r.Connections) == 0 && (r.AutoDispose > 0 && time.Since(r.LastActivity) > time.Duration(r.AutoDispose)*time.Second)
}

// Disposes of the room and cleans up all resources.
func (r *Room) Dispose() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.disposed {
		return
	}
	r.disposed = true

	for id := range r.Connections {
		r.Disconnect(id)
	}

	close(r.messages)
	close(r.disposeChan)

	log.Printf("Room %s disposed", r.Id)
}
