package sniffer

import (
	"fmt"
	"net"
	"testing"
	"time"
)

// TestCaptureLoopback verifies the full capture pipeline end to end:
// device open, BPF filter, packet read and TCP reassembly — using loopback
// traffic on a dedicated test port, so no game client is needed.
// Requires Npcap; skipped under `go test -short`.
func TestCaptureLoopback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping capture test (needs Npcap); run without -short")
	}

	const testPort = 13329 // not the real game port, avoids clashing with a client

	s := New(Options{Port: testPort})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", testPort))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 32*1024)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					if _, err := c.Write(buf[:n/2+1]); err != nil {
						return
					}
				}
			}(c)
		}
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	go func() {
		payload := make([]byte, 256)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			if _, err := conn.Write(payload); err != nil {
				return
			}
		}
	}()

	deadline := time.After(10 * time.Second)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			st := s.Stats()
			t.Fatalf("timed out waiting for captured traffic: %+v", st)
		case <-tick.C:
			st := s.Stats()
			if st.TrafficSeen && st.Packets > 0 && st.PayloadBytes > 0 && st.Streams >= 2 {
				t.Logf("capture OK: packets=%d bytes=%d payload=%d streams=%d gaps=%d",
					st.Packets, st.Bytes, st.PayloadBytes, st.Streams, st.Gaps)
				return
			}
		}
	}
}
