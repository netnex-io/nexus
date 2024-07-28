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

type Room struct {
	Id              string
	RoomType        string
	Connections     map[string]*Connection
	ConnectionLimit int
	AutoDispose     int // Auto dispose timeout in seconds, 0 for no timeout
	LastActivity    time.Time
	mutex           sync.Mutex

	// Messages is in charge of direct bidirectional communication, handling connection managers when a client joins/leaves the room
	messages chan RoomMessage
	// The pubsub is in charge of application logic (e.g room specific events, player movement, etc)
	pubsub *pubsub.PubSub

	OnJoin    func(c *Connection)
	OnLeave   func(c *Connection)
	OnMessage func(c *Connection, message []byte)

	disposeChan chan struct{}
	disposed    bool
}

type Connection struct {
	Id         string
	Connection *websocket.Conn
	send       chan []byte
	room       *Room
	pubsub     *pubsub.PubSub
}

func (c *Connection) Send(message []byte) {
	c.send <- message
}

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

	go r.run()
	return r
}

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

func (r *Room) On(event string, handler pubsub.EventHandler) {
	r.pubsub.Subscribe(event, handler)
}

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

func (r *Room) IsInactive() bool {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return len(r.Connections) == 0 && (r.AutoDispose > 0 && time.Since(r.LastActivity) > time.Duration(r.AutoDispose)*time.Second)
}

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
