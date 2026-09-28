package egress

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	domain "github.com/chenyme/grok2api/backend/internal/domain/egress"
	"github.com/chenyme/grok2api/backend/internal/pkg/netguard"
	"golang.org/x/sync/singleflight"
)

const (
	exitCountryLookupTimeout = 5 * time.Second
	exitCountryCacheTTL      = 12 * time.Hour
	exitCountryMissTTL       = 10 * time.Minute
	exitCountryCacheLimit    = 4096
	exitCountryBodyLimit     = 32 << 10
)

// exitCountryClient queries fixed public geolocation endpoints directly. The
// exit IP is already known, so these requests must not go through the proxy.
var exitCountryClient = &http.Client{
	Timeout: exitCountryLookupTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 3 * time.Second,
		MaxIdleConns:          8,
		IdleConnTimeout:       30 * time.Second,
	},
}

type exitCountryProvider struct {
	url   func(ip string) string
	parse func([]byte) string
}

var exitCountryProviders = []exitCountryProvider{
	{url: func(ip string) string { return "https://ipinfo.io/" + ip + "/json" }, parse: parseCountryKeys("country")},
	{url: func(ip string) string { return "https://ipwho.is/" + ip }, parse: parseCountryKeys("country_code")},
	{url: func(ip string) string { return "https://get.geojs.io/v1/ip/geo/" + ip + ".json" }, parse: parseCountryKeys("country_code", "country")},
	{url: func(ip string) string { return "https://api.country.is/" + ip }, parse: parseCountryKeys("country")},
}

type exitCountryCacheItem struct {
	code    string
	expires time.Time
}

var (
	exitCountryFlight singleflight.Group
	exitCountryMu     sync.Mutex
	exitCountryCache  = map[string]exitCountryCacheItem{}
)

func (m *Manager) LookupExitCountry(ctx context.Context, ip string) string {
	if m != nil && m.exitCountryLookup != nil {
		return m.exitCountryLookup(ctx, ip)
	}
	return cachedExitCountry(ctx, ip)
}

func mergeObservedCountry(ctx context.Context, ip, observed string) string {
	observed = domain.NormalizeCountryCode(observed)
	if lookedUp := cachedExitCountry(ctx, ip); lookedUp != "" {
		return lookedUp
	}
	return observed
}

func cachedExitCountry(ctx context.Context, ip string) string {
	address, ok := publicExitIP(ip)
	if !ok {
		return ""
	}
	key := address.String()
	if code, hit := loadExitCountryCache(key); hit {
		return code
	}
	value, _, _ := exitCountryFlight.Do(key, func() (any, error) {
		if code, hit := loadExitCountryCache(key); hit {
			return code, nil
		}
		code := lookupExitCountryWith(ctx, exitCountryClient, exitCountryProviders, key)
		if ctx.Err() != nil && code == "" {
			return "", nil
		}
		storeExitCountryCache(key, code)
		return code, nil
	})
	code, _ := value.(string)
	return code
}

func lookupExitCountryWith(ctx context.Context, client *http.Client, providers []exitCountryProvider, ip string) string {
	address, ok := publicExitIP(ip)
	if !ok || client == nil || len(providers) == 0 {
		return ""
	}
	ip = address.String()
	ctx, cancel := context.WithTimeout(ctx, exitCountryLookupTimeout)
	defer cancel()
	samples := make([]string, len(providers))
	var wg sync.WaitGroup
	for index, provider := range providers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			samples[index] = queryExitCountry(ctx, client, provider, ip)
		}()
	}
	wg.Wait()
	return consensusCountry(samples)
}

func queryExitCountry(ctx context.Context, client *http.Client, provider exitCountryProvider, ip string) string {
	if provider.url == nil || provider.parse == nil {
		return ""
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.url(ip), nil)
	if err != nil {
		return ""
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", DefaultUserAgent)
	response, err := client.Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, exitCountryBodyLimit))
	if err != nil {
		return ""
	}
	return provider.parse(body)
}

func parseCountryKeys(keys ...string) func([]byte) string {
	return func(body []byte) string {
		var payload map[string]any
		if json.Unmarshal(body, &payload) != nil {
			return ""
		}
		if success, ok := payload["success"].(bool); ok && !success {
			return ""
		}
		if bogon, ok := payload["bogon"].(bool); ok && bogon {
			return ""
		}
		for _, key := range keys {
			value, _ := payload[key].(string)
			if code := domain.NormalizeCountryCode(value); code != "" {
				return code
			}
		}
		return ""
	}
}

func consensusCountry(samples []string) string {
	counts := make(map[string]int, len(samples))
	best, bestCount := "", 0
	tied := false
	for _, sample := range samples {
		code := domain.NormalizeCountryCode(sample)
		if code == "" {
			continue
		}
		counts[code]++
		count := counts[code]
		if count > bestCount {
			best, bestCount, tied = code, count, false
			continue
		}
		if count == bestCount && code != best {
			tied = true
		}
	}
	if bestCount == 0 || tied {
		return ""
	}
	return best
}

func publicExitIP(value string) (netip.Addr, bool) {
	address, err := netip.ParseAddr(strings.Trim(strings.TrimSpace(value), "[]"))
	if err != nil || !netguard.IsPublicAddress(address) {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}

func loadExitCountryCache(ip string) (string, bool) {
	exitCountryMu.Lock()
	defer exitCountryMu.Unlock()
	item, ok := exitCountryCache[ip]
	if !ok || time.Now().After(item.expires) {
		delete(exitCountryCache, ip)
		return "", false
	}
	return item.code, true
}

func storeExitCountryCache(ip, code string) {
	ttl := exitCountryMissTTL
	if code != "" {
		ttl = exitCountryCacheTTL
	}
	exitCountryMu.Lock()
	defer exitCountryMu.Unlock()
	now := time.Now()
	if len(exitCountryCache) >= exitCountryCacheLimit {
		for key, item := range exitCountryCache {
			if now.After(item.expires) {
				delete(exitCountryCache, key)
			}
		}
	}
	if len(exitCountryCache) >= exitCountryCacheLimit {
		clear(exitCountryCache)
	}
	exitCountryCache[ip] = exitCountryCacheItem{code: code, expires: now.Add(ttl)}
}
