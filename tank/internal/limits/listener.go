package limits

import (
	"fmt"
	"net"
	"sync"
)

type boundedListener struct {
	net.Listener
	slots  chan struct{}
	closed chan struct{}
	once   sync.Once
}

// Listen bounds accepted TCP connections, including keep-alive and slow-body
// connections which can outlive an HTTP handler's admission slot.
func Listen(address string, maximum int) (net.Listener, error) {
	if maximum < 1 || maximum > 4096 {
		return nil, fmt.Errorf("invalid connection limit")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	return &boundedListener{Listener: listener, slots: make(chan struct{}, maximum), closed: make(chan struct{})}, nil
}
func (l *boundedListener) Accept() (net.Conn, error) {
	select {
	case l.slots <- struct{}{}:
	case <-l.closed:
		return nil, net.ErrClosed
	}
	connection, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	return &boundedConnection{Conn: connection, release: func() { <-l.slots }}, nil
}
func (l *boundedListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return l.Listener.Close()
}

type boundedConnection struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *boundedConnection) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}
