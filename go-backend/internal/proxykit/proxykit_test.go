package proxykit

import (
	"encoding/base64"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseMihomoScriptLinks(t *testing.T) {
	// Links in the exact format printed by mihomo_script (ss2022, mierus, sudoku).
	ss := "ss://2022-blake3-aes-128-gcm:" + pctEncode("AAECAwQFBgcICQoLDA0ODw==") + "@1.2.3.4:20001#node-ss"
	p, err := ParseShareLink(ss)
	if err != nil {
		t.Fatalf("parse ss: %v", err)
	}
	if Type(p) != "ss" || Port(p) != 20001 || GetString(p, "cipher") != "2022-blake3-aes-128-gcm" || GetString(p, "password") != "AAECAwQFBgcICQoLDA0ODw==" {
		t.Fatalf("unexpected ss proxy: %#v", p)
	}

	mieru := "mierus://u0a1b2c:pass1234@1.2.3.4:20002?handshake-mode=HANDSHAKE_STANDARD&mtu=1400&multiplexing=MULTIPLEXING_LOW&port=20002&port=20002&profile=node-mieru&protocol=TCP&protocol=UDP&traffic-pattern=IgIQATICCAM%3D"
	p, err = ParseShareLink(mieru)
	if err != nil {
		t.Fatalf("parse mieru: %v", err)
	}
	if Type(p) != "mieru" || Server(p) != "1.2.3.4" || GetString(p, "username") != "u0a1b2c" || GetString(p, "password") != "pass1234" {
		t.Fatalf("unexpected mieru proxy: %#v", p)
	}
	if Port(p) != 20002 && GetString(p, "port-range") == "" {
		t.Fatalf("mieru port missing: %#v", p)
	}
	if GetString(p, "transport") != "TCP" || !GetBool(p, "udp") {
		t.Fatalf("TCP+UDP mieru links should use TCP transport with UDP relay: %#v", p)
	}

	payload := `{"n":"node-sudoku","h":"1.2.3.4","p":20003,"k":"8f0c7c55-8d4f-4c5f-9f53-1a2b3c4d5e6f","a":"entropy","e":"chacha20-poly1305","m":1080,"x":true,"hd":true}`
	sudoku := "sudoku://" + base64.RawURLEncoding.EncodeToString([]byte(payload))
	p, err = ParseShareLink(sudoku)
	if err != nil {
		t.Fatalf("parse sudoku: %v", err)
	}
	if Type(p) != "sudoku" || Name(p) != "node-sudoku" || Port(p) != 20003 || GetString(p, "table-type") != "prefer_entropy" {
		t.Fatalf("unexpected sudoku proxy: %#v", p)
	}
	if GetBool(p, "enable-pure-downlink") {
		t.Fatalf("x=true means packed downlink, got pure downlink: %#v", p)
	}
	if hm, ok := p["httpmask"].(map[string]any); !ok || !GetBool(hm, "disable") {
		t.Fatalf("expected http mask disabled: %#v", p)
	}
}

func TestShareLinkRoundTrip(t *testing.T) {
	params := DeployParams{Protocols: []string{"ss", "mieru", "sudoku"}, Host: "203.0.113.7", IPVersion: "ipv4-prefer", BlockPrivate: true}
	if err := FillDeployDefaults(&params); err != nil {
		t.Fatal(err)
	}
	for proto, proxy := range ClientProxies(params, "测试节点") {
		link, err := ShareLink(proxy)
		if err != nil {
			t.Fatalf("%s share link: %v", proto, err)
		}
		back, err := ParseShareLink(link)
		if err != nil {
			t.Fatalf("%s parse back %q: %v", proto, link, err)
		}
		if Type(back) != Type(proxy) || Server(back) != Server(proxy) {
			t.Fatalf("%s round trip mismatch: %#v vs %#v", proto, back, proxy)
		}
		if Name(back) != Name(proxy) {
			t.Fatalf("%s name mismatch: %q vs %q", proto, Name(back), Name(proxy))
		}
		switch proto {
		case "ss":
			if GetString(back, "password") != params.SSPassword || Port(back) != params.SSPort {
				t.Fatalf("ss credentials lost: %#v", back)
			}
		case "mieru":
			if GetString(back, "password") != params.MieruPassword || GetString(back, "traffic-pattern") != MieruTrafficPattern("48") {
				t.Fatalf("mieru settings lost: %#v (%s)", back, link)
			}
			if !strings.Contains(link, "protocol=TCP&protocol=UDP") {
				t.Fatalf("mieru link should advertise TCP and UDP: %s", link)
			}
		case "sudoku":
			if GetString(back, "key") != params.SudokuKey || Port(back) != params.SudokuPort {
				t.Fatalf("sudoku key lost: %#v", back)
			}
		}
	}
}

func TestDeployParamsMatchScriptValidators(t *testing.T) {
	for i := 0; i < 20; i++ {
		params := DeployParams{Protocols: []string{"sudoku", "ss", "mieru"}, Host: "example.com"}
		if err := FillDeployDefaults(&params); err != nil {
			t.Fatal(err)
		}
		if err := ValidateDeployParams(params); err != nil {
			t.Fatalf("generated params rejected: %v (%#v)", err, params)
		}
		if strings.Join(params.Protocols, ",") != "ss,mieru,sudoku" {
			t.Fatalf("protocols not normalised: %v", params.Protocols)
		}
		if params.SSPort == params.MieruPort || params.SSPort == params.SudokuPort || params.MieruPort == params.SudokuPort {
			t.Fatalf("ports must be distinct: %#v", params)
		}
		raw, err := base64.StdEncoding.DecodeString(params.SSPassword)
		if err != nil || len(raw) != 16 {
			t.Fatalf("ss2022 password must be base64 of 16 bytes: %q", params.SSPassword)
		}
		if !regexp.MustCompile(`^[A-Za-z0-9-]{16,255}$`).MatchString(params.SudokuKey) {
			t.Fatalf("sudoku key fails script validator: %q", params.SudokuKey)
		}
	}
}

func TestBuildInstallCommand(t *testing.T) {
	params := DeployParams{Protocols: []string{"ss", "sudoku"}, Host: "203.0.113.7", IPVersion: "ipv4-prefer", BlockPrivate: true,
		SSPort: 8388, SSCipher: "2022-blake3-aes-128-gcm", SSPassword: "AAECAwQFBgcICQoLDA0ODw==",
		SudokuPort: 9000, SudokuKeyMode: "psk", SudokuKey: "8f0c7c55-8d4f-4c5f-9f53-1a2b3c4d5e6f", SudokuTable: "prefer_entropy", SudokuPadding: "1-15"}
	cmd := BuildInstallCommand(params, "")
	for _, want := range []string{
		"bash <(curl -fsSL '" + DefaultMihomoScriptURL + "') install -y",
		"--protocols ss,sudoku",
		"--ss-port 8388",
		"--ss-password AAECAwQFBgcICQoLDA0ODw==",
		"--sudoku-key 8f0c7c55-8d4f-4c5f-9f53-1a2b3c4d5e6f",
		"--block-private 1",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command missing %q:\n%s", want, cmd)
		}
	}
	if strings.Contains(cmd, "--mieru") {
		t.Fatalf("mieru options must be omitted: %s", cmd)
	}
	quoted := BuildInstallCommand(DeployParams{Protocols: []string{"ss"}, Host: "h", IPVersion: "ipv4", SSPort: 1, SSCipher: "aes-128-gcm", SSPassword: "it's+bad"}, "")
	if !strings.Contains(quoted, `--ss-password 'it'"'"'s+bad'`) {
		t.Fatalf("special characters must be single-quoted: %s", quoted)
	}
	args := ScriptOpArgs(ScriptOpInstall, params)
	if len(args) == 0 || args[0] != "-y" || strings.Join(args, " ") != strings.Join(InstallArgs(params)[1:], " ") {
		t.Fatalf("agent install args must be the install options without the op: %v", args)
	}
	if got := strings.Join(ScriptOpArgs(ScriptOpUninstall, params), " "); got != "-y" {
		t.Fatalf("uninstall args: %q", got)
	}
	if got := ScriptOpArgs(ScriptOpInfo, params); len(got) != 0 {
		t.Fatalf("info takes no args: %v", got)
	}
}

func TestExtractAndApplyShareLinks(t *testing.T) {
	params := DeployParams{Protocols: []string{"sudoku"}, Host: "1.2.3.4", SudokuKeyMode: "pair", SudokuTable: "prefer_entropy", SudokuPadding: "1-15", SudokuPort: 1}
	payload := `{"n":"x","h":"1.2.3.4","p":20003,"k":"abcdef0123456789abcdef0123456789","a":"entropy","e":"chacha20-poly1305","m":1080,"x":true,"hd":true}`
	output := "==> 安装完成\nsudoku://" + base64.RawURLEncoding.EncodeToString([]byte(payload)) + "\n"
	links := ExtractShareLinks(output)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %v", links)
	}
	found := ApplyShareLinks(&params, links)
	if len(found) != 1 || params.SudokuKey != "abcdef0123456789abcdef0123456789" || params.SudokuPort != 20003 {
		t.Fatalf("share links not applied: %v %#v", found, params)
	}
}

func TestParseContentFormats(t *testing.T) {
	yamlDoc := "proxies:\n  - {name: a, type: ss, server: 1.1.1.1, port: 443, cipher: aes-128-gcm, password: pw123456}\n  - name: b\n    type: trojan\n    server: example.com\n    port: 443\n    password: x\n"
	res := ParseContent([]byte(yamlDoc))
	if res.Format != "clash" || len(res.Proxies) != 2 {
		t.Fatalf("clash yaml: %#v", res)
	}
	lines := "trojan://pw@example.com:443?sni=example.com#t1\nss://YWVzLTEyOC1nY206cHcxMjM0NTY@1.1.1.1:8388#s1\nnot-a-link\n"
	res = ParseContent([]byte(base64.StdEncoding.EncodeToString([]byte(lines))))
	if res.Format != "uri" || len(res.Proxies) != 2 || len(res.Failed) != 1 {
		t.Fatalf("base64 uri list: %#v", res)
	}
}

func TestRenderGeneratesLoadableConfig(t *testing.T) {
	nodes := []Proxy{
		{"name": "香港 01", "type": "ss", "server": "1.1.1.1", "port": 443, "cipher": "aes-128-gcm", "password": "pw123456"},
		{"name": "香港 01", "type": "trojan", "server": "2.2.2.2", "port": 443, "password": "x"},
		{"name": "日本 01", "type": "trojan", "server": "3.3.3.3", "port": 443, "password": "x"},
	}
	cfg := GeneratorConfig{Template: "standard"}
	cfg.Rules = append([]string{"DOMAIN-SUFFIX,example.org,不存在的组", "AND,((DOMAIN,a.com),(NETWORK,UDP)),REJECT"}, TemplateByID("standard").Rules...)
	res, err := Render(cfg, nodes)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		MixedPort   int              `yaml:"mixed-port"`
		Proxies     []map[string]any `yaml:"proxies"`
		ProxyGroups []map[string]any `yaml:"proxy-groups"`
		Rules       []string         `yaml:"rules"`
	}
	if err := yaml.Unmarshal([]byte(res.Content), &doc); err != nil {
		t.Fatalf("rendered yaml invalid: %v\n%s", err, res.Content)
	}
	if doc.MixedPort != 7890 || len(doc.Proxies) != 3 {
		t.Fatalf("unexpected document: %+v", doc)
	}
	if doc.Proxies[1]["name"] != "香港 01 2" {
		t.Fatalf("duplicate names must be made unique: %v", doc.Proxies[1]["name"])
	}
	if !strings.HasPrefix(doc.Rules[len(doc.Rules)-1], "MATCH,") {
		t.Fatalf("rules must end with MATCH: %v", doc.Rules)
	}
	if doc.Rules[0] != "DOMAIN-SUFFIX,example.org,🚀 节点选择" {
		t.Fatalf("unknown rule target should fall back to first group, got %q", doc.Rules[0])
	}
	if doc.Rules[1] != "AND,((DOMAIN,a.com),(NETWORK,UDP)),REJECT" {
		t.Fatalf("logical rule mangled: %q", doc.Rules[1])
	}
	groupNames := map[string]bool{}
	for _, g := range doc.ProxyGroups {
		groupNames[g["name"].(string)] = true
	}
	for _, g := range doc.ProxyGroups {
		for _, m := range g["proxies"].([]any) {
			name := m.(string)
			if !groupNames[name] && !builtinPolicies[name] && name != "香港 01" && name != "香港 01 2" && name != "日本 01" {
				t.Fatalf("group %v references unknown member %q", g["name"], name)
			}
		}
	}
	if len(res.Warnings) == 0 {
		t.Fatal("expected a warning for the unknown rule target")
	}
}

func TestRenderCustomBaseConfigKeepsOrder(t *testing.T) {
	cfg := GeneratorConfig{
		BaseConfig: "port: 7891\nproxies: []\nmode: rule\n",
		Groups:     []ProxyGroupSpec{{Name: "PROXY", Type: "select", IncludeAllNodes: true, Filter: "^JP"}},
		Rules:      []string{"MATCH,PROXY"},
	}
	res, err := Render(cfg, []Proxy{{"name": "JP-1", "type": "socks5", "server": "1.1.1.1", "port": 1080}, {"name": "US-1", "type": "socks5", "server": "1.1.1.2", "port": 1080}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Content, "port: 7891\nmode: rule\nproxies:") {
		t.Fatalf("base config order not kept:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "- US-1\n") {
		t.Fatalf("regex filter not applied:\n%s", res.Content)
	}
}

func TestSubscriptionUserInfo(t *testing.T) {
	info := ParseSubscriptionUserInfo("upload=1024; download=2048; total=1073741824; expire=1767196800")
	if info == nil || info.Total != 1073741824 || info.Expire != 1767196800 {
		t.Fatalf("unexpected info: %#v", info)
	}
	if ParseSubscriptionUserInfo("") != nil {
		t.Fatal("empty header should return nil")
	}
}

func TestBlockedIP(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "::1", "fe80::1", "0.0.0.0"} {
		if !blockedIP(parseIP(t, ip)) {
			t.Fatalf("%s should be blocked", ip)
		}
	}
	for _, ip := range []string{"1.1.1.1", "2606:4700:4700::1111"} {
		if blockedIP(parseIP(t, ip)) {
			t.Fatalf("%s should be allowed", ip)
		}
	}
}

func parseIP(t *testing.T, s string) []byte {
	t.Helper()
	ip := netParseIP(s)
	if ip == nil {
		t.Fatalf("bad ip %q", s)
	}
	return ip
}
