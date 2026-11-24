package socket

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	// writeWait is the time allowed to write a single message to a client.
	writeWait = 10 * time.Second
	// pongWait is how long we wait for any frame (usually a pong) from the client
	// before treating the connection as dead.
	pongWait = 60 * time.Second
	// pingPeriod must be shorter than pongWait. It also keeps proxied
	// connections (nginx proxy_read_timeout defaults to 60s) from looking idle.
	pingPeriod = 30 * time.Second
	// maxMessageSize caps inbound frames; clients only listen on this socket.
	maxMessageSize = 64 * 1024
)

// client wraps a connection with a write lock, since gorilla/websocket allows
// only one concurrent writer per connection.
type client struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (cl *client) write(data []byte) error {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	_ = cl.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return cl.conn.WriteMessage(websocket.TextMessage, data)
}

var (
	clientsMu sync.RWMutex
	clients   = make(map[*websocket.Conn]*client)
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

func addClient(conn *websocket.Conn) *client {
	cl := &client{conn: conn}
	clientsMu.Lock()
	clients[conn] = cl
	clientsMu.Unlock()
	return cl
}

func removeClient(conn *websocket.Conn) {
	clientsMu.Lock()
	delete(clients, conn)
	clientsMu.Unlock()
	_ = conn.Close()
}

func snapshotClients() []*client {
	clientsMu.RLock()
	defer clientsMu.RUnlock()
	list := make([]*client, 0, len(clients))
	for _, cl := range clients {
		list = append(list, cl)
	}
	return list
}

func (base *socketHandler) Handler(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println("Error upgrading to WebSocket:", err)
		return
	}

	addClient(conn)
	done := make(chan struct{})
	defer func() {
		close(done)
		removeClient(conn)
	}()

	// Any frame from the client, including the pong reply to our ping, pushes
	// the read deadline forward, so active connections are never cut.
	conn.SetReadLimit(maxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	go pingLoop(conn, done)

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			logReadError(err)
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	}
}

// pingLoop sends a ping every pingPeriod until done is closed or a ping fails.
// WriteControl is safe to call concurrently with WriteMessage.
func pingLoop(conn *websocket.Conn, done <-chan struct{}) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				// Closing unblocks ReadMessage in Handler, which cleans up.
				_ = conn.Close()
				return
			}
		}
	}
}

// logReadError stays quiet for normal client departures and only logs
// abnormal ones.
func logReadError(err error) {
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
		return
	}
	if errors.Is(err, net.ErrClosed) {
		return // closed by us (ping failure or broadcast write failure)
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		log.Println("WebSocket client stopped responding to pings:", err)
		return
	}
	if websocket.IsUnexpectedCloseError(err) {
		log.Printf("WebSocket closed unexpectedly: %v", err)
		return
	}
	log.Println("WebSocket Read Error:", err)
}

func (base *socketHandler) Broadcast() {
	for message := range Broadcast {
		data, err := json.Marshal(message)
		if err != nil {
			log.Println("WebSocket broadcast marshal error:", err)
			continue
		}
		// Write outside the map lock so one slow client can't block
		// new connections from registering.
		for _, cl := range snapshotClients() {
			if err := cl.write(data); err != nil {
				log.Println("WebSocket write error:", err)
				removeClient(cl.conn)
			}
		}
	}
}
