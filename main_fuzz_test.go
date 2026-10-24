package main

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"syscall"
	"testing"
	"time"
)

var httpMethods = []string{"OPTIONS", "GET", "HEAD", "POST", "PUT", "DELETE", "TRACE", "CONNECT"}

var (
	portOnce sync.Once
	portAddr string
)

// startSplitPort serves splitPort on a loopback port. HTTP gets a small mux;
// the Stratum side reads until EOF and echoes "stratum:" plus what it read.
func startSplitPort(t testing.TB) string {
	portOnce.Do(func() {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		m, httpL, stratumL := splitPort(l)
		mux := http.NewServeMux()
		mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"ready":true}`)) })
		go func() { _ = (&http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}).Serve(httpL) }()
		go func() {
			for {
				c, err := stratumL.Accept()
				if err != nil {
					return
				}
				go func(c net.Conn) {
					defer c.Close()
					b, _ := io.ReadAll(c)
					_, _ = c.Write(append([]byte("stratum:"), b...))
				}(c)
			}
		}()
		go func() { _ = m.Serve() }()
		portAddr = l.Addr().String()
	})
	return portAddr
}

func wantHTTP(data []byte) bool {
	for _, m := range httpMethods {
		if bytes.HasPrefix(data, []byte(m)) {
			return true
		}
	}
	return false
}

// FuzzSplitPort checks that any first bytes reach exactly one of HTTP or Stratum,
// that Stratum gets them unchanged, and that the connection ends once the client
// stops sending.
func FuzzSplitPort(f *testing.F) {
	for _, s := range []string{
		"",
		"G",
		"GE",
		"GET",
		"GET / HTTP/1.1\r\nHost: localhost\r\n\r\n",
		"GET /api/status HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n",
		"GET /metrics HTTP/1.0\r\n\r\n",
		"POST /api/shutdown HTTP/1.1\r\nHost: localhost\r\nContent-Length: 0\r\n\r\n",
		"HEAD / HTTP/1.1\r\n\r\n",
		"OPTIONS * HTTP/1.1\r\n\r\n",
		"PATCH / HTTP/1.1\r\n\r\n",
		"get / HTTP/1.1\r\n\r\n",
		"GET000\n00",
		"\x16\x03\x01\x00\xa5\x01\x00\x00\xa1\x03\x03",
		`{"id":1,"method":"mining.subscribe","params":["VerthashMiner/0.7.2"]}` + "\n",
		`{"id":2,"method":"mining.authorize","params":["vtc1qxyz.rig1","x"]}` + "\n",
		`{"id":3,"method":"mining.submit","params":["vtc1qxyz","1","00000000","6533d1a2","1a2b3c4d"]}` + "\n",
		"\x00\x00\x00\x00",
	} {
		f.Add([]byte(s))
	}
	addr := startSplitPort(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err := c.Write(data); err != nil {
			t.Fatalf("write: %v", err)
		}
		_ = c.(*net.TCPConn).CloseWrite()
		got, err := io.ReadAll(c)
		if wantHTTP(data) {
			// net/http resets a connection it closes with unread request bytes.
			if err != nil && !errors.Is(err, syscall.ECONNRESET) {
				t.Fatalf("connection did not end after the client finished sending: %v", err)
			}
			if len(got) > 0 && !bytes.HasPrefix(got, []byte("HTTP/1.")) {
				t.Fatalf("HTTP request %q got non-HTTP reply %q", data, got)
			}
			return
		}
		if err != nil {
			t.Fatalf("connection did not end after the client finished sending: %v", err)
		}
		if want := append([]byte("stratum:"), data...); !bytes.Equal(got, want) {
			t.Fatalf("Stratum connection %q got %q", data, got)
		}
	})
}
