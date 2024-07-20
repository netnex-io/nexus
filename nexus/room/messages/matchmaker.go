package messages

import "github.com/gorilla/websocket"

type MatchmakerJoinOrCreate struct {
	RoomType string
	Conn     *websocket.Conn
}

type MatchmakerJoin struct {
	RoomId string
	Conn   *websocket.Conn
}

type MatchmakerCreate struct {
	RoomType string
	Conn     *websocket.Conn
}
