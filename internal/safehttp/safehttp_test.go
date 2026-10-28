package safehttp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The classifier is the crux of the SSRF guard: every internal/sensitive range must be blocked
// (especially the cloud metadata endpoint 169.254.169.254), and public addresses must be allowed so
// legitimate external importer/webhook URLs still work.
func TestBlockedClassifier(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"169.254.169.254", true},       // cloud metadata — the crown-jewel SSRF target
		{"127.0.0.1", true},             // loopback
		{"::1", true},                   // loopback v6
		{"10.0.0.5", true},              // RFC1918
		{"172.16.3.4", true},            // RFC1918
		{"192.168.1.1", true},           // RFC1918
		{"fd00::1", true},               // ULA
		{"fe80::1", true},               // link-local v6
		{"0.0.0.0", true},               // unspecified
		{"100.100.100.200", true},       // CGNAT 100.64/10 — Alibaba Cloud's metadata endpoint, and tailnets
		{"100.64.0.1", true},            // CGNAT
		{"0.1.2.3", true},               // 0.0.0.0/8 "this network"
		{"192.0.0.170", true},           // IETF protocol assignments
		{"198.18.0.1", true},            // benchmarking
		{"240.0.0.1", true},             // reserved
		{"255.255.255.255", true},       // broadcast
		{"::ffff:127.0.0.1", true},      // IPv4-mapped loopback
		{"64:ff9b::a9fe:a9fe", true},    // NAT64 of 169.254.169.254
		{"64:ff9b::7f00:1", true},       // NAT64 of 127.0.0.1
		{"64:ff9b:1::a00:5", true},      // local-use NAT64 prefix
		{"2606:4700:4700::1111", false}, // public v6
		{"8.8.8.8", false},              // public — must be allowed
		{"1.1.1.1", false},              // public
		{"93.184.216.34", false},        // public (example.com)
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("bad test IP %q", c.ip)
		}
		if got := blocked(ip); got != c.want {
			t.Errorf("blocked(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}

// A proxy from the environment would dial the proxy, not the target, so the target would never be
// checked: the client must ignore HTTP(S)_PROXY.
func TestClientIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://8.8.8.8:3128")
	t.Setenv("HTTP_PROXY", "http://8.8.8.8:3128")
	tr := Client(time.Second).Transport.(*http.Transport)
	if tr.Proxy != nil {
		req, _ := http.NewRequest(http.MethodGet, "http://169.254.169.254/", nil)
		if u, _ := tr.Proxy(req); u != nil {
			t.Fatalf("request to the metadata address would go via proxy %s, unchecked", u)
		}
	}
}

// A public URL that redirects to a loopback address is refused at the redirect hop.
func TestRedirectToLoopbackIsRefused(t *testing.T) {
	inner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer inner.Close()
	c := Client(5 * time.Second)
	// The first hop is served by a test transport so it can stand in for a public host; the redirect
	// target is then dialled through the guarded transport.
	guarded := c.Transport
	c.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "public.example" {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {inner.URL}},
				Body: http.NoBody, Request: r}, nil
		}
		return guarded.RoundTrip(r)
	})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://public.example/", nil)
	resp, err := c.Do(req)
	if err == nil {
		resp.Body.Close()
		t.Fatal("redirect to a loopback address was followed")
	}
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("err = %v, want ErrBlockedAddress", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
