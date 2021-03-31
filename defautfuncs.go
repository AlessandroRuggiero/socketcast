package socketcast

import "net/http"

func defOnMessage(c *Client, msg []byte) bool {
	c.Pool.Log.Debug(string(msg))
	return false
}
func defCeckOrigin(r *http.Request) bool { return true }
func defOnConnect(c *Client) {
	c.Pool.Log.Info("%s connesso", c.Conn.RemoteAddr().String())
}
