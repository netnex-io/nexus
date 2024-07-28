// This package contains teh server's entry point and related functionalities.
// Responsible for initializing the WebSocket server, setting up routes, managing server configuration,
// and handling incoming WebSocket connections.

package nexus

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/netnex-io/nexus/error"
	"github.com/netnex-io/nexus/matchmaker"
)

// Defines config options for the Nexus Server.
type ServerConfig struct {
	AllowedOrigins    []string // List of allowed origins for WebSocket connections
	MaxMessageSize    int64    // Maximum message size allowed from clients
	CertFile          string   // File path for SSL/TLS Certificate
	KeyFile           string   // File path for SSL/TLS Key
	ConnectionLimit   int      // Maximum number of concurrent connections
	IdleTimeout       int      // Time in seconds before an idle connection is closed
	EnableCompression bool     // Whether to enable WebSocket message compression
}

func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		AllowedOrigins:    []string{"*"}, // Allow all origins by default
		MaxMessageSize:    0,             // Default max message size (1KB)
		CertFile:          "",            // Default empty string for CertFile
		KeyFile:           "",            // Default empty string for KeyFile
		ConnectionLimit:   0,             // Default unlimited connections
		IdleTimeout:       0,             // Default idle timeout in seconds
		EnableCompression: true,          // Enable permessage-deflate compression
	}
}

// Represents the Nexus/WebSocket server and its associated state.
// Manages the matchmaker for room handling.
type Server struct {
	Matchmaker  *matchmaker.Matchmaker // Manages rooms & matches players
	Config      ServerConfig           // Server configuration settings
	connections int                    // Active connection count
}

// Creates and returns a new server instance with provided cfg.
func NewServer(config ServerConfig) *Server {
	return &Server{
		Matchmaker: matchmaker.NewMatchmaker(),
		Config:     config,
	}
}

// Entrypoint for handling incoming WebSocket connections.
// Responsible for upgrading HTTP requests, enforcing server cfg limits, and managing client communication.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrades the HTTP server connection to the WebSocket protocol.
	upgrader := websocket.Upgrader{
		// Checks the origin of the request, and allows connections only from the specified origin(s).
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
		// enables WebSocket compression if set in the server cfg
		EnableCompression: s.Config.EnableCompression,
	}

	// Perform the HTTP -> WebSocket protocol upgrade
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("failed to upgrade connection:", err)
		return
	}

	// Set the connection's max message size based on the server's configuration
	if s.Config.MaxMessageSize != 0 {
		conn.SetReadLimit(s.Config.MaxMessageSize)
	}

	// Increment the active connections counter
	s.connections++
	defer func() {
		s.connections-- // Decrement when connections close
		conn.Close()    // Ensure the connection is closed
	}()

	// Limit the number of concurrent connections by server config
	if s.connections > s.Config.ConnectionLimit && s.Config.ConnectionLimit != 0 {
		error.SendErrorResponse(conn, error.NewErrorResponse(error.ConnectionLimitError, "Connection limit reached"))
		conn.Close()
		return
	}

	// Generate a unique connection ID for this connection
	connectionId := uuid.New().String()

	// Send the connection ID to the client
	s.handleConnectionEstablishedMessage(conn, connectionId)

	// Handle cleanup when the connection is closed
	defer func() {
		log.Printf("Connection closed for ID: %s\n", connectionId)
		s.Matchmaker.RemoveConnection(connectionId)
		conn.Close()
	}()

	// Continuously read from the WebSocket connection
	for {
		// Set a read deadline to enforce the IdleTimeout server cfg, if configured.
		if s.Config.IdleTimeout != 0 {
			conn.SetReadDeadline(time.Now().Add(time.Duration(s.Config.IdleTimeout) * time.Second))
		}

		// Read a message from the connection
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("Connection closed normally for ID %s: %v\n", connectionId, err)
			} else {
				log.Printf("Unexpected error for ID %s: %v\n", connectionId, err)
			}
			return
		}

		// Unmarshal the received message into a MatchmakerRequestPayload
		// This will handle which action the client sent to the server
		var payload matchmaker.MatchmakerRequestPayload
		err = json.Unmarshal(message, &payload)
		if err != nil {
			log.Printf("Failed to decode request payload for ID %s: %v\n:", connectionId, err)
			return
		}

		// Process the action specified in the payload
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

// Handles sending a connection established message to the client with the server-generated connection ID
// confirming the connection. This is important as Nexus is authoritative with connection IDs, clients should
// not manage their own connection ID to the server.
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

// Start begins the WebSocket server, setting up routes and starting the HTTP server.
// It listens on the provided address and uses TLS if certificates and key files were provided in cfg.
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
