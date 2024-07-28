package matchmaker

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/netnex-io/nexus/error"
	"github.com/netnex-io/nexus/room"
)

type Matchmaker struct {
	Rooms     map[string]*room.Room
	RoomTypes map[string]func(string) *room.Room
	mutex     sync.Mutex
}

func NewMatchmaker() *Matchmaker {
	m := &Matchmaker{
		Rooms:     make(map[string]*room.Room),
		RoomTypes: make(map[string]func(string) *room.Room),
	}

	go m.cleanInactiveRoomsJob()

	return m
}

func (m *Matchmaker) DefineRoomType(roomType string, factory func(string) *room.Room) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.RoomTypes[roomType] = factory
}

func (m *Matchmaker) JoinOrCreate(conn *websocket.Conn, connectionId string, roomType string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, room := range m.Rooms {
		if room.RoomType != roomType {
			continue
		}

		if room.ConnectionLimit != 0 && len(room.Connections) >= room.ConnectionLimit {
			continue
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

		return
	}

	m.createRoom(conn, connectionId, roomType)
}

func (m *Matchmaker) Join(conn *websocket.Conn, connectionId string, roomId string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	room, ok := m.Rooms[roomId]
	if !ok {
		error.SendErrorResponse(conn, error.NewErrorResponse(error.RoomNotFound, fmt.Sprintf("Room %s not found", roomId)))
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
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.createRoom(conn, connectionId, roomType)
}

func (m *Matchmaker) createRoom(conn *websocket.Conn, connectionId string, roomType string) {
	roomId := uuid.New().String()
	factory, ok := m.RoomTypes[roomType]
	if !ok {
		error.SendErrorResponse(conn, error.NewErrorResponse(error.RoomTypeNotFound, "Invalid room type"))
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
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, room := range m.Rooms {
		room.Disconnect(connectionId)
	}
}

func (m *Matchmaker) cleanInactiveRoomsJob() {
	for {
		time.Sleep(1 * time.Minute)
		m.CleanInactiveRooms()
	}
}

func (m *Matchmaker) CleanInactiveRooms() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for id, rm := range m.Rooms {
		if rm.IsInactive() {
			delete(m.Rooms, id)
			rm.Dispose()
		}
	}
}
