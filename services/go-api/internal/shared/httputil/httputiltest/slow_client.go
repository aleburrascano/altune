package httputiltest

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

const socketBuffer = 64 << 10

func NewServer(t testing.TB, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.Listener = smallBufferListener{srv.Listener}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

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
		return nil, err
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetWriteBuffer(socketBuffer)
	}
	return conn, nil
}
