package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"sync"

	"synest-sfu/types"

	"github.com/gorilla/websocket"
)

// nolint
var (
	addr     = flag.String("addr", ":8080", "http service address")
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	coordinator = types.NewCoordinator()
)

func main() {
	// Parse the flags passed to program
	flag.Parse()

	// websocket handler
	http.HandleFunc("/", websocketHandler)

	// start HTTP server
	fmt.Println("Started server with addr", *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil { //nolint: gosec
		fmt.Println("Failed to start http server: ", err)
	}
}

// Handle incoming websockets
func websocketHandler(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP request to Websocket
	unsafeConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("Failed to upgrade HTTP to Websocket: ", err)
		return
	}

	c := &types.ThreadSafeWriter{Conn: unsafeConn, Mutex: sync.Mutex{}}

	// When this frame returns close the Websocket
	defer c.Close() //nolint

	message := &types.WsMessage{}
	for {
		_, raw, err := unsafeConn.ReadMessage()
		if err != nil {
			fmt.Println("Failed to read message: ", err)
			return
		}

		if err := json.Unmarshal(raw, message); err != nil {
			fmt.Println("Failed to unmarshal json to message: ", err)
			return
		}

		coordinator.HandleEvent(*message, c)
	}
}
