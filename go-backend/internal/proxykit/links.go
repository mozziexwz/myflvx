package proxykit

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/MMWOrg/mmwX-plugins/proxyparser"
	"github.com/MMWOrg/mmwX-plugins/proxyparser/substore"
	"gopkg.in/yaml.v3"
)

// ParseShareLink parses a single share link (ss://, vmess://, vless://,
// trojan://, hysteria2://, tuic://, anytls://, mierus://, sudoku://, ...)
// into a Clash proxy.
func ParseShareLink(link string) (Proxy, error) {
	link = strings.TrimSpace(link)
	if link == "" {
		return nil, fmt.Errorf("分享链接为空")
	}
	var (
		p   Proxy
		err error
	)
	lower := strings.ToLower(link)
	if strings.HasPrefix(lower, "sudoku://") {
		p, err = parseSudokuLink(link)
	} else {
		p, err = proxyparser.Parse(link)
	}
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(lower, "mierus://") || strings.HasPrefix(lower, "mieru://") {
		applyMieruProtocols(p, link)
	}
	if Name(p) == "" {
		p["name"] = fmt.Sprintf("%s %s", strings.ToUpper(Type(p)), HostPort(Server(p), Port(p)))
	}
	return PrepareProxy(p)
}

// ParseResult is the outcome of parsing a block of subscription content.
type ParseResult struct {
	Proxies []Proxy  `json:"proxies"`
	Failed  []string `json:"failed"`
	Format  string   `json:"format"` // clash | uri | unknown
}

// ParseContent parses subscription content: a Clash/mihomo YAML document, a
// base64 encoded URI list or a plain list of share links (one per line).
func ParseContent(content []byte) ParseResult {
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	text := strings.TrimSpace(string(content))
	if text == "" {
		return ParseResult{Format: "unknown"}
	}
	if looksLikeClashYAML(text) {
		if proxies, err := ParseClashProxies([]byte(text)); err == nil {
			return ParseResult{Proxies: proxies, Format: "clash"}
		}
	}
	if !strings.Contains(text, "://") {
		if decoded := decodeBase64Loose(text); decoded != "" {
			if looksLikeClashYAML(decoded) {
				if proxies, err := ParseClashProxies([]byte(decoded)); err == nil {
					return ParseResult{Proxies: proxies, Format: "clash"}
				}
			}
			if strings.Contains(decoded, "://") {
				text = decoded
			}
		}
	}

	result := ParseResult{Format: "uri"}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		if !strings.Contains(line, "://") {
			result.Failed = append(result.Failed, line)
			continue
		}
		p, err := ParseShareLink(line)
		if err != nil {
			result.Failed = append(result.Failed, line)
			continue
		}
		result.Proxies = append(result.Proxies, p)
	}
	if len(result.Proxies) == 0 && len(result.Failed) == 0 {
		result.Format = "unknown"
	}
	return result
}

// applyMieruProtocols follows mihomo_script's client config for links that
// advertise both protocols on one port: TCP transport with UDP relay enabled
// (the parser otherwise keeps the last protocol, UDP).
func applyMieruProtocols(p Proxy, link string) {
	parts := strings.SplitN(link, "?", 2)
	if len(parts) != 2 {
		return
	}
	query := parts[1]
	if i := strings.Index(query, "#"); i >= 0 {
		query = query[:i]
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return
	}
	hasTCP, hasUDP := false, false
	for _, v := range values["protocol"] {
		switch strings.ToUpper(strings.TrimSpace(v)) {
		case "TCP":
			hasTCP = true
		case "UDP":
			hasUDP = true
		}
	}
	if hasTCP {
		p["transport"] = "TCP"
	}
	if hasUDP {
		p["udp"] = true
	}
}

func looksLikeClashYAML(text string) bool {
	return strings.HasPrefix(text, "proxies:") || strings.Contains(text, "\nproxies:")
}

// ParseClashProxies extracts the `proxies:` list from a Clash YAML document.
func ParseClashProxies(content []byte) ([]Proxy, error) {
	var doc struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("YAML 解析失败: %w", err)
	}
	out := make([]Proxy, 0, len(doc.Proxies))
	for _, item := range doc.Proxies {
		p, err := PrepareProxy(normalizeYAMLMap(item))
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// normalizeYAMLMap converts map[any]any values produced by YAML decoding into
// JSON-compatible maps.
func normalizeYAMLMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = normalizeYAMLValue(v)
	}
	return out
}

func normalizeYAMLValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return normalizeYAMLMap(t)
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, item := range t {
			out[fmt.Sprint(k)] = normalizeYAMLValue(item)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = normalizeYAMLValue(item)
		}
		return out
	default:
		return v
	}
}

func decodeBase64Loose(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.RawURLEncoding} {
		if out, err := enc.DecodeString(s); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}

// ShareLink renders a proxy as a share link. The SS 2022, mieru and Sudoku
// formats follow mihomo_script so links exported by the panel and by the
// script are identical.
func ShareLink(p Proxy) (string, error) {
	switch Type(p) {
	case "sudoku":
		return sudokuLink(p)
	case "mieru":
		return mieruLink(p)
	case "ss":
		if GetString(p, "plugin") == "" {
			return ssLink(p)
		}
	}
	link, err := substore.NewURIProducer().ProduceOne(substore.Proxy(CloneProxy(p)))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(link) == "" {
		return "", fmt.Errorf("不支持导出 %s 类型的分享链接", Type(p))
	}
	return link, nil
}

// ShareLinks renders every proxy that supports share links, one per line.
func ShareLinks(proxies []Proxy) string {
	lines := make([]string, 0, len(proxies))
	for _, p := range proxies {
		if link, err := ShareLink(p); err == nil && link != "" {
			lines = append(lines, link)
		}
	}
	return strings.Join(lines, "\n")
}

// pctEncode percent-encodes everything except RFC 3986 unreserved characters
// (the same rule as mihomo_script's urlencode).
func pctEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func b64url(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func hostInURL(host string) string {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

func isSS2022(cipher string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(cipher)), "2022-")
}

// ssLink renders SIP002: 2022 ciphers keep the userinfo percent-encoded,
// classic AEAD ciphers use base64url(cipher:password).
func ssLink(p Proxy) (string, error) {
	server, port := Server(p), Port(p)
	cipher, password := GetString(p, "cipher"), GetString(p, "password")
	if server == "" || port <= 0 || cipher == "" {
		return "", fmt.Errorf("SS 节点缺少地址、端口或加密方式")
	}
	host := hostInURL(server)
	if isSS2022(cipher) {
		return fmt.Sprintf("ss://%s:%s@%s:%d#%s", pctEncode(cipher), pctEncode(password), host, port, pctEncode(Name(p))), nil
	}
	return fmt.Sprintf("ss://%s@%s:%d#%s", b64url(cipher+":"+password), host, port, pctEncode(Name(p))), nil
}

// mieruLink renders the official mierus:// simple link. The port is written
// both in the query (official parser) and in the authority (other clients).
func mieruLink(p Proxy) (string, error) {
	server, port := Server(p), Port(p)
	if server == "" || port <= 0 {
		return "", fmt.Errorf("mieru 节点缺少地址或端口")
	}
	user, pass := GetString(p, "username"), GetString(p, "password")
	params := []string{}
	if hm := GetString(p, "handshake-mode"); hm != "" {
		params = append(params, "handshake-mode="+pctEncode(hm))
	}
	mtu := GetInt(p, "mtu")
	if mtu <= 0 {
		mtu = 1400
	}
	params = append(params, "mtu="+strconv.Itoa(mtu))
	if mux := GetString(p, "multiplexing"); mux != "" {
		params = append(params, "multiplexing="+pctEncode(mux))
	}
	transport := strings.ToUpper(GetString(p, "transport"))
	if transport != "UDP" {
		transport = "TCP"
	}
	withUDP := transport == "TCP" && GetBool(p, "udp")
	params = append(params, "port="+strconv.Itoa(port))
	if withUDP {
		params = append(params, "port="+strconv.Itoa(port))
	}
	params = append(params, "profile="+pctEncode(Name(p)))
	params = append(params, "protocol="+transport)
	if withUDP {
		params = append(params, "protocol=UDP")
	}
	if tp := GetString(p, "traffic-pattern"); tp != "" {
		params = append(params, "traffic-pattern="+pctEncode(tp))
	}
	return fmt.Sprintf("mierus://%s:%s@%s:%d?%s", pctEncode(user), pctEncode(pass), hostInURL(server), port, strings.Join(params, "&")), nil
}

type sudokuShortLink struct {
	N  string   `json:"n,omitempty"`
	H  string   `json:"h"`
	P  int      `json:"p"`
	K  string   `json:"k"`
	A  string   `json:"a,omitempty"`
	E  string   `json:"e,omitempty"`
	M  int      `json:"m,omitempty"`
	X  bool     `json:"x,omitempty"`
	TS []string `json:"ts,omitempty"`
	HD bool     `json:"hd,omitempty"`
}

func sudokuTableToShort(table string) string {
	switch table {
	case "prefer_ascii":
		return "ascii"
	case "", "prefer_entropy":
		return "entropy"
	default:
		return table
	}
}

func sudokuTableFromShort(short string) string {
	switch short {
	case "ascii":
		return "prefer_ascii"
	case "", "entropy":
		return "prefer_entropy"
	default:
		return short
	}
}

// sudokuLink renders the official Sudoku short link: sudoku:// + base64url(JSON).
func sudokuLink(p Proxy) (string, error) {
	server, port, key := Server(p), Port(p), GetString(p, "key")
	if server == "" || port <= 0 || key == "" {
		return "", fmt.Errorf("Sudoku 节点缺少地址、端口或密钥")
	}
	aead := GetString(p, "aead-method")
	if aead == "" {
		aead = "chacha20-poly1305"
	}
	// mihomo defaults to a pure Sudoku downlink when the field is absent.
	pureDownlink := true
	if _, ok := p["enable-pure-downlink"]; ok {
		pureDownlink = GetBool(p, "enable-pure-downlink")
	}
	link := sudokuShortLink{
		N: Name(p),
		H: strings.Trim(server, "[]"),
		P: port,
		K: key,
		A: sudokuTableToShort(GetString(p, "table-type")),
		E: aead,
		M: 1080,
		X: !pureDownlink,
	}
	link.TS = stringList(p["custom-tables"])
	if hm, ok := p["httpmask"].(map[string]any); ok && GetBool(hm, "disable") {
		link.HD = true
	} else if v, ok := p["http-mask"].(bool); ok && !v {
		link.HD = true
	}
	raw, err := json.Marshal(link)
	if err != nil {
		return "", err
	}
	return "sudoku://" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func parseSudokuLink(link string) (Proxy, error) {
	payload := strings.TrimSpace(link[len("sudoku://"):])
	if i := strings.IndexAny(payload, "#?"); i >= 0 {
		payload = payload[:i]
	}
	var raw []byte
	var err error
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if raw, err = enc.DecodeString(payload); err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("Sudoku 链接解码失败: %w", err)
	}
	var s sudokuShortLink
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("Sudoku 链接格式错误: %w", err)
	}
	if s.H == "" || s.P <= 0 || s.K == "" {
		return nil, fmt.Errorf("Sudoku 链接缺少地址、端口或密钥")
	}
	name := s.N
	if name == "" {
		name = "Sudoku " + HostPort(s.H, s.P)
	}
	if fragment := strings.SplitN(link, "#", 2); len(fragment) == 2 {
		if decoded, err := url.PathUnescape(fragment[1]); err == nil && strings.TrimSpace(decoded) != "" {
			name = strings.TrimSpace(decoded)
		}
	}
	aead := s.E
	if aead == "" {
		aead = "none"
	}
	p := Proxy{
		"name":                 name,
		"type":                 "sudoku",
		"server":               s.H,
		"port":                 s.P,
		"key":                  s.K,
		"aead-method":          aead,
		"table-type":           sudokuTableFromShort(s.A),
		"enable-pure-downlink": !s.X,
	}
	if len(s.TS) > 0 {
		tables := make([]any, 0, len(s.TS))
		for _, t := range s.TS {
			tables = append(tables, t)
		}
		p["custom-tables"] = tables
	}
	if s.HD {
		p["httpmask"] = map[string]any{"disable": true}
	}
	return p, nil
}

func stringList(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.TrimSpace(t) == "" {
			return nil
		}
		parts := strings.Split(t, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if s := strings.TrimSpace(part); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
