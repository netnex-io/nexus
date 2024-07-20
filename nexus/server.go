package nexus

import (
	"encoding/json"
	"log"
	"net/http"

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

	_, message, err := conn.ReadMessage()
	if err != nil {
		log.Println("Failed to read message:", err)
		conn.Close()
		return
	}

	var payload matchmaker.MatchmakerRequestPayload
	err = json.Unmarshal(message, &payload)
	if err != nil {
		log.Println("Failed to decode request payload:", err)
		conn.Close()
		return
	}

	switch payload.Action {
	case "JoinOrCreate":
		s.Matchmaker.JoinOrCreate(conn, payload.RoomType)
	case "Join":
		s.Matchmaker.Join(conn, payload.RoomId)
	case "Create":
		s.Matchmaker.Create(conn, payload.RoomType)
	default:
		log.Println("Unknown Matchmaker Action:", payload.Action)
		conn.Close()
	}
}

func (s *Server) Start(addr string) {
	r := mux.NewRouter()
	r.HandleFunc("/ws", s.handleWebSocket).Methods("GET")

	log.Printf("Server started on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
