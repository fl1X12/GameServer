package constants

// WebSocket Message Types
const (
	MessageTypeFindMatch = "FIND_MATCH"
	MessageTypeMakeMove  = "MAKE_MOVE"
	MessageTypeForfeit   = "FORFEIT"
	MessageTypeGameStart = "GAME_START"
	MessageTypeOppMove   = "OPP_MOVE"
	MessageTypeGameOver  = "GAME_OVER"
)

// Game Constants
const (
	TicTacToeGridSide = 3
	BoardSize         = TicTacToeGridSide * TicTacToeGridSide
	EmptyCell         = -1
	Player0           = 0
	Player1           = 1
	DrawResult        = 2
	GameContinues     = -1
	MinBoardIndex     = 0
	MaxBoardIndex     = 8
)

// Room Constants
const (
	RoomIDPrefix = "room_"
	FirstTurn    = 0
)

// Player Index
const (
	FirstPlayer  = 0
	SecondPlayer = 1
)

// Server Configuration
const (
	DefaultServerPort = ":8080"
	WebSocketPath     = "/ws"
	StatusPath        = "/status"
)

// HTTP Responses
const (
	StatusCheckConsole = "Check console for server state"
)
