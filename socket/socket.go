package components

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var (
	clients   = make(map[*websocket.Conn]bool)
	Broadcast = make(chan map[string]any)
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type SocketHandler interface {
	Handler(c *gin.Context)
	Broadcast()
}

type socketHandler struct{}

func NewSocketHandler() SocketHandler {
	return &socketHandler{}
}

func (base *socketHandler) Handler(c *gin.Context) {
	//	config.Log.Println("WebSocket connection established")
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println("Error upgrading to WebSocket:", err)
		return
	}

	// Set timeouts for read/write operations if needed
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Minute))  // Example: extend read timeout to 10 minutes
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Minute)) // Example: extend write timeout to 10 minutes

	// Register the new client
	clients[conn] = true
	defer func() {
		// Remove the client when the function returns
		delete(clients, conn)
		conn.Close()
	}()
	// Create a context for this WebSocket connection
	_, cancel := context.WithCancel(c) // Create a cancelable context
	defer cancel()                     // Ensure we cancel the context on function return

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("WebSocket closed unexpectedly: %v", err)
			} else {
				log.Println("WebSocket Read Error:", err)
			}
			break
		}
	}
}

func (base *socketHandler) Broadcast() {
	for {
		// Wait for a new notification message
		message := <-Broadcast
		data, err := json.Marshal(message)
		if err != nil {
			log.Print(err)
		}
		for client := range clients {
			err := client.WriteMessage(websocket.TextMessage, []byte(data))
			if err != nil {
				log.Println("WebSocket write error:", err)
				// Remove the client if there is an error
				client.Close()
				delete(clients, client)
			}
		}
	}
}
