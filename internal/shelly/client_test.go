package shelly

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGetRejectsOversizedAndInvalidResponses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body string
		want       error
	}{
		{name: "oversized", body: `{"value":"` + strings.Repeat("x", 40) + `"}`, want: ErrResponseTooLarge},
		{name: "invalid", body: `{"value":`, want: errors.New("invalid")},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &Client{MaxBodyBytes: 24, RequestTimeout: time.Second}
			httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(http.StatusOK, tt.body, nil), nil })}
			_, err := client.get(context.Background(), httpClient, netip.MustParseAddr("192.168.1.4"), "/shelly", nil)
			if err == nil {
				t.Fatal("expected response rejection")
			}
			if errors.Is(tt.want, ErrResponseTooLarge) && !errors.Is(err, ErrResponseTooLarge) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestGetDigestAuthenticationRetriesWithoutLeakingPassword(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var requests int
	var authorization string
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		requests++
		authorization = request.Header.Get("Authorization")
		if requests == 1 {
			return response(http.StatusUnauthorized, "", map[string]string{"WWW-Authenticate": `Digest realm="shelly", nonce="nonce", algorithm=SHA-256, qop="auth"`}), nil
		}
		return response(http.StatusOK, `{"id":"shellyplus1-a","gen":2}`, nil), nil
	})}
	client := &Client{MaxBodyBytes: 1024, RequestTimeout: time.Second}
	_, err := client.get(context.Background(), httpClient, netip.MustParseAddr("192.168.1.4"), "/shelly", &Credential{Username: "admin", Password: "do-not-leak"})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || !strings.HasPrefix(authorization, "Digest ") {
		t.Fatalf("requests=%d auth=%q", requests, authorization)
	}
	if strings.Contains(authorization, "do-not-leak") {
		t.Fatal("digest header contains plaintext password")
	}
}

func TestGetReportsAuthenticationRequiredWithoutCredentials(t *testing.T) {
	t.Parallel()
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusUnauthorized, "", map[string]string{"WWW-Authenticate": `Digest realm="shelly", nonce="nonce"`}), nil
	})}
	client := &Client{MaxBodyBytes: 1024, RequestTimeout: time.Second}
	_, err := client.get(context.Background(), httpClient, netip.MustParseAddr("192.168.1.4"), "/shelly", nil)
	if !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf("got %v", err)
	}
}

func TestGetHonorsClientTimeout(t *testing.T) {
	t.Parallel()
	httpClient := &http.Client{Timeout: 20 * time.Millisecond, Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	client := &Client{MaxBodyBytes: 1024, RequestTimeout: time.Second}
	_, err := client.get(context.Background(), httpClient, netip.MustParseAddr("192.168.1.4"), "/shelly", nil)
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func TestHTTPClientBlocksRedirects(t *testing.T) {
	t.Parallel()
	client := NewClient(time.Second, time.Second).httpClient(netip.MustParseAddr("192.168.1.4"))
	if err := client.CheckRedirect(&http.Request{}, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy = %v", err)
	}
}

func TestProbeGen1AndRPCThroughPinnedDialer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                 string
		info, config, status string
		wantGen              int
	}{
		{name: "gen1", info: `{"type":"SHSW-1","mac":"AABBCCDDEE01","auth":false,"fw":"v1.14.0"}`, config: `{"name":"Garage","device":{"hostname":"shelly1-a"}}`, status: `{"relays":[{"ison":true}]}`, wantGen: 1},
		{name: "gen3", info: `{"id":"shelly1g3-a","model":"S3SW-001X16EU","gen":3,"mac":"AABBCCDDEE03","ver":"1.5.0"}`, config: `{"device":{"name":"Kitchen"}}`, status: `{"switch:0":{"output":true,"apower":4.2}}`, wantGen: 3},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/shelly":
					io.WriteString(w, tt.info)
				case "/settings", "/rpc/Sys.GetConfig":
					io.WriteString(w, tt.config)
				case "/status", "/rpc/Shelly.GetStatus":
					io.WriteString(w, tt.status)
				default:
					http.NotFound(w, r)
				}
			})
			client := NewClient(time.Second, time.Second)
			client.dialContext = pipeDial(handler)
			got, err := client.Probe(context.Background(), netip.MustParseAddr("192.168.1.10"), nil)
			if err != nil {
				t.Fatal(err)
			}
			if got.Generation != tt.wantGen || got.MACAddress == "" {
				t.Fatalf("probe = %+v", got)
			}
		})
	}
}

func TestProbeReturnsPartialIdentityWhenDetailsRequireBasicAuth(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/shelly" {
			io.WriteString(w, `{"type":"SHSW-1","mac":"AABBCCDDEE10","auth":true,"fw":"v1.14.0"}`)
			return
		}
		if r.Header.Get("Authorization") != "Basic YWRtaW46c2VjcmV0" {
			w.Header().Set("WWW-Authenticate", `Basic realm="shelly"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		io.WriteString(w, `{}`)
	})
	client := NewClient(time.Second, time.Second)
	client.dialContext = pipeDial(handler)
	partial, err := client.Probe(context.Background(), netip.MustParseAddr("192.168.1.12"), nil)
	if !errors.Is(err, ErrAuthenticationRequired) || partial.MACAddress != "aabbccddee10" || !partial.AuthenticationRequired {
		t.Fatalf("partial probe = %+v, %v", partial, err)
	}
	client = NewClient(time.Second, time.Second)
	client.dialContext = pipeDial(handler)
	full, err := client.Probe(context.Background(), netip.MustParseAddr("192.168.1.12"), &Credential{Username: "admin", Password: "secret"})
	if err != nil || full.MACAddress != "aabbccddee10" {
		t.Fatalf("authenticated probe = %+v, %v", full, err)
	}
}

func TestProbeRejectsGenericJSON(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, `{"hello":"world"}`) })
	client := NewClient(time.Second, time.Second)
	client.dialContext = pipeDial(handler)
	_, err := client.Probe(context.Background(), netip.MustParseAddr("192.168.1.11"), nil)
	if !errors.Is(err, ErrNotShelly) {
		t.Fatalf("got %v", err)
	}
}

func TestIdentifyUsesOnlyUnauthenticatedShellyEndpoint(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/shelly" {
			t.Errorf("unexpected identity path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatal("Identify sent an authorization header")
		}
		_, _ = io.WriteString(w, `{"id":"shellypro3em-main","model":"SPEM-003CEBEU","gen":2,"mac":"AABBCCDDEEFF"}`)
	})
	client := NewClient(time.Second, time.Second)
	client.dialContext = pipeDial(handler)
	identity, err := client.Identify(context.Background(), netip.MustParseAddr("192.168.1.40"))
	if err != nil {
		t.Fatal(err)
	}
	if identity.ShellyDeviceID != "shellypro3em-main" || identity.MACAddress != "aabbccddeeff" {
		t.Fatalf("identity = %+v", identity)
	}
}

func response(status int, body string, headers map[string]string) *http.Response {
	value := &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
	for key, item := range headers {
		value.Header.Set(key, item)
	}
	return value
}

type oneConnListener struct {
	connection net.Conn
	accepted   bool
}

func (l *oneConnListener) Accept() (net.Conn, error) {
	if l.accepted {
		return nil, io.EOF
	}
	l.accepted = true
	return l.connection, nil
}
func (l *oneConnListener) Close() error   { return nil }
func (l *oneConnListener) Addr() net.Addr { return pipeAddress("pipe") }

type pipeAddress string

func (p pipeAddress) Network() string { return string(p) }
func (p pipeAddress) String() string  { return string(p) }

func pipeDial(handler http.Handler) func(context.Context, string, string) (net.Conn, error) {
	return func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		go http.Serve(&oneConnListener{connection: server}, handler)
		return client, nil
	}
}
