package matchmaker

import (
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

func (m *Matchmaker) JoinOrCreate(conn *websocket.Conn, roomType string) {
	m.Mutex.Lock()
	defer m.Mutex.Unlock()

	for _, room := range m.Rooms {
		if room.RoomType == roomType {
			room.AddConnection(conn)
			return
		}
	}

	m.createRoom(conn, roomType)
}

func (m *Matchmaker) Join(conn *websocket.Conn, roomId string) {
	m.Mutex.Lock()
	defer m.Mutex.Unlock()

	room, ok := m.Rooms[roomId]
	if !ok {
		log.Printf("Room %s does not exist", roomId)
		conn.Close()
		return
	}
	room.AddConnection(conn)
}

func (m *Matchmaker) Create(conn *websocket.Conn, roomType string) {
	m.Mutex.Lock()
	defer m.Mutex.Unlock()

	m.createRoom(conn, roomType)
}

func (m *Matchmaker) createRoom(conn *websocket.Conn, roomType string) {
	roomId := uuid.New().String()
	factory, ok := m.RoomTypes[roomType]
	if !ok {
		log.Printf("Room type %s is not defined", roomType)
		conn.Close()
		return
	}

	room := factory(roomId)
	m.Rooms[roomId] = room
	room.AddConnection(conn)
}
