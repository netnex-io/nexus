package matchmaker

type MatchmakerRequestPayload struct {
	Action   string `json:"action"`
	RoomType string `json:"room_type,omitempty"`
	RoomId   string `json:"room_id,omitempty"`
}
