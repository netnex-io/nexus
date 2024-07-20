package messages

import "github.com/gorilla/websocket"

type NewConnection struct {
	Connection *websocket.Conn
}

type ConnectionMessage struct {
	ConnectionId string
	Message      []byte
}

type Disconnect struct {
	ConnectionId string
}
