package buildtransport

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConfigureHTTP2HealthEnablesActivePing(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	transport := &http.Transport{
		ForceAttemptHTTP2: true,
		TLSClientConfig:   server.Client().Transport.(*http.Transport).TLSClientConfig.Clone(),
	}
	defer transport.CloseIdleConnections()
	h2, err := ConfigureHTTP2Health(transport)
	if err != nil {
		t.Fatal(err)
	}
	if h2.ReadIdleTimeout != HTTP2ReadIdleTimeout || h2.PingTimeout != HTTP2PingTimeout {
		t.Fatalf("HTTP/2 health = (%s, %s)", h2.ReadIdleTimeout, h2.PingTimeout)
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.ProtoMajor != 2 {
		t.Fatalf("negotiated %s, want HTTP/2", response.Proto)
	}
}

func TestConfigureHTTP2HealthRejectsNilTransport(t *testing.T) {
	if _, err := ConfigureHTTP2Health(nil); err == nil {
		t.Fatal("nil transport was accepted")
	}
}
