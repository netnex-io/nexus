package matchmaker

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/netnex-io/nexus/room"
)

type Matchmaker struct {
	Rooms     map[string]*room.Room
	RoomTypes map[string]func(string) *room.Room
	Mutex     sync.Mutex
}

func NewMatchmaker() *Matchmaker {
	return &Matchmaker{
		Rooms:     make(map[string]*room.Room),
		RoomTypes: make(map[string]func(string) *room.Room),
	}
}

func (m *Matchmaker) DefineRoomType(roomType string, factory func(string) *room.Room) {
	m.Mutex.Lock()
	defer m.Mutex.Unlock()
	m.RoomTypes[roomType] = factory
}

func (m *Matchmaker) JoinOrCreate(conn *websocket.Conn, connectionId string, roomType string) {
	m.Mutex.Lock()
	defer m.Mutex.Unlock()

	for _, room := range m.Rooms {
		if room.RoomType == roomType {
			// Send the room joined event to the client
			joinMessage := map[string]interface{}{
				"event":   "nexus:room-joined",
				"room_id": room.Id,
			}
			jsonMessage, err := json.Marshal(joinMessage)

			if err != nil {
				log.Println("Failed to marshal room joined message:", err)
				return
			}

			if err := conn.WriteMessage(websocket.TextMessage, jsonMessage); err != nil {
				log.Println("Failed to send room joined message:", err)
			}

			room.AddConnection(conn, connectionId)

			return
		}
	}

	m.createRoom(conn, connectionId, roomType)
}

func (m *Matchmaker) Join(conn *websocket.Conn, connectionId string, roomId string) {
	m.Mutex.Lock()
	defer m.Mutex.Unlock()

	room, ok := m.Rooms[roomId]
	if !ok {
		log.Printf("Room %s does not exist", roomId)
		conn.Close()
		return
	}

	// Send the room joined event to the client
	joinMessage := map[string]interface{}{
		"event":   "nexus:room-joined",
		"room_id": room.Id,
	}
	jsonMessage, err := json.Marshal(joinMessage)

	if err != nil {
		log.Println("Failed to marshal room joined message:", err)
		return
	}

	if err := conn.WriteMessage(websocket.TextMessage, jsonMessage); err != nil {
		log.Println("Failed to send room joined message:", err)
	}

	room.AddConnection(conn, connectionId)
}

func (m *Matchmaker) Create(conn *websocket.Conn, connectionId string, roomType string) {
	m.Mutex.Lock()
	defer m.Mutex.Unlock()

	m.createRoom(conn, connectionId, roomType)
}

func (m *Matchmaker) createRoom(conn *websocket.Conn, connectionId string, roomType string) {
	roomId := uuid.New().String()
	factory, ok := m.RoomTypes[roomType]
	if !ok {
		log.Printf("Room type %s is not defined", roomType)
		conn.Close()
		return
	}

	room := factory(roomId)
	m.Rooms[roomId] = room

	// Send the room created event to the client
	joinMessage := map[string]interface{}{
		"event":   "nexus:room-created",
		"room_id": room.Id,
	}
	jsonMessage, err := json.Marshal(joinMessage)

	if err != nil {
		log.Println("Failed to marshal room created message:", err)
		return
	}

	if err := conn.WriteMessage(websocket.TextMessage, jsonMessage); err != nil {
		log.Println("Failed to send room created message:", err)
	}

	room.AddConnection(conn, connectionId)
}

func (m *Matchmaker) RemoveConnection(connectionId string) {
	m.Mutex.Lock()
	defer m.Mutex.Unlock()

	for _, room := range m.Rooms {
		room.Disconnect(connectionId)
	}
}
