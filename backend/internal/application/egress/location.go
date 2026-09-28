package egress

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"strings"

	domain "github.com/chenyme/grok2api/backend/internal/domain/egress"
	"github.com/chenyme/grok2api/backend/internal/pkg/tunnelproxy"
)

const locationHongKong = "HK"

type locationPhrase struct {
	text string
	code string
}

// locationPhrases is matched against the node label. Earlier entries win when
// several regions appear, so Hong Kong is recognized before a secondary city.
var locationPhrases = []locationPhrase{
	{"香港", "HK"},
	{"🇭🇰", "HK"},
	{"hong kong", "HK"},
	{"hongkong", "HK"},
	{"澳门", "MO"},
	{"澳門", "MO"},
	{"macau", "MO"},
	{"macao", "MO"},
	{"台湾", "TW"},
	{"台灣", "TW"},
	{"taiwan", "TW"},
	{"台北", "TW"},
	{"日本", "JP"},
	{"东京", "JP"},
	{"東京", "JP"},
	{"大阪", "JP"},
	{"japan", "JP"},
	{"tokyo", "JP"},
	{"osaka", "JP"},
	{"新加坡", "SG"},
	{"狮城", "SG"},
	{"獅城", "SG"},
	{"singapore", "SG"},
	{"美国", "US"},
	{"美國", "US"},
	{"硅谷", "US"},
	{"美西", "US"},
	{"美东", "US"},
	{"美東", "US"},
	{"洛杉矶", "US"},
	{"洛杉磯", "US"},
	{"圣何塞", "US"},
	{"西雅图", "US"},
	{"united states", "US"},
	{"韩国", "KR"},
	{"韓國", "KR"},
	{"韩國", "KR"},
	{"首尔", "KR"},
	{"korea", "KR"},
	{"seoul", "KR"},
	{"英国", "GB"},
	{"英國", "GB"},
	{"伦敦", "GB"},
	{"倫敦", "GB"},
	{"united kingdom", "GB"},
	{"london", "GB"},
	{"德国", "DE"},
	{"德國", "DE"},
	{"法兰克福", "DE"},
	{"germany", "DE"},
	{"frankfurt", "DE"},
	{"法国", "FR"},
	{"法國", "FR"},
	{"巴黎", "FR"},
	{"france", "FR"},
	{"paris", "FR"},
	{"荷兰", "NL"},
	{"荷蘭", "NL"},
	{"阿姆斯特丹", "NL"},
	{"netherlands", "NL"},
	{"amsterdam", "NL"},
	{"加拿大", "CA"},
	{"canada", "CA"},
	{"澳大利亚", "AU"},
	{"澳洲", "AU"},
	{"australia", "AU"},
	{"sydney", "AU"},
	{"俄罗斯", "RU"},
	{"俄國", "RU"},
	{"russia", "RU"},
	{"土耳其", "TR"},
	{"turkey", "TR"},
	{"istanbul", "TR"},
	{"泰国", "TH"},
	{"泰國", "TH"},
	{"thailand", "TH"},
	{"bangkok", "TH"},
	{"越南", "VN"},
	{"vietnam", "VN"},
	{"菲律宾", "PH"},
	{"菲律賓", "PH"},
	{"philippines", "PH"},
	{"马来西亚", "MY"},
	{"馬來西亞", "MY"},
	{"malaysia", "MY"},
	{"印度尼西亚", "ID"},
	{"印尼", "ID"},
	{"indonesia", "ID"},
	{"阿联酋", "AE"},
	{"迪拜", "AE"},
	{"dubai", "AE"},
	{"印度", "IN"},
	{"india", "IN"},
}

var locationTokens = map[string]string{
	"hk": "HK", "hkg": "HK", "hkt": "HK", "hongkong": "HK",
	"mo": "MO",
	"tw": "TW", "tpe": "TW",
	"jp": "JP", "tyo": "JP", "nrt": "JP",
	"kr": "KR", "icn": "KR",
	"sg": "SG", "sin": "SG",
	"us": "US", "usa": "US", "lax": "US", "sjc": "US", "sea": "US",
	"gb": "GB", "uk": "GB",
	"de": "DE",
	"nl": "NL", "ams": "NL",
	"ca": "CA",
	"au": "AU", "syd": "AU",
	"ru": "RU",
	"tr": "TR", "ist": "TR",
	"th": "TH", "bkk": "TH",
	"vn": "VN",
	"ph": "PH", "mnl": "PH",
	"ae": "AE", "dxb": "AE",
}

// detectProxyLocation classifies a proxy from its subscription label first,
// then from the server hostname. An empty result means the region is unknown.
func detectProxyLocation(label, host string) string {
	if code := matchLocationText(label); code != "" {
		return code
	}
	if code := matchLocationFlag(label); code != "" {
		return code
	}
	return matchLocationHost(host)
}

// matchLocationFlag reads regional-indicator pairs such as 🇰🇷. Text phrases
// stay first so an explicit city still wins when a flag and a label disagree.
func matchLocationFlag(value string) string {
	var pending rune
	for _, character := range value {
		if character < 0x1F1E6 || character > 0x1F1FF {
			pending = 0
			continue
		}
		if pending == 0 {
			pending = character
			continue
		}
		code := string([]byte{byte('a' + pending - 0x1F1E6), byte('a' + character - 0x1F1E6)})
		pending = 0
		if mapped, ok := locationTokens[code]; ok {
			return mapped
		}
		if normalized := domain.NormalizeCountryCode(code); normalized != "" {
			return normalized
		}
	}
	return ""
}

func matchLocationText(value string) string {
	spaced := normalizeLocationText(value)
	if spaced == "" {
		return ""
	}
	compact := strings.ReplaceAll(spaced, " ", "")
	for _, phrase := range locationPhrases {
		if strings.Contains(spaced, phrase.text) || strings.Contains(compact, phrase.text) {
			return phrase.code
		}
	}
	for _, word := range strings.Fields(spaced) {
		if code, ok := locationTokens[trimLocationDigits(word)]; ok {
			return code
		}
	}
	return ""
}

func matchLocationHost(host string) string {
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
	if host == "" {
		return ""
	}
	for _, label := range strings.FieldsFunc(host, func(character rune) bool {
		return character == '.' || character == '-' || character == '_'
	}) {
		if code, ok := locationTokens[trimLocationDigits(label)]; ok {
			return code
		}
	}
	return ""
}

func normalizeLocationText(value string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		switch character {
		case '-', '_', '|', '/', '.', ',', '·', '，', '、':
			builder.WriteByte(' ')
		default:
			if character == ' ' || character == '\t' {
				builder.WriteByte(' ')
				continue
			}
			builder.WriteRune(character)
		}
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

// trimLocationDigits removes a numeric suffix such as jp01 or de3. Leading
// digits stay in place so a label like 80th is not reduced to the country
// code th.
func trimLocationDigits(value string) string {
	return strings.TrimRight(value, "0123456789")
}

func proxyServerHost(proxyURL string) string {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return ""
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return ""
	}
	if tunnelproxy.IsSupportedScheme(parsed.Scheme) {
		config, err := tunnelproxy.Parse(proxyURL)
		if err != nil {
			return ""
		}
		return hostWithoutPort(config.Server)
	}
	return parsed.Hostname()
}

func hostWithoutPort(value string) string {
	value = strings.TrimSpace(value)
	if host, _, err := net.SplitHostPort(value); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(value, "[]")
}

func proxyRemark(line string) string {
	fragment := fragmentRemark(line)
	if matchLocationText(fragment) != "" {
		return fragment
	}
	if name := vmessDisplayName(line); name != "" {
		return name
	}
	return fragment
}

func fragmentRemark(line string) string {
	hash := strings.LastIndex(line, "#")
	if hash < 0 || hash == len(line)-1 {
		return ""
	}
	remark := strings.TrimSpace(line[hash+1:])
	if decoded, err := url.PathUnescape(remark); err == nil {
		remark = strings.TrimSpace(decoded)
	}
	return remark
}

func vmessDisplayName(line string) string {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "vmess://") {
		return ""
	}
	payload := strings.TrimSpace(line)[len("vmess://"):]
	payload, _, _ = strings.Cut(payload, "#")
	payload = strings.TrimSpace(payload)
	decoded, ok := decodeSubscriptionBase64(payload)
	if !ok {
		return ""
	}
	var body struct {
		Name string `json:"ps"`
	}
	if err := json.Unmarshal(decoded, &body); err != nil {
		return ""
	}
	return strings.TrimSpace(body.Name)
}

func decodeSubscriptionBase64(value string) ([]byte, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		decoded, err := encoding.DecodeString(value)
		if err == nil && len(decoded) > 0 {
			return decoded, true
		}
	}
	return nil, false
}

func selectSubscriptionEntries(entries []subscriptionEntry, excludeHongKong bool) ([]subscriptionEntry, int) {
	if !excludeHongKong {
		return entries, 0
	}
	kept := make([]subscriptionEntry, 0, len(entries))
	skipped := 0
	for _, entry := range entries {
		if entry.Location == locationHongKong {
			skipped++
			continue
		}
		kept = append(kept, entry)
	}
	return kept, skipped
}

func resolveNodeLocation(previousName, name, previousLocation, host string, proxyChanged bool) string {
	if code := detectProxyLocation(name, host); code != "" {
		return code
	}
	if previousName == name && !proxyChanged {
		return previousLocation
	}
	return ""
}
