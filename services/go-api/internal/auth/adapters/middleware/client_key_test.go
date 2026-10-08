package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientKey(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		forwarded  []string
		want       string
	}{
		{name: "public peer", remoteAddr: "203.0.113.7:5000", want: "203.0.113.7"},
		{name: "public peer ignores spoofed forwarded header", remoteAddr: "203.0.113.7:5000", forwarded: []string{"198.51.100.1"}, want: "203.0.113.7"},
		{name: "private proxy peer uses last forwarded hop", remoteAddr: "172.18.0.3:5000", forwarded: []string{"10.9.9.9, 198.51.100.1"}, want: "198.51.100.1"},
		{name: "loopback proxy uses last forwarded header line", remoteAddr: "127.0.0.1:5000", forwarded: []string{"1.1.1.1", "198.51.100.2"}, want: "198.51.100.2"},
		{name: "private peer with garbage forwarded falls back to peer", remoteAddr: "172.18.0.3:5000", forwarded: []string{"not-an-ip"}, want: "172.18.0.3"},
		{name: "ipv6 keyed by /64", remoteAddr: "[2001:db8:1:2:aaaa::1]:5000", want: "2001:db8:1:2::/64"},
		{name: "ipv6 neighbours in the same /64 share a key", remoteAddr: "[2001:db8:1:2:bbbb::9]:5000", want: "2001:db8:1:2::/64"},
		{name: "ipv4-mapped ipv6 keyed as ipv4", remoteAddr: "[::ffff:203.0.113.7]:5000", want: "203.0.113.7"},
		{name: "unparseable remote addr used verbatim", remoteAddr: "pipe", want: "pipe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for _, v := range tt.forwarded {
				req.Header.Add("X-Forwarded-For", v)
			}
			if got := clientKey(req); got != tt.want {
				t.Errorf("clientKey: got %q, want %q", got, tt.want)
			}
		})
	}
}
