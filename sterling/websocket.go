package main

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	wsContinuation byte = 0x0
	wsText         byte = 0x1
	wsBinary       byte = 0x2
	wsClose        byte = 0x8
	wsPing         byte = 0x9
	wsPong         byte = 0xa

	wsMaxMessageSize = 64 << 20
	websocketGUID    = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

type wsConn struct {
	conn   net.Conn
	reader *bufio.Reader

	mu     sync.Mutex
	closed bool
}

func isWebSocketRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") &&
		r.Header.Get("Sec-WebSocket-Key") != ""
}

func upgradeWebSocket(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "WebSocket upgrade is not supported", http.StatusInternalServerError)
		return nil, fmt.Errorf("response writer does not support hijacking")
	}

	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}

	key := r.Header.Get("Sec-WebSocket-Key")
	accept := base64.StdEncoding.EncodeToString(sha1Sum(key + websocketGUID))
	if _, err := fmt.Fprintf(rw,
		"HTTP/1.1 101 Switching Protocols\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Accept: %s\r\n\r\n", accept); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	return &wsConn{conn: conn, reader: rw.Reader}, nil
}

func sha1Sum(value string) []byte {
	sum := sha1.Sum([]byte(value))
	return sum[:]
}

func (c *wsConn) readMessage() (byte, []byte, error) {
	var message []byte
	var messageOp byte

	for {
		header := make([]byte, 2)
		if _, err := io.ReadFull(c.reader, header); err != nil {
			return 0, nil, err
		}

		fin := header[0]&0x80 != 0
		op := header[0] & 0x0f
		masked := header[1]&0x80 != 0
		length := int64(header[1] & 0x7f)

		switch length {
		case 126:
			var extended [2]byte
			if _, err := io.ReadFull(c.reader, extended[:]); err != nil {
				return 0, nil, err
			}
			length = int64(binary.BigEndian.Uint16(extended[:]))
		case 127:
			var extended [8]byte
			if _, err := io.ReadFull(c.reader, extended[:]); err != nil {
				return 0, nil, err
			}
			length = int64(binary.BigEndian.Uint64(extended[:]))
		}

		if length < 0 || length > wsMaxMessageSize {
			return 0, nil, fmt.Errorf("WebSocket message is too large")
		}

		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(c.reader, mask[:]); err != nil {
				return 0, nil, err
			}
		}

		payload := make([]byte, int(length))
		if _, err := io.ReadFull(c.reader, payload); err != nil {
			return 0, nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%len(mask)]
			}
		}

		switch op {
		case wsText, wsBinary:
			messageOp = op
			message = payload
		case wsContinuation:
			message = append(message, payload...)
		case wsClose:
			_ = c.writeFrame(wsClose, nil)
			return 0, nil, io.EOF
		case wsPing:
			_ = c.writeFrame(wsPong, payload)
			continue
		case wsPong:
			continue
		default:
			continue
		}

		if fin {
			return messageOp, message, nil
		}
	}
}

func (c *wsConn) writeText(message string) error {
	return c.writeFrame(wsText, []byte(message))
}

func (c *wsConn) writeFrame(op byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return net.ErrClosed
	}

	header := make([]byte, 0, 10)
	header = append(header, 0x80|op)
	length := len(payload)
	switch {
	case length < 126:
		header = append(header, byte(length))
	case length <= 65535:
		header = append(header, 126, byte(length>>8), byte(length))
	default:
		header = append(header, 127)
		var extended [8]byte
		binary.BigEndian.PutUint64(extended[:], uint64(length))
		header = append(header, extended[:]...)
	}

	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	if length == 0 {
		return nil
	}
	_, err := c.conn.Write(payload)
	return err
}

func (c *wsConn) close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}
	c.closed = true
	_ = c.conn.SetWriteDeadline(time.Now().Add(time.Second))
	_, _ = c.conn.Write([]byte{0x80 | wsClose, 0x00})
	_ = c.conn.Close()
}
