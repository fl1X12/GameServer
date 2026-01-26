package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	manager := NewRoomManager()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("Upgrade error:", err)
			return
		}

		defer func() {
			manager.HandleDisconnect(conn) // ✅ Full cleanup
			conn.Close()
		}()

		fmt.Println("Client Connected:", conn.RemoteAddr())

		for {

			_, msgBytes, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Println("WebSocket error:", err)
				}
				break
			}

			var msg Message
			if err := json.Unmarshal(msgBytes, &msg); err != nil {
				log.Println("Invalid message format:", err)
				continue
			}
			fmt.Println(msg)

			switch msg.Type {
			case "find_match":
				fmt.Println("Running find match for", conn.RemoteAddr())
				manager.HandleFindMatch(conn)

			case "make_move":
				manager.HandleMove(conn, msg)

			default:
				log.Println("Unknown message type:", msg.Type)
			}
		}
	})

	fmt.Println("Server started on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
