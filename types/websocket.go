package types

import (
	"sync"

	"github.com/gorilla/websocket"
)

type WsMessage struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Helper to make Gorilla Websockets threadsafe
type ThreadSafeWriter struct {
	*websocket.Conn
	sync.Mutex
}

func (t *ThreadSafeWriter) WriteJSON(v interface{}) error {
	t.Lock()
	defer t.Unlock()

	return t.Conn.WriteJSON(v)
}
