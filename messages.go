package main

// message type
type Message struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

// Payload to make a move
type MakeMovePayload struct {
	Index int `json:"index"` //indicates where the move is being played
}

type BroadcastMovePayload struct {
	Index       int `json:"index"`
	PlayerIndex int `json:"playerIndex"`
}

// Initial Game payload to tell Unity the starting state of the game
type GameStartPayload struct {
	PlayerIndex int  `json:"playerIndex"` // 0 - O and 1 - X
	Turn        bool `json:"turn"`
}

type GameOverPayload struct {
	Winner int `json:"winner"`
}
