package chat

import (
	"context"

	"github.com/gorilla/websocket"
)

// 用途：固定与原 ChatRoom 消费一致的单一帧结构。
type Frame struct {
	Type int `json:"type"`
	Data any `json:"data"`
}

// 用途：保存每条连接的浏览器归属与独立有界发送队列。
type Client struct {
	Conn       *websocket.Conn
	VisitorKey string
	Send       chan Frame
	ipAddress  string
	ipSource   string
}

// 用途：在单个事件循环内拥有在线连接及有序广播。
type Hub struct {
	Register   chan *Client
	Unregister chan *Client
	Broadcast  chan Frame
	Clients    map[*Client]struct{}
	work       chan hubWork
}

type hubWork struct {
	ctx   context.Context
	run   func(context.Context) error
	reply chan error
}

// 用途：驱动独立连接的注册/注销/广播，不让单个慢连接阻塞其他人。
func (h *Hub) Run(ctx context.Context) error {
	if h.Clients == nil {
		h.Clients = make(map[*Client]struct{})
	}
	defer func() {
		for client := range h.Clients {
			h.drop(client)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case client := <-h.Register:
			h.register(client)
		case client := <-h.Unregister:
			if h.drop(client) {
				h.online()
			}
		case frame := <-h.Broadcast:
			h.broadcast(frame)
		case work := <-h.work:
			if err := work.ctx.Err(); err != nil {
				work.reply <- err
			} else {
				work.reply <- work.run(work.ctx)
			}
		}
	}
}

func (h *Hub) register(client *Client) {
	if client == nil || client.Send == nil {
		return
	}
	if _, exists := h.Clients[client]; exists {
		return
	}
	h.Clients[client] = struct{}{}
	h.online()
}
func (h *Hub) drop(client *Client) bool {
	if _, exists := h.Clients[client]; !exists {
		return false
	}
	delete(h.Clients, client)
	close(client.Send)
	if client.Conn != nil {
		_ = client.Conn.Close()
	}
	return true
}
func (h *Hub) send(client *Client, frame Frame) bool {
	if _, exists := h.Clients[client]; !exists {
		return false
	}
	select {
	case client.Send <- frame:
		return true
	default:
		h.drop(client)
		return false
	}
}
func (h *Hub) broadcast(frame Frame) {
	dropped := false
	for client := range h.Clients {
		if !h.send(client, frame) {
			dropped = true
		}
	}
	if dropped {
		h.online()
	}
}
func (h *Hub) online() {
	// A count update can itself fill a slow peer's queue. Repeat only when a
	// peer was removed, so survivors always receive the final real count.
	for {
		dropped := false
		frame := Frame{Type: 1, Data: len(h.Clients)}
		for client := range h.Clients {
			if !h.send(client, frame) {
				dropped = true
			}
		}
		if !dropped {
			return
		}
	}
}
