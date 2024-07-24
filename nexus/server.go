package nexus

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/netnex-io/nexus/matchmaker"
)

type ServerConfig struct {
	AllowedOrigins    []string
	MaxMessageSize    int64
	CertFile          string
	KeyFile           string
	ConnectionLimit   int
	IdleTimeout       int
	EnableCompression bool
}

func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		AllowedOrigins:    []string{"*"}, // Allow all origins by default
		MaxMessageSize:    1024,          // Default max message size (1KB)
		CertFile:          "",            // Default empty string for CertFile
		KeyFile:           "",            // Default empty string for KeyFile
		ConnectionLimit:   0,             // Default unlimited connections
		IdleTimeout:       0,             // Default idle timeout in seconds
		EnableCompression: true,          // Enable permessage-deflate compression
	}
}

type Server struct {
	// Room Matchmaker
	Matchmaker *matchmaker.Matchmaker

	// Config
	Config ServerConfig

	// Active connection count
	connections int
}

func NewServer(config ServerConfig) *Server {
	return &Server{
		Matchmaker: matchmaker.NewMatchmaker(),
		Config:     config,
	}
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			if len(s.Config.AllowedOrigins) == 0 || s.Config.AllowedOrigins[0] == "*" {
				return true
			}

			origin := r.Header.Get("Origin")
			for _, allowedOrigin := range s.Config.AllowedOrigins {
				if origin == allowedOrigin {
					return true
				}
			}

			return false
		},
		EnableCompression: s.Config.EnableCompression,
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("failed to upgrade connection:", err)
		return
	}
	conn.SetReadLimit(s.Config.MaxMessageSize)
	s.connections++
	defer func() {
		s.connections--
		conn.Close()
	}()

	// Limit the number of concurrent connections by server config
	if s.connections > s.Config.ConnectionLimit && s.Config.ConnectionLimit != 0 {
		log.Printf("Connection limit exceeded: %v", s.connections)
		conn.Close()
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
		if s.Config.IdleTimeout != 0 {
			conn.SetReadDeadline(time.Now().Add(time.Duration(s.Config.IdleTimeout) * time.Second))
		}

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
	if s.Config.CertFile != "" && s.Config.KeyFile != "" {
		log.Fatal(http.ListenAndServeTLS(addr, s.Config.CertFile, s.Config.KeyFile, r))
	} else {
		log.Fatal(http.ListenAndServe(addr, r))
	}
}
