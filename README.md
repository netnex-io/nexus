# 🌐 Nexus
Open source server framework for realtime games & apps.

### Example
```go
package main

import (
  "github.com/netnex-io/nexus"
  "github.com/netnex-io/nexus/room"
)

type CustomRoom struct {
  *room.Room
  // Throw in your own room's state!
  Score int
}

func NewCustomRoom(id string) *room.Room {
  customRoom := &CustomRoom {
    Room: room.NewRoom(id, "custom-room"),
    Score: 0,
  }

  // Configure your room!
  customRoom.Room.ConnectionLimit = 5;

  // Hook into room events!
  customRoom.Room.OnJoin = customRoom.onJoin
  customRoom.Room.OnLeave = ...
  customRoom.Room.OnMessage = ...

  // Subscribe to a custom event!
  customRoom.Room.On("custom-event", func(payload interface{}) {
    // ...
  })

  return customRoom.Room
}

func (cr *CustomRoom) onJoin(c *room.Connection) {
  // Emit an event to all connected clients
  cr.Room.Emit("player:join", map[string]string{
    "player_id": c.Id,
  })

  // Emit an event to a specified client
  cr.Room.EmitTo(c.Id, "room:joined", map[string]string{"message": "You joined the room!"})
}

func main() {
  server := nexus.NewServer(nexus.DefaultServerConfig())
  server.Matchmaker.DefineRoomType("custom-room", NewCustomRoom)

  server.Start(":8080")
}
```

