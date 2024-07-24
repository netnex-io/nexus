package nexus

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/netnex-io/nexus/matchmaker"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Server struct {
	Matchmaker *matchmaker.Matchmaker
}

func NewServer() *Server {
	return &Server{
		Matchmaker: matchmaker.NewMatchmaker(),
	}
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("failed to upgrade connection:", err)
		return
	}

	// Generate a unique connection ID for this connection
	connectionId := uuid.New().String()

	// Send the connection ID to the client
	s.handleConnectionEstablishedMessage(conn, connectionId)

	// Handle disconnect & clean-up
	defer func() {
		log.Printf("Connection closed for ID: %s\n", connectionId)
		s.Matchmaker.RemoveConnection(connectionId)
		conn.Close()
	}()

	// Handle messages after sending the connection ID
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("Connection closed normally for ID %s: %v\n", connectionId, err)
			} else {
				log.Printf("Unexpected error for ID %s: %v\n", connectionId, err)
			}
			return
		}

		var payload matchmaker.MatchmakerRequestPayload
		err = json.Unmarshal(message, &payload)
		if err != nil {
			log.Printf("Failed to decode request payload for ID %s: %v\n:", connectionId, err)
			return
		}

		switch payload.Action {
		case "JoinOrCreate":
			s.Matchmaker.JoinOrCreate(conn, connectionId, payload.RoomType)
		case "Join":
			s.Matchmaker.Join(conn, connectionId, payload.RoomId)
		case "Create":
			s.Matchmaker.Create(conn, connectionId, payload.RoomType)
		default:
			log.Println("Unknown Matchmaker Action:", payload.Action)
			conn.Close()
		}
	}
}

func (s *Server) handleConnectionEstablishedMessage(conn *websocket.Conn, connectionId string) {
	// Send the server-created connection id to the client
	successMessage := map[string]interface{}{
		"event":         "nexus:connection-established",
		"connection_id": connectionId,
	}
	jsonMessage, err := json.Marshal(successMessage)

	if err != nil {
		log.Println("Failed to marshal connection established message:", err)
		return
	}

	if err := conn.WriteMessage(websocket.TextMessage, jsonMessage); err != nil {
		log.Println("Failed to send connection established message:", err)
	}

}

func (s *Server) Start(addr string) {
	r := mux.NewRouter()
	r.HandleFunc("/ws", s.handleWebSocket).Methods("GET")

	log.Printf("Server started on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
