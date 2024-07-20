package room

import (
	"log"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/netnex-io/nexus/room/messages"
)

type Room struct {
	Id          string
	RoomType    string
	Connections map[string]*Connection
	Mutex       sync.Mutex
	messages    chan RoomMessage

	OnJoin    func(c *Connection)
	OnLeave   func(c *Connection)
	OnMessage func(c *Connection, message []byte)
}

type Connection struct {
	Id         string
	Connection *websocket.Conn
	send       chan []byte
}

func NewRoom(id string, roomType string) *Room {
	r := &Room{
		Id:          id,
		RoomType:    roomType,
		Connections: make(map[string]*Connection),
		messages:    make(chan RoomMessage),
	}

	go r.run()
	return r
}

func (r *Room) run() {
	for msg := range r.messages {
		switch m := msg.(type) {
		case *messages.NewConnection:
			r.handleNewConnection(m.Connection)
		case *messages.ConnectionMessage:
			r.handleConnectionMessage(m.ConnectionId, m.Message)
		case *messages.Disconnect:
			r.handleDisconnect(m.ConnectionId)
		}
	}
}

func (r *Room) AddConnection(conn *websocket.Conn) {
	connectionId := uuid.New().String()
	connection := &Connection{
		Id:         connectionId,
		Connection: conn,
		send:       make(chan []byte),
	}

	r.Mutex.Lock()
	r.Connections[connectionId] = connection
	r.Mutex.Unlock()

	go r.readMessages(connection)
	go r.writeMessages(connection)

	log.Printf("New connection %s added to room %s", connectionId, r.Id)
	if r.OnJoin != nil {
		r.OnJoin(connection)
	}
}

func (r *Room) handleNewConnection(conn *websocket.Conn) {
	connectionId := uuid.New().String()
	connection := &Connection{
		Id:         connectionId,
		Connection: conn,
		send:       make(chan []byte),
	}

	r.Mutex.Lock()
	r.Connections[connectionId] = connection
	r.Mutex.Unlock()

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
	r.Mutex.Lock()
	connection, ok := r.Connections[connectionId]
	r.Mutex.Unlock()
	if !ok {
		return
	}

	if r.OnMessage != nil {
		r.OnMessage(connection, message)
	}
}

func (r *Room) handleDisconnect(connectionId string) {
	r.Mutex.Lock()
	connection, ok := r.Connections[connectionId]
	if ok {
		delete(r.Connections, connectionId)
		close(connection.send)
	}
	r.Mutex.Unlock()

	if ok && r.OnLeave != nil {
		r.OnLeave(connection)
	}
}
