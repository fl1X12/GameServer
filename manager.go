package main

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
)

type Client struct {
	Conn        *websocket.Conn
	PlayerIndex int
	RoomId      string
}

type Room struct {
	Id      string
	Clients map[*websocket.Conn]*Client
	Turn    int
	Board   [9]int
	Mutex   sync.Mutex
}

type RoomManager struct {
	matchmakingQueue []*websocket.Conn
	queueLock        sync.Mutex

	rooms     map[string]*Room
	roomsLock sync.RWMutex

	totalMatches int

	ClientMap     map[*websocket.Conn]*Client
	ClientMapLock sync.RWMutex
}

func NewRoomManager() *RoomManager {
	return &RoomManager{
		matchmakingQueue: make([]*websocket.Conn, 0),
		rooms:            make(map[string]*Room),
		totalMatches:     0,
		ClientMap:        make(map[*websocket.Conn]*Client),
	}
}

func (rm *RoomManager) HandleMove(conn *websocket.Conn, msg Message) {
	rm.ClientMapLock.RLock()
	client, ok := rm.ClientMap[conn]
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
	moveMsg := Message{Type: "OPP_MOVE", Payload: string(moveJson)}

	for _, c := range room.Clients {
		if c.Conn != conn {
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
		finalMsg := Message{Type: "GAME_OVER", Payload: string(jsonBytes)}

		for _, c := range room.Clients {
			c.Conn.WriteJSON(finalMsg)
		}

		go rm.DeleteRoom(room.Id)
		return
	}
	room.Turn = 1 - room.Turn
}

func (rm *RoomManager) HandleFindMatch(conn *websocket.Conn) {
	rm.queueLock.Lock()
	defer rm.queueLock.Unlock()

	if len(rm.matchmakingQueue) == 0 {
		rm.matchmakingQueue = append(rm.matchmakingQueue, conn)
		fmt.Println("Player added to queue, waiting for match...")
		return
	}

	oppConn := rm.matchmakingQueue[0]
	rm.matchmakingQueue = rm.matchmakingQueue[1:]

	rm.roomsLock.Lock()
	rm.totalMatches++
	roomId := fmt.Sprintf("room_%d", rm.totalMatches)

	newRoom := &Room{
		Id:      roomId,
		Clients: make(map[*websocket.Conn]*Client),
		Turn:    0,
		Board:   [9]int{-1, -1, -1, -1, -1, -1, -1, -1, -1},
	}
	rm.rooms[roomId] = newRoom
	rm.roomsLock.Unlock()

	ClientO := &Client{Conn: conn, PlayerIndex: 0, RoomId: roomId}
	ClientX := &Client{Conn: oppConn, PlayerIndex: 1, RoomId: roomId}

	newRoom.Clients[conn] = ClientO
	newRoom.Clients[oppConn] = ClientX

	rm.ClientMapLock.Lock()
	rm.ClientMap[conn] = ClientO
	rm.ClientMap[oppConn] = ClientX
	rm.ClientMapLock.Unlock()

	fmt.Printf("Match Started! Room: %s\n", roomId)

	broadcastGameStart(newRoom)
}

func (rm *RoomManager) HandleDisconnect(conn *websocket.Conn) {
	rm.RemoveFromQueue(conn)

	rm.ClientMapLock.RLock()
	client, exists := rm.ClientMap[conn]
	rm.ClientMapLock.RUnlock()

	if !exists {
		fmt.Println("Client disconnected (not in game)")
		return
	}

	fmt.Printf("Player %d disconnected from room %s\n", client.PlayerIndex, client.RoomId)

	rm.roomsLock.RLock()
	room, roomExists := rm.rooms[client.RoomId]
	rm.roomsLock.RUnlock()

	if roomExists {
		room.Mutex.Lock()
		for _, c := range room.Clients {
			if c.Conn != conn {
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

	rm.ClientMapLock.Lock()
	delete(rm.ClientMap, conn)
	rm.ClientMapLock.Unlock()
}

func (rm *RoomManager) HandleForfeit(conn *websocket.Conn){
	rm.ClientMapLock.RLock()
	client,ok := rm.ClientMap[conn]
	rm.ClientMapLock.RUnlock()

	if !ok{
		return
	}

	rm.roomsLock.RLock()
	room,exists :=rm.rooms[client.RoomId]
	rm.roomsLock.RUnlock()

	var overPayload GameOverPayload
	if exists{
		room.Mutex.Lock()
		for _,c := range room.Clients{
			if c.Conn !=conn{
				overPayload = GameOverPayload{Winner: c.PlayerIndex}
				break
			}
		}

		jsonBytes,_ :=json.Marshal(overPayload)
		dcMsg:=Message{
			Type: "GAME_OVER",
			Payload: string(jsonBytes),
		}

		for _,c := range room.Clients{
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

func (rm *RoomManager) RemoveFromQueue(conn *websocket.Conn) {
	rm.queueLock.Lock()
	defer rm.queueLock.Unlock()

	for i, c := range rm.matchmakingQueue {
		if c == conn {
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

	// Get all connections
	room.Mutex.Lock()
	connections := make([]*websocket.Conn, 0, len(room.Clients))
	for conn := range room.Clients {
		connections = append(connections, conn)
	}
	room.Mutex.Unlock()

	// Only remove from ClientMap - DON'T close connections!
	// Players stay connected and can immediately find another match
	rm.ClientMapLock.Lock()
	for _, conn := range connections {
		delete(rm.ClientMap, conn)
	}
	rm.ClientMapLock.Unlock()

	fmt.Printf("Room %s deleted. Active games: %d (connections kept alive)\n", roomId, len(rm.rooms))
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
