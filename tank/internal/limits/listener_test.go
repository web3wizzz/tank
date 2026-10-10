package limits

import (
	"net"
	"testing"
	"time"
)

func TestListenerBoundsAcceptedConnectionsAndRestoresCapacity(t *testing.T) {
	listener, err := Listen("127.0.0.1:0", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	firstClient, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer firstClient.Close()
	first, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	secondClient, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer secondClient.Close()
	accepted := make(chan net.Conn, 1)
	failures := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			failures <- err
			return
		}
		accepted <- connection
	}()
	select {
	case connection := <-accepted:
		connection.Close()
		t.Fatal("connection limit accepted an extra socket")
	case <-time.After(50 * time.Millisecond):
	}
	first.Close()
	first.Close()
	select {
	case connection := <-accepted:
		connection.Close()
	case err := <-failures:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("closing a socket did not release capacity")
	}
}
func TestListenerCloseUnblocksCapacityWaiters(t *testing.T) {
	listener, err := Listen("127.0.0.1:0", 1)
	if err != nil {
		t.Fatal(err)
	}
	client, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	connection, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	done := make(chan error, 1)
	go func() { _, err := listener.Accept(); done <- err }()
	listener.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("closed listener accepted a connection")
		}
	case <-time.After(time.Second):
		t.Fatal("listener close did not release a blocked accept")
	}
}
