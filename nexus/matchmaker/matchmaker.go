// The matchmaker package manages the creation, joining and removal of rooms in the server.
// Also manages room types, and routine cleanup of inactive servers.

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

// Handles room management, including room creation, joining, and maintenance.
// Uses a map to store active rooms and another map to store factory functions for creating various room types.
type Matchmaker struct {
	Rooms     map[string]*room.Room              // Active rooms mapped by their unique IDs
	RoomTypes map[string]func(string) *room.Room // Factory functions for creating rooms by type, mapped by their defined id
	mutex     sync.Mutex                         // Mutex for synchronizing access to rooms & room types
}

// Initializes a matchmaker instances and starts related jobs.
func NewMatchmaker() *Matchmaker {
	m := &Matchmaker{
		Rooms:     make(map[string]*room.Room),
		RoomTypes: make(map[string]func(string) *room.Room),
	}

	go m.cleanInactiveRoomsJob()

	return m
}

// Registers a factory function for creating rooms of a specific type.
// e.g server.Matchmaker.DefineRoomType("GameRoom", NewGameRoom)
// then: server.Matchmaker.JoinOrCreate("GameRoom") <-- Joins or creates a new GameRoom
func (m *Matchmaker) DefineRoomType(roomType string, factory func(string) *room.Room) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.RoomTypes[roomType] = factory
}

// JoinOrCreate tries to join a room of the specified type if one exists, otherwise creates a new room of the specified type.
func (m *Matchmaker) JoinOrCreate(conn *websocket.Conn, connectionId string, roomType string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, room := range m.Rooms {
		if room.RoomType != roomType {
			continue
		}

		// Skip rooms that are full
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

	// No room was found? Create a new one
	m.createRoom(conn, connectionId, roomType)
}

// Join adds a connection to an existing room by ID.
// If the room does not exist, it'll send an error response to the client.
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

// Create initializes a new room of a specified type and adds the connection to it.
func (m *Matchmaker) Create(conn *websocket.Conn, connectionId string, roomType string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.createRoom(conn, connectionId, roomType)
}

// Internal helper for creating new rooms and notifying the client that sent the action.
// Checks if the room type is defined and uses the corresponding factory function.
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

// Removes the connection (by id) from all rooms, usually called when a client disconnects.
func (m *Matchmaker) RemoveConnection(connectionId string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, room := range m.Rooms {
		room.Disconnect(connectionId)
	}
}

// Background job for periodically removing inactive rooms.
// TODO: Have rooms handle their cleanup independently through a callback back to the matchmaker.
func (m *Matchmaker) cleanInactiveRoomsJob() {
	for {
		time.Sleep(1 * time.Minute)
		m.CleanInactiveRooms()
	}
}

// Removes rooms that are considered inactive, i.e, rooms with no active connections
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
