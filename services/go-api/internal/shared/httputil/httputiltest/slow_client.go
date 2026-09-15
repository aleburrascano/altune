// Package httputiltest drives real HTTP servers with slow-reading clients, so
// write-deadline behavior is exercised against actual TCP back-pressure rather
// than a mocked ResponseWriter.
package httputiltest

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// socketBuffer is the kernel buffer size requested on both ends. Small buffers
// make a client that stops reading block the server's Write within a few
// hundred KB, instead of after the megabytes loopback autotuning would absorb.
// Much smaller (a few KB) collapses loopback TCP throughput and makes timing
// flaky.
const socketBuffer = 64 << 10

// NewServer starts h on a real loopback server whose accepted connections have
// a small send buffer. The server is closed on test cleanup.
func NewServer(t testing.TB, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.Listener = smallBufferListener{srv.Listener}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

// Get opens a raw TCP connection with a small receive buffer to srv and sends a
// GET for path with header. The caller decides how (and whether) to read; the
// connection is closed on test cleanup, before the server is.
func Get(t testing.TB, srv *httptest.Server, path string, header http.Header) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetReadBuffer(socketBuffer)
	}
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if err := req.Write(conn); err != nil {
		t.Fatalf("write request: %v", err)
	}
	return conn, bufio.NewReaderSize(conn, socketBuffer)
}

// ReadResponse parses the response head from r, read off conn, for a GET
// request. Closing the returned body closes conn rather than draining the body,
// so it is safe on never-ending streams (SSE).
func ReadResponse(t testing.TB, conn net.Conn, r *bufio.Reader) *http.Response {
	t.Helper()
	resp, err := http.ReadResponse(r, &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	resp.Body = connBody{Reader: resp.Body, conn: conn}
	return resp
}

type connBody struct {
	io.Reader
	conn net.Conn
}

func (b connBody) Close() error { return b.conn.Close() }

type smallBufferListener struct{ net.Listener }

func (l smallBufferListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		// Returned unwrapped: http.Server inspects Accept errors by type.
		return nil, err
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetWriteBuffer(socketBuffer)
	}
	return conn, nil
}
