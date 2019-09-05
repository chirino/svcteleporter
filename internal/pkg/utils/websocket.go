package utils

import (
	"context"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"log"
	"net"
	"time"
)

type websocketNetConn struct {
	*websocket.Conn
	nextReader io.Reader
	ctx        context.Context
	logPrefix  string
}

func (conn *websocketNetConn) Close() error {
	log.Println(conn.logPrefix + "closed")
	return conn.Conn.Close()
}

func (conn *websocketNetConn) next() error {
	mt, r, err := conn.NextReader()
	if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		return io.EOF
	}
	if err != nil {
		return err
	}
	if mt != websocket.BinaryMessage {
		return fmt.Errorf("unexpected websocket message type")
	}
	conn.nextReader = r
	return nil
}

func (conn *websocketNetConn) Read(b []byte) (int, error) {
	err := conn.ctx.Err()
	if err != nil {
		return 0, err
	}
	if conn.nextReader == nil {
		err := conn.next()
		if err != nil {
			return 0, err
		}
	}
	for {
		c, err := conn.nextReader.Read(b)
		if err == io.EOF {
			// log.Println("frame eof")
			err := conn.next()
			if err != nil {
				return 0, err
			}
			continue
		} else {
			// log.Println("frame:", string(b[0:c]))
		}
		return c, err
	}
}

func (conn *websocketNetConn) Write(b []byte) (int, error) {
	err := conn.ctx.Err()
	if err != nil {
		return 0, err
	}
	err = conn.WriteMessage(websocket.BinaryMessage, b)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

func (conn *websocketNetConn) SetDeadline(t time.Time) error {
	err := conn.Conn.SetReadDeadline(t)
	if err != nil {
		return err
	}
	err = conn.Conn.SetWriteDeadline(t)
	if err != nil {
		return err
	}
	return nil
}

func WebSocketToNetConn(ctx context.Context, ws *websocket.Conn, logPrefix string) net.Conn {
	ws.SetCloseHandler(func(code int, text string) error {
		log.Println(logPrefix + "closed")
		return nil
	})
	return &websocketNetConn{Conn: ws, ctx: ctx, logPrefix: logPrefix}
}

func CopyWebSocketIO(ctx context.Context, websocketConnection *websocket.Conn, logPrefix string, sshOut io.Writer, sshIn io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	wsConn := WebSocketToNetConn(ctx, websocketConnection, logPrefix)

	// read from websocket
	go func() {
		defer cancel()
		if _, err := io.Copy(sshOut, wsConn); err != nil {
			log.Println(logPrefix+"read error:", err)
			return
		}
	}()

	_, err := io.Copy(wsConn, sshIn)
	if err == io.EOF {
		if err := websocketConnection.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(10*time.Second)); err == websocket.ErrCloseSent {
		} else if err != nil {
			return fmt.Errorf("sending close message: %v", err)
		} else {
			return nil
		}
	}
	return err
}

type WsListener struct {
	ctx       context.Context
	ch        chan *websocket.Conn
	logPrefix string
}

func (w *WsListener) Offer(ws *websocket.Conn) {
	w.ch <- ws
}

func (w *WsListener) Accept() (net.Conn, error) {
	x, more := <-w.ch
	if more {
		log.Println(w.logPrefix + "accepted a connection")
		conn := WebSocketToNetConn(w.ctx, x, w.logPrefix)
		return conn, nil
	} else {
		return nil, fmt.Errorf("closed")
	}
}

func (w *WsListener) Close() error {
	close(w.ch)
	return nil
}

func (w *WsListener) Addr() net.Addr {
	return w
}

func (w *WsListener) Network() string {
	return "ws"
}

func (w *WsListener) String() string {
	return "ws://"
}

func ToWSListener(ctx context.Context, logPrefix string) *WsListener {
	ch := make(chan *websocket.Conn)
	return &WsListener{ch: ch, ctx: ctx, logPrefix: logPrefix}
}
