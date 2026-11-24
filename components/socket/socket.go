package socket

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/TechAlkurn/core/config"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
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

// Broadcast consumes the Broadcast channel and sends each message to every
// connected client across all instances of the app.
//
// When Redis is configured (config.InitRedis called and REDIS_ADDR set), each
// message is published to a Redis channel and every instance, this one
// included, delivers it to its own clients. That way a message sent by the
// instance handling a webhook reaches clients connected to any other instance
// behind the load balancer. Without Redis, or if a publish fails, the message
// goes only to this instance's clients.
func (base *socketHandler) Broadcast() {
	rdb := redisClient()
	if rdb != nil {
		startSubscriber(rdb)
	}
	for message := range Broadcast {
		data, err := json.Marshal(message)
		if err != nil {
			log.Println("WebSocket broadcast marshal error:", err)
			continue
		}
		if rdb != nil {
			ctx, cancel := context.WithTimeout(context.Background(), writeWait)
			err = rdb.Publish(ctx, redisChannel(), data).Err()
			cancel()
			if err == nil {
				continue // delivered locally by the subscriber
			}
			log.Println("WebSocket redis publish failed, delivering to local clients only:", err)
		}
		deliverLocal(data)
	}
}

// redisClient returns the shared Redis client, or nil when Redis isn't set up.
// config.Redis is created even with an empty REDIS_ADDR (go-redis then dials
// localhost:6379), so the env var decides whether Redis is meant to be used.
func redisClient() *redis.Client {
	if config.Redis == nil || os.Getenv("REDIS_ADDR") == "" {
		return nil
	}
	return config.Redis
}

// redisChannel is the pub/sub channel for broadcasts. It is namespaced per app
// so apps sharing one Redis server don't receive each other's messages.
// SOCKET_REDIS_CHANNEL overrides it; otherwise it is derived from DB_NAME,
// falling back to SITE_NAME.
func redisChannel() string {
	if ch := os.Getenv("SOCKET_REDIS_CHANNEL"); ch != "" {
		return ch
	}
	name := os.Getenv("DB_NAME")
	if name == "" {
		name = os.Getenv("SITE_NAME")
	}
	return "core:socket:broadcast:" + name
}

var subscribeOnce sync.Once

// startSubscriber delivers messages from the Redis channel to this instance's
// clients. It runs once per process, even if Broadcast is started twice, so
// clients never get duplicates. go-redis reconnects the subscription on its
// own; messages published while it is disconnected are not replayed.
func startSubscriber(rdb *redis.Client) {
	subscribeOnce.Do(func() {
		channel := redisChannel()
		ctx, cancel := context.WithTimeout(context.Background(), writeWait)
		if err := rdb.Ping(ctx).Err(); err != nil {
			log.Printf("WebSocket redis unreachable at startup (%v); will keep retrying on %q", err, channel)
		}
		cancel()

		pubsub := rdb.Subscribe(context.Background(), channel)
		go func() {
			for msg := range pubsub.Channel() {
				deliverLocal([]byte(msg.Payload))
			}
		}()
	})
}

// deliverLocal sends data to the clients connected to this instance.
// It writes outside the map lock so one slow client can't block new
// connections from registering.
func deliverLocal(data []byte) {
	for _, cl := range snapshotClients() {
		if err := cl.write(data); err != nil {
			log.Println("WebSocket write error:", err)
			removeClient(cl.conn)
		}
	}
}
