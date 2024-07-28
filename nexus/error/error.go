package error

import (
	"encoding/json"

	"github.com/gorilla/websocket"
)

type ErrorType string

const (
	// Room/Matchmaking Errors
	RoomNotFound     ErrorType = "ROOM_NOT_FOUND"
	RoomTypeNotFound ErrorType = "ROOM_TYPE_NOT_FOUND"
	RoomFull         ErrorType = "ROOM_FULL"

	// Auth Errors
	AuthenticationError ErrorType = "AUTHENTICATION_ERROR"

	// General Server Error
	ServerError          ErrorType = "SERVER_ERROR"
	ConnectionLimitError ErrorType = "SERVER_CONNECTION_LIMIT"
)

type ErrorResponse struct {
	ErrorType    ErrorType `json:"type"`
	ErrorMessage string    `json:"message"`
}

func NewErrorResponse(errorType ErrorType, message string) ErrorResponse {
	return ErrorResponse{
		ErrorType:    errorType,
		ErrorMessage: message,
	}
}

func SendErrorResponse(conn *websocket.Conn, resp ErrorResponse) {
	message, err := json.Marshal(resp)
	if err != nil {
		conn.Close()
		return
	}

	if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
		conn.Close()
	}
}
