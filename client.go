package socketcast

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	cmap "github.com/orcaman/concurrent-map"
)

type Client struct {
	sync.RWMutex

	Pool *Pool

	// The websocket connection.
	Conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan []byte

	//Metadata of client
	Metadata cmap.ConcurrentMap

	//Auth state of client
	Auth Auth

	Active bool
}

type Auth struct {
	Token         string
	Authenticated bool
}

func (c *Client) readPump() {
	defer func() {
		c.Destroy()
	}()
	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error { c.Conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.Pool.Log.Warnf("UnexpectedCloseError, closing connection: %s", err.Error())
			} else if err.Error() == "websocket: close 1006 (abnormal closure): unexpected EOF" {
				c.Pool.Log.Infof("Client %s disconnected", c.Conn.RemoteAddr().String())
			} else {
				c.Pool.Log.Warn(err)
			}
			break
		}
		close := c.Pool.Config.OnMessage(c, message)
		if close {
			break
		}
		//message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		//c.pool.hub.broadcast <- message
	}
}

func (c *Client) Start() {
	c.Active = true
	c.Pool.hub.register <- c
	go c.readPump()
	go c.writePump()
	c.Pool.Config.OnConnect(c)
}

func (c *Client) Destroy() {
	c.Lock()
	defer c.Unlock()
	c.Pool.Log.Debugf("About to destroy client: %s", c.Conn.RemoteAddr().String())
	c.Pool.hub.unregister <- c
	c.Active = false
	c.Conn.Close()
	c.Pool.Log.Infof("Destroyed client: %s", c.Conn.RemoteAddr().String())
	c.Pool.Config.OnDisconnect(c)
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		c.Lock()
		defer c.Unlock()
		ticker.Stop()
		c.Conn.Close()
		c.Active = false
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued chat messages to the current websocket message.
			// n := len(c.send)
			// for i := 0; i < n; i++ {
			// 	w.Write(newline)
			// 	w.Write(<-c.send)
			// }

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) Send(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		c.Pool.Log.Error("Trying to convert to json but failed: the value is:%v", msg)
		return err
	}
	c.send <- data
	return nil
}

func newClient(pool *Pool, conn *websocket.Conn) *Client {
	pool.Log.Debug("New client is being created")
	c := &Client{Pool: pool, Conn: conn, send: make(chan []byte, pool.Config.Buffers.Send), Metadata: cmap.New()}
	if !pool.Config.DisableClientAutostart {
		c.Start()
	}
	return c
}
