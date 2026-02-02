package main

import (
	"encoding/json"
	"fmt"
	"network/constants"
	"sync"

	"github.com/gorilla/websocket"
)

type Client struct {
	ID          string
	Conn        *websocket.Conn
	PlayerIndex int
	RoomId      string
}

type Room struct {
	Id      string
	Clients map[string]*Client
	Turn    int
	Board   [9]int
	Mutex   sync.Mutex
}

type RoomManager struct {
	matchmakingQueue []string
	queueLock        sync.Mutex

	rooms     map[string]*Room
	roomsLock sync.RWMutex

	totalMatches int

	ClientMap     map[string]*Client // Changed from map[*websocket.Conn]*Client to map[string]*Client
	ClientMapLock sync.RWMutex

	// New: mapping from connection to clientID for quick lookups
	ConnToClientID     map[*websocket.Conn]string
	ConnToClientIDLock sync.RWMutex
}

func NewRoomManager() *RoomManager {
	return &RoomManager{
		matchmakingQueue: make([]string, 0),
		rooms:            make(map[string]*Room),
		totalMatches:     0,
		ClientMap:        make(map[string]*Client),
		ConnToClientID:   make(map[*websocket.Conn]string),
	}
}

func (rm *RoomManager) HandleMove(conn *websocket.Conn, msg Message) {
	// Get clientID from connection
	rm.ConnToClientIDLock.RLock()
	clientID, ok := rm.ConnToClientID[conn]
	rm.ConnToClientIDLock.RUnlock()
	if !ok {
		fmt.Println("Connection not found, ignoring move")
		return
	}

	rm.ClientMapLock.RLock()
	client, ok := rm.ClientMap[clientID]
	rm.ClientMapLock.RUnlock()
	if !ok {
		fmt.Println("Invalid player ignoring move")
		return
	}

	rm.roomsLock.RLock()
	room, ok := rm.rooms[client.RoomId]
	rm.roomsLock.RUnlock()
	if !ok {
		fmt.Println("Invalid Room ignoring move")
		return
	}

	room.Mutex.Lock()
	defer room.Mutex.Unlock()

	if room.Turn != client.PlayerIndex {
		fmt.Println("Move played out of turn ignore")
		return
	}

	var moveData MakeMovePayload
	if err := json.Unmarshal([]byte(msg.Payload), &moveData); err != nil {
		fmt.Println("Invalid move payload:", err)
		return
	}

	if moveData.Index < 0 || moveData.Index > 8 {
		fmt.Println("Invalid move Position", moveData.Index)
		return
	}

	if room.Board[moveData.Index] != -1 {
		fmt.Println("Cell full")
		return
	}

	room.Board[moveData.Index] = client.PlayerIndex
	broadcastMove := BroadcastMovePayload{
		Index:       moveData.Index,
		PlayerIndex: client.PlayerIndex,
	}
	moveJson, _ := json.Marshal(broadcastMove)
	moveMsg := Message{Type: constants.MessageTypeOppMove, Payload: string(moveJson)}

	// Broadcast to all clients except the current one
	for _, c := range room.Clients {
		if c.ID != clientID {
			c.Conn.WriteJSON(moveMsg)
		}
	}

	res := checkWin(room.Board)

	if res != -1 {
		overPayload := GameOverPayload{Winner: res}
		if res == 2 {
			overPayload.Winner = -1
		}

		jsonBytes, _ := json.Marshal(overPayload)
		finalMsg := Message{Type: constants.MessageTypeGameOver, Payload: string(jsonBytes)}

		for _, c := range room.Clients {
			c.Conn.WriteJSON(finalMsg)
		}

		go rm.DeleteRoom(room.Id)
		return
	}
	room.Turn = 1 - room.Turn
}

func (rm *RoomManager) HandleFindMatch(conn *websocket.Conn, clientID string) {
	rm.queueLock.Lock()
	defer rm.queueLock.Unlock()

	if len(rm.matchmakingQueue) == 0 {
		rm.matchmakingQueue = append(rm.matchmakingQueue, clientID)
		fmt.Println("Player added to queue, waiting for match...")
		return
	}

	oppClientID := rm.matchmakingQueue[0]
	rm.matchmakingQueue = rm.matchmakingQueue[1:]

	// Get opponent's client info
	rm.ClientMapLock.RLock()
	oppClient, oppExists := rm.ClientMap[oppClientID]
	rm.ClientMapLock.RUnlock()

	if !oppExists {
		// Opponent no longer exists, try to match current player again
		rm.matchmakingQueue = append(rm.matchmakingQueue, clientID)
		fmt.Println("Opponent disconnected, re-queuing player...")
		return
	}

	roomId := fmt.Sprintf("%s", generateClientID())

	newRoom := &Room{
		Id:      roomId,
		Clients: make(map[string]*Client),
		Turn:    0,
		Board:   [9]int{-1, -1, -1, -1, -1, -1, -1, -1, -1},
	}
	rm.rooms[roomId] = newRoom
	rm.roomsLock.Unlock()

	ClientO := &Client{ID: clientID, Conn: conn, PlayerIndex: 0, RoomId: roomId}
	ClientX := &Client{ID: oppClientID, Conn: oppClient.Conn, PlayerIndex: 1, RoomId: roomId}

	newRoom.Clients[clientID] = ClientO
	newRoom.Clients[oppClientID] = ClientX

	rm.ClientMapLock.Lock()
	rm.ClientMap[clientID] = ClientO
	rm.ClientMap[oppClientID] = ClientX
	rm.ClientMapLock.Unlock()

	fmt.Printf("Match Started! Room: %s\n", roomId)

	broadcastGameStart(newRoom)
}

func (rm *RoomManager) HandleDisconnect(conn *websocket.Conn) {
	// Get clientID from connection
	rm.ConnToClientIDLock.RLock()
	clientID, exists := rm.ConnToClientID[conn]
	rm.ConnToClientIDLock.RUnlock()

	if !exists {
		fmt.Println("Connection not found in ConnToClientID map")
		return
	}

	rm.RemoveFromQueue(clientID)

	rm.ClientMapLock.RLock()
	client, clientExists := rm.ClientMap[clientID]
	rm.ClientMapLock.RUnlock()

	if !clientExists {
		fmt.Println("Client disconnected (not in game)")
		// Clean up connection mapping
		rm.ConnToClientIDLock.Lock()
		delete(rm.ConnToClientID, conn)
		rm.ConnToClientIDLock.Unlock()
		return
	}

	fmt.Printf("Player %d (ID: %s) disconnected from room %s\n", client.PlayerIndex, clientID, client.RoomId)

	rm.roomsLock.RLock()
	room, roomExists := rm.rooms[client.RoomId]
	rm.roomsLock.RUnlock()

	if roomExists {
		room.Mutex.Lock()
		for _, c := range room.Clients {
			if c.ID != clientID {
				overPayload := GameOverPayload{Winner: c.PlayerIndex}
				jsonBytes, _ := json.Marshal(overPayload)
				dcMsg := Message{
					Type:    "GAME_OVER",
					Payload: string(jsonBytes),
				}
				c.Conn.WriteJSON(dcMsg)
			}
		}
		room.Mutex.Unlock()

		go rm.DeleteRoom(client.RoomId)
	}

	// Clean up client mapping
	rm.ClientMapLock.Lock()
	delete(rm.ClientMap, clientID)
	rm.ClientMapLock.Unlock()

	// Clean up connection to clientID mapping
	rm.ConnToClientIDLock.Lock()
	delete(rm.ConnToClientID, conn)
	rm.ConnToClientIDLock.Unlock()
}

func (rm *RoomManager) HandleForfeit(conn *websocket.Conn) {
	// Get clientID from connection
	rm.ConnToClientIDLock.RLock()
	clientID, ok := rm.ConnToClientID[conn]
	rm.ConnToClientIDLock.RUnlock()

	if !ok {
		fmt.Println("Connection not found for forfeit")
		return
	}

	rm.ClientMapLock.RLock()
	client, ok := rm.ClientMap[clientID]
	rm.ClientMapLock.RUnlock()

	if !ok {
		return
	}

	rm.roomsLock.RLock()
	room, exists := rm.rooms[client.RoomId]
	rm.roomsLock.RUnlock()

	var overPayload GameOverPayload
	if exists {
		room.Mutex.Lock()
		for _, c := range room.Clients {
			if c.ID != clientID {
				overPayload = GameOverPayload{Winner: c.PlayerIndex}
				break
			}
		}

		jsonBytes, _ := json.Marshal(overPayload)
		dcMsg := Message{
			Type:    "GAME_OVER",
			Payload: string(jsonBytes),
		}

		for _, c := range room.Clients {
			c.Conn.WriteJSON(dcMsg)
		}
		room.Mutex.Unlock()
		go rm.DeleteRoom(client.RoomId)
	}
}

func checkWin(board [9]int) int {
	wins := [][]int{
		{0, 1, 2}, {3, 4, 5}, {6, 7, 8},
		{0, 3, 6}, {1, 4, 7}, {2, 5, 8},
		{0, 4, 8}, {2, 4, 6},
	}

	for _, line := range wins {
		a, b, c := line[0], line[1], line[2]
		if board[a] != -1 && board[a] == board[b] && board[b] == board[c] {
			return board[a]
		}
	}

	for _, cell := range board {
		if cell == -1 {
			return -1
		}
	}

	return 2
}

func (rm *RoomManager) RemoveFromQueue(clientID string) {
	rm.queueLock.Lock()
	defer rm.queueLock.Unlock()

	for i, id := range rm.matchmakingQueue {
		if id == clientID {
			rm.matchmakingQueue = append(rm.matchmakingQueue[:i], rm.matchmakingQueue[i+1:]...)
			fmt.Println("Removed disconnected player from matchmaking")
			break
		}
	}
}

func (rm *RoomManager) DeleteRoom(roomId string) {
	rm.roomsLock.Lock()
	room, exists := rm.rooms[roomId]
	if !exists {
		rm.roomsLock.Unlock()
		return
	}
	delete(rm.rooms, roomId)
	rm.roomsLock.Unlock()

	// Get all client IDs
	room.Mutex.Lock()
	clientIDs := make([]string, 0, len(room.Clients))
	for clientID := range room.Clients {
		clientIDs = append(clientIDs, clientID)
	}
	room.Mutex.Unlock()

	// Only removing from ClientMap
	// Players stay connected and can immediately find another match
	rm.ClientMapLock.Lock()
	for _, clientID := range clientIDs {
		delete(rm.ClientMap, clientID)
	}
	rm.ClientMapLock.Unlock()

	fmt.Printf("Room %s deleted. Active games: %d \n", roomId, len(rm.rooms))
}

func broadcastGameStart(room *Room) {
	room.Mutex.Lock()
	defer room.Mutex.Unlock()

	for _, c := range room.Clients {
		isTurn := (c.PlayerIndex == room.Turn)

		startData := GameStartPayload{
			PlayerIndex: c.PlayerIndex,
			Turn:        isTurn,
		}
		fmt.Printf("%v : %v ", startData.PlayerIndex, startData.Turn)
		jsonBytes, _ := json.Marshal(startData)
		msg := Message{Type: "GAME_START", Payload: string(jsonBytes)}

		c.Conn.WriteJSON(msg)
	}
}

func (rm *RoomManager) Server_state_logger() {
	rm.roomsLock.Lock()
	rm.ClientMapLock.Lock()
	defer func() {
		rm.roomsLock.Unlock()
		rm.ClientMapLock.Unlock()
	}()

	for key := range rm.rooms {
		fmt.Printf("%s \n", key)
	}
}

// RegisterClient registers a new client connection with a unique ID
func (rm *RoomManager) RegisterClient(conn *websocket.Conn, clientID string) {
	rm.ConnToClientIDLock.Lock()
	rm.ConnToClientID[conn] = clientID
	rm.ConnToClientIDLock.Unlock()

	rm.ClientMapLock.Lock()
	rm.ClientMap[clientID] = &Client{
		ID:   clientID,
		Conn: conn,
	}
	rm.ClientMapLock.Unlock()

	fmt.Printf("Client registered with ID: %s\n", clientID)
}

// GetClientID retrieves the clientID for a given connection
func (rm *RoomManager) GetClientID(conn *websocket.Conn) (string, bool) {
	rm.ConnToClientIDLock.RLock()
	defer rm.ConnToClientIDLock.RUnlock()
	clientID, ok := rm.ConnToClientID[conn]
	return clientID, ok
}
