package egress

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsensusCountryUsesMajorityAndRejectsTies(t *testing.T) {
	if got := consensusCountry([]string{"jp", "US", "JP", ""}); got != "JP" {
		t.Fatalf("majority = %q", got)
	}
	if got := consensusCountry([]string{"US", "JP"}); got != "" {
		t.Fatalf("tie = %q", got)
	}
	if got := consensusCountry([]string{"uk"}); got != "GB" {
		t.Fatalf("single normalized = %q", got)
	}
	if got := consensusCountry([]string{"", "not-a-country"}); got != "" {
		t.Fatalf("invalid = %q", got)
	}
}

func TestParseCountryKeysReadsPublicProviders(t *testing.T) {
	cases := []struct {
		body string
		keys []string
		want string
	}{
		{body: `{"country":"us"}`, keys: []string{"country"}, want: "US"},
		{body: `{"success":true,"country_code":"de"}`, keys: []string{"country_code"}, want: "DE"},
		{body: `{"success":false,"country_code":"de"}`, keys: []string{"country_code"}, want: ""},
		{body: `{"bogon":true,"country":"us"}`, keys: []string{"country"}, want: ""},
		{body: `{"country_code":"SG","country":"Singapore"}`, keys: []string{"country_code", "country"}, want: "SG"},
	}
	for _, test := range cases {
		if got := parseCountryKeys(test.keys...)([]byte(test.body)); got != test.want {
			t.Fatalf("parse %s = %q, want %q", test.body, got, test.want)
		}
	}
}

func TestLookupExitCountryUsesMajorityOfProviders(t *testing.T) {
	japan := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !strings.Contains(request.URL.Path, "8.8.8.8") {
			t.Errorf("lookup path = %s", request.URL.Path)
		}
		_, _ = response.Write([]byte(`{"country":"JP"}`))
	}))
	defer japan.Close()
	unitedStates := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"country":"US"}`))
	}))
	defer unitedStates.Close()
	failed := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusTooManyRequests)
	}))
	defer failed.Close()
	providers := []exitCountryProvider{
		{url: func(ip string) string { return japan.URL + "/" + ip }, parse: parseCountryKeys("country")},
		{url: func(ip string) string { return japan.URL + "/" + ip }, parse: parseCountryKeys("country")},
		{url: func(ip string) string { return unitedStates.URL + "/" + ip }, parse: parseCountryKeys("country")},
		{url: func(ip string) string { return failed.URL + "/" + ip }, parse: parseCountryKeys("country")},
	}
	client := &http.Client{}
	if got := lookupExitCountryWith(context.Background(), client, providers, "8.8.8.8"); got != "JP" {
		t.Fatalf("lookup = %q", got)
	}
}

func TestLookupExitCountrySkipsNonPublicAddresses(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("non-public address was sent to a geolocation service")
		return nil, nil
	})}
	for _, ip := range []string{"", "127.0.0.1", "10.1.2.3", "198.51.100.11", "2001:db8::11"} {
		if got := lookupExitCountryWith(context.Background(), client, exitCountryProviders, ip); got != "" {
			t.Fatalf("%s lookup = %q", ip, got)
		}
	}
}

func TestMergeObservedCountryKeepsProbeCountryWithoutPublicIP(t *testing.T) {
	if got := mergeObservedCountry(context.Background(), "198.51.100.11", "US"); got != "US" {
		t.Fatalf("documentation address = %q", got)
	}
	if got := mergeObservedCountry(context.Background(), "", "uk"); got != "GB" {
		t.Fatalf("observed country = %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
