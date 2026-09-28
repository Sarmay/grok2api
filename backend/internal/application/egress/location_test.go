package egress

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDetectProxyLocationFromLabelsAndHosts(t *testing.T) {
	cases := []struct {
		label string
		host  string
		want  string
	}{
		{label: "香港 01", want: "HK"},
		{label: "HK-BGP", want: "HK"},
		{label: "HKG 家宽", want: "HK"},
		{label: "Hong Kong 02", want: "HK"},
		{label: "🇭🇰 IEPL", want: "HK"},
		{label: "东京 01", host: "hk1.provider.net", want: "JP"},
		{label: "美西 02", want: "US"},
		{label: "新加坡-01", want: "SG"},
		{label: "台湾 01", want: "TW"},
		{label: "NHK World", host: "shark.example.com", want: ""},
		{label: "普通节点", host: "hk01.provider.net", want: "HK"},
		{label: "", host: "jp-1.example.com", want: "JP"},
		{label: "checkout relay", host: "edge.example.com", want: ""},
	}
	for _, test := range cases {
		if got := detectProxyLocation(test.label, test.host); got != test.want {
			t.Fatalf("detect(%q, %q) = %q, want %q", test.label, test.host, got, test.want)
		}
	}
}

func TestParseProxySubscriptionRecordsLocation(t *testing.T) {
	vmess := "vmess://" + base64.RawStdEncoding.EncodeToString([]byte(`{"v":"2","ps":"香港 01","add":"proxy.example","port":"443","id":"123e4567-e89b-12d3-a456-426614174000","aid":"0","scy":"auto","net":"tcp"}`))
	entries, skipped, err := parseProxySubscription(strings.Join([]string{
		vmess,
		"trojan://password@jp.example:443#东京-01",
		"http://user:pass@sg.example:8080",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || len(entries) != 3 {
		t.Fatalf("entries=%#v skipped=%d", entries, skipped)
	}
	if entries[0].Location != "HK" || entries[1].Location != "JP" || entries[2].Location != "SG" {
		t.Fatalf("locations=%q %q %q", entries[0].Location, entries[1].Location, entries[2].Location)
	}
	if strings.Contains(entries[1].ProxyURL, "#") || strings.Contains(entries[1].ProxyURL, "东京") {
		t.Fatalf("remark leaked into proxy URL: %s", entries[1].ProxyURL)
	}
}

func TestParseClashSubscriptionRecordsProxyNameLocation(t *testing.T) {
	content := `
proxies:
  - name: 香港 01
    type: http
    server: http.example
    port: 8080
  - name: 大阪 01
    type: socks5
    server: socks.example
    port: 1080
`
	entries, skipped, matched := parseClashSubscription(content)
	if !matched || skipped != 0 || len(entries) != 2 {
		t.Fatalf("entries=%#v skipped=%d matched=%v", entries, skipped, matched)
	}
	if entries[0].Location != "HK" || entries[1].Location != "JP" {
		t.Fatalf("locations=%q %q", entries[0].Location, entries[1].Location)
	}
}

func TestSelectSubscriptionEntriesDropsHongKong(t *testing.T) {
	entries := []subscriptionEntry{{Location: "HK"}, {Location: "JP"}, {Location: ""}}
	kept, skipped := selectSubscriptionEntries(entries, true)
	if skipped != 1 || len(kept) != 2 || kept[0].Location != "JP" || kept[1].Location != "" {
		t.Fatalf("kept=%#v skipped=%d", kept, skipped)
	}
	kept, skipped = selectSubscriptionEntries(entries, false)
	if skipped != 0 || len(kept) != 3 {
		t.Fatalf("unfiltered kept=%d skipped=%d", len(kept), skipped)
	}
}
