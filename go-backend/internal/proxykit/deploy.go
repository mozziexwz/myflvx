package proxykit

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// DefaultMihomoScriptURL is the one-click script the deploy feature drives.
const DefaultMihomoScriptURL = "https://raw.githubusercontent.com/mozziexwz/mihomo_script/main/mihomo.sh"

// Deploy protocol identifiers (same names as mihomo_script).
const (
	DeployProtoSS     = "ss"
	DeployProtoMieru  = "mieru"
	DeployProtoSudoku = "sudoku"
)

// DeployParams mirrors the options of mihomo_script `install`. The panel fills
// every value up front so it can build the client nodes without reading
// anything back from the server (except for Sudoku key pairs).
type DeployParams struct {
	Protocols     []string `json:"protocols"`
	Host          string   `json:"host"`
	ScriptName    string   `json:"scriptName,omitempty"`
	IPVersion     string   `json:"ipVersion"`
	BlockPrivate  bool     `json:"blockPrivate"`
	MihomoVersion string   `json:"mihomoVersion,omitempty"`

	SSPort     int    `json:"ssPort,omitempty"`
	SSCipher   string `json:"ssCipher,omitempty"`
	SSPassword string `json:"ssPassword,omitempty"`

	MieruPort       int    `json:"mieruPort,omitempty"`
	MieruUser       string `json:"mieruUser,omitempty"`
	MieruPassword   string `json:"mieruPassword,omitempty"`
	MieruLowEntropy string `json:"mieruLowEntropy,omitempty"`

	SudokuPort         int    `json:"sudokuPort,omitempty"`
	SudokuKeyMode      string `json:"sudokuKeyMode,omitempty"` // psk | pair
	SudokuKey          string `json:"sudokuKey,omitempty"`     // client key (psk: shared key)
	SudokuServerKey    string `json:"sudokuServerKey,omitempty"`
	SudokuTable        string `json:"sudokuTable,omitempty"`
	SudokuPadding      string `json:"sudokuPadding,omitempty"`
	SudokuPureDownlink bool   `json:"sudokuPureDownlink"`
	SudokuCustomTables string `json:"sudokuCustomTables,omitempty"`
}

var (
	reDeployHost     = regexp.MustCompile(`^[A-Za-z0-9.:-]{1,253}$`)
	reDeployNodeName = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
	reDeployUser     = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	reDeployPass     = regexp.MustCompile(`^[A-Za-z0-9_.-]{8,64}$`)
	reDeploySudoKey  = regexp.MustCompile(`^[A-Za-z0-9-]{16,255}$`)
	reDeployPadding  = regexp.MustCompile(`^([0-9]{1,3})-([0-9]{1,3})$`)
	reDeployVersion  = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	reCustomTable    = regexp.MustCompile(`^[xpv]{8}$`)
	reBase64Key      = regexp.MustCompile(`^[A-Za-z0-9+/]+=*$`)
	reShellSafeArg   = regexp.MustCompile(`^[A-Za-z0-9_.,:/=-]+$`)
)

// mieruTrafficPatterns are the base64 protobuf traffic patterns used by
// mihomo_script for each low-entropy level.
var mieruTrafficPatterns = map[string]string{
	"56":  "IgIQATICCAQ=",
	"48":  "IgIQATICCAM=",
	"40":  "IgIQATICCAI=",
	"32":  "IgIQATICCAE=",
	"off": "IgIQAQ==",
}

// MieruTrafficPattern returns the traffic-pattern for a low-entropy level.
func MieruTrafficPattern(level string) string {
	return mieruTrafficPatterns[strings.TrimSpace(level)]
}

var ssCiphers = map[string]int{
	"2022-blake3-aes-128-gcm":       16,
	"2022-blake3-aes-256-gcm":       32,
	"2022-blake3-chacha20-poly1305": 32,
	"aes-128-gcm":                   0,
	"aes-256-gcm":                   0,
	"chacha20-ietf-poly1305":        0,
}

var sudokuTables = map[string]bool{
	"prefer_ascii": true, "prefer_entropy": true,
	"up_ascii_down_entropy": true, "up_entropy_down_ascii": true,
}

var ipVersions = map[string]bool{
	"ipv4-prefer": true, "ipv6-prefer": true, "ipv4": true, "ipv6": true, "dual": true,
}

// HasProtocol reports whether the protocol is enabled.
func (p *DeployParams) HasProtocol(name string) bool {
	for _, item := range p.Protocols {
		if item == name {
			return true
		}
	}
	return false
}

// NormalizeProtocols de-duplicates and orders the protocol list like the script.
func NormalizeProtocols(list []string) ([]string, error) {
	seen := map[string]bool{}
	for _, item := range list {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		switch item {
		case DeployProtoSS, DeployProtoMieru, DeployProtoSudoku:
			seen[item] = true
		default:
			return nil, fmt.Errorf("不支持的协议: %s（可选 ss / mieru / sudoku）", item)
		}
	}
	out := make([]string, 0, 3)
	for _, item := range []string{DeployProtoSS, DeployProtoMieru, DeployProtoSudoku} {
		if seen[item] {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("至少选择一个协议")
	}
	return out, nil
}

// FillDeployDefaults fills empty fields with defaults or random values. Values
// that are already set are kept, so re-running a deployment keeps its ports
// and passwords unless they are cleared explicitly.
func FillDeployDefaults(p *DeployParams) error {
	protocols, err := NormalizeProtocols(p.Protocols)
	if err != nil {
		return err
	}
	p.Protocols = protocols
	p.Host = strings.Trim(strings.TrimSpace(p.Host), "[]")
	if p.IPVersion == "" {
		p.IPVersion = "ipv4-prefer"
	}
	used := map[int]bool{}
	pick := func(current int) (int, error) {
		if current > 0 && !used[current] {
			used[current] = true
			return current, nil
		}
		port, err := randomPort(used)
		if err != nil {
			return 0, err
		}
		used[port] = true
		return port, nil
	}

	if p.HasProtocol(DeployProtoSS) {
		if p.SSCipher == "" {
			p.SSCipher = "2022-blake3-aes-128-gcm"
		}
		if p.SSPort, err = pick(p.SSPort); err != nil {
			return err
		}
		if p.SSPassword == "" || validSSPassword(p.SSCipher, p.SSPassword) != nil {
			if p.SSPassword, err = GenerateSSPassword(p.SSCipher); err != nil {
				return err
			}
		}
	}
	if p.HasProtocol(DeployProtoMieru) {
		if p.MieruPort, err = pick(p.MieruPort); err != nil {
			return err
		}
		if p.MieruUser == "" {
			p.MieruUser = "u" + RandomHex(6)
		}
		if p.MieruPassword == "" {
			p.MieruPassword = RandomHex(16)
		}
		if p.MieruLowEntropy == "" {
			p.MieruLowEntropy = "48"
		}
	}
	if p.HasProtocol(DeployProtoSudoku) {
		if p.SudokuPort, err = pick(p.SudokuPort); err != nil {
			return err
		}
		if p.SudokuKeyMode == "" {
			p.SudokuKeyMode = "psk"
		}
		if p.SudokuKeyMode == "psk" && p.SudokuKey == "" {
			p.SudokuKey = NewUUID()
			p.SudokuServerKey = p.SudokuKey
		}
		if p.SudokuTable == "" {
			p.SudokuTable = "prefer_entropy"
		}
		if p.SudokuPadding == "" {
			p.SudokuPadding = "1-15"
		}
	}
	return nil
}

// ValidateDeployParams applies the same checks as mihomo_script so the
// unattended install never stops on an invalid value.
func ValidateDeployParams(p DeployParams) error {
	if _, err := NormalizeProtocols(p.Protocols); err != nil {
		return err
	}
	if !reDeployHost.MatchString(p.Host) {
		return fmt.Errorf("客户端连接地址无效（IP 或域名）")
	}
	if p.ScriptName != "" && !reDeployNodeName.MatchString(p.ScriptName) {
		return fmt.Errorf("脚本节点名只能包含字母数字 _.:-")
	}
	if !ipVersions[p.IPVersion] {
		return fmt.Errorf("出站 IP 设置无效")
	}
	if p.MihomoVersion != "" && p.MihomoVersion != "latest" && !reDeployVersion.MatchString(p.MihomoVersion) {
		return fmt.Errorf("mihomo 版本格式应为 vX.Y.Z")
	}
	ports := map[int]string{}
	checkPort := func(label string, port int) error {
		if port < 1 || port > 65535 {
			return fmt.Errorf("%s 端口无效", label)
		}
		if other, ok := ports[port]; ok {
			return fmt.Errorf("%s 与 %s 端口冲突", label, other)
		}
		ports[port] = label
		return nil
	}
	if p.HasProtocol(DeployProtoSS) {
		if err := checkPort("SS", p.SSPort); err != nil {
			return err
		}
		if _, ok := ssCiphers[p.SSCipher]; !ok {
			return fmt.Errorf("不支持的 SS 加密方式: %s", p.SSCipher)
		}
		if err := validSSPassword(p.SSCipher, p.SSPassword); err != nil {
			return err
		}
	}
	if p.HasProtocol(DeployProtoMieru) {
		if err := checkPort("mieru", p.MieruPort); err != nil {
			return err
		}
		if !reDeployUser.MatchString(p.MieruUser) {
			return fmt.Errorf("mieru 用户名只能包含字母数字 _.-")
		}
		if !reDeployPass.MatchString(p.MieruPassword) {
			return fmt.Errorf("mieru 密码需为 8-64 位字母数字 _.-")
		}
		if MieruTrafficPattern(p.MieruLowEntropy) == "" {
			return fmt.Errorf("mieru 低熵档位无效（off/56/48/40/32）")
		}
	}
	if p.HasProtocol(DeployProtoSudoku) {
		if err := checkPort("Sudoku", p.SudokuPort); err != nil {
			return err
		}
		switch p.SudokuKeyMode {
		case "psk":
			if !reDeploySudoKey.MatchString(p.SudokuKey) {
				return fmt.Errorf("Sudoku 共享密钥需为 16-255 位字母数字或 -")
			}
		case "pair":
		default:
			return fmt.Errorf("Sudoku 密钥模式无效")
		}
		if !sudokuTables[p.SudokuTable] {
			return fmt.Errorf("Sudoku 编码表无效")
		}
		if _, _, err := ParsePadding(p.SudokuPadding); err != nil {
			return err
		}
		if err := validCustomTables(p.SudokuCustomTables); err != nil {
			return err
		}
	}
	return nil
}

// ParsePadding parses "min-max" (0-100).
func ParsePadding(v string) (int, int, error) {
	m := reDeployPadding.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return 0, 0, fmt.Errorf("Sudoku 填充率格式应为 最小-最大，例如 1-15")
	}
	minV, _ := strconv.Atoi(m[1])
	maxV, _ := strconv.Atoi(m[2])
	if minV > maxV || maxV > 100 {
		return 0, 0, fmt.Errorf("Sudoku 填充率范围无效（0-100，最小值不大于最大值）")
	}
	return minV, maxV, nil
}

func validCustomTables(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	for _, t := range strings.Split(v, ",") {
		t = strings.TrimSpace(t)
		if !reCustomTable.MatchString(t) || strings.Count(t, "x") != 2 || strings.Count(t, "p") != 2 {
			return fmt.Errorf("自定义编码表格式错误（每个 8 位，正好 2 个 x、2 个 p、4 个 v，如 xpxvvpvv）")
		}
	}
	return nil
}

func validSSPassword(cipher, password string) error {
	keyLen, ok := ssCiphers[cipher]
	if !ok {
		return fmt.Errorf("不支持的 SS 加密方式: %s", cipher)
	}
	if keyLen > 0 {
		if !reBase64Key.MatchString(password) {
			return fmt.Errorf("SS 2022 密码必须是 %d 字节密钥的 base64", keyLen)
		}
		raw, err := base64.StdEncoding.DecodeString(password)
		if err != nil || len(raw) != keyLen {
			return fmt.Errorf("SS 2022 密码必须是 %d 字节密钥的 base64", keyLen)
		}
		return nil
	}
	if !reDeployPass.MatchString(password) {
		return fmt.Errorf("SS 密码需为 8-64 位字母数字 _.-")
	}
	return nil
}

// GenerateSSPassword returns a random password suitable for the cipher.
func GenerateSSPassword(cipher string) (string, error) {
	keyLen, ok := ssCiphers[cipher]
	if !ok {
		return "", fmt.Errorf("不支持的 SS 加密方式: %s", cipher)
	}
	if keyLen > 0 {
		buf := make([]byte, keyLen)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		return base64.StdEncoding.EncodeToString(buf), nil
	}
	return RandomHex(16), nil
}

// RandomHex returns 2n random hex characters.
func RandomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// NewUUID returns a random RFC 4122 version 4 UUID.
func NewUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func randomPort(used map[int]bool) (int, error) {
	var b [2]byte
	for i := 0; i < 200; i++ {
		if _, err := rand.Read(b[:]); err != nil {
			return 0, err
		}
		port := 20000 + int(binary.BigEndian.Uint16(b[:]))%40000
		if !used[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("无法分配随机端口")
}

func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'"'"'`) + "'"
}

// InstallArgs returns the `install -y ...` arguments for mihomo_script.
func InstallArgs(p DeployParams) []string {
	args := []string{"install", "-y", "--protocols", strings.Join(p.Protocols, ",")}
	if p.Host != "" {
		args = append(args, "--host", p.Host)
	}
	if p.ScriptName != "" {
		args = append(args, "--name", p.ScriptName)
	}
	args = append(args, "--ip-version", p.IPVersion)
	if p.BlockPrivate {
		args = append(args, "--block-private", "1")
	} else {
		args = append(args, "--block-private", "0")
	}
	if p.MihomoVersion != "" && p.MihomoVersion != "latest" {
		args = append(args, "--version", p.MihomoVersion)
	}
	if p.HasProtocol(DeployProtoSS) {
		args = append(args, "--ss-port", strconv.Itoa(p.SSPort), "--ss-cipher", p.SSCipher, "--ss-password", p.SSPassword)
	}
	if p.HasProtocol(DeployProtoMieru) {
		args = append(args, "--mieru-port", strconv.Itoa(p.MieruPort), "--mieru-user", p.MieruUser,
			"--mieru-password", p.MieruPassword, "--mieru-low-entropy", p.MieruLowEntropy)
	}
	if p.HasProtocol(DeployProtoSudoku) {
		args = append(args, "--sudoku-port", strconv.Itoa(p.SudokuPort))
		if p.SudokuKeyMode == "psk" && p.SudokuKey != "" {
			args = append(args, "--sudoku-key", p.SudokuKey)
		}
		args = append(args, "--sudoku-table", p.SudokuTable, "--sudoku-padding", p.SudokuPadding)
		if p.SudokuPureDownlink {
			args = append(args, "--sudoku-pure-downlink", "1")
		} else {
			args = append(args, "--sudoku-pure-downlink", "0")
		}
		if strings.TrimSpace(p.SudokuCustomTables) != "" {
			args = append(args, "--sudoku-custom-tables", strings.TrimSpace(p.SudokuCustomTables))
		}
	}
	return args
}

func quotedArgs(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		if reShellSafeArg.MatchString(a) {
			parts[i] = a
		} else {
			parts[i] = shellQuote(a)
		}
	}
	return strings.Join(parts, " ")
}

// BuildInstallCommand returns the one-click command to paste on the server.
func BuildInstallCommand(p DeployParams, scriptURL string) string {
	if scriptURL == "" {
		scriptURL = DefaultMihomoScriptURL
	}
	return fmt.Sprintf("bash <(curl -fsSL %s) %s", shellQuote(scriptURL), quotedArgs(InstallArgs(p)))
}

// Share-link markers printed around /etc/mihomo/share.txt by BuildSSHScript.
const (
	ShareBeginMarker = "===FLVX-MIHOMO-SHARE-BEGIN==="
	ShareEndMarker   = "===FLVX-MIHOMO-SHARE-END==="
)

// BuildSSHScript returns a POSIX shell script that downloads mihomo_script,
// runs the unattended install and prints the resulting share links.
func BuildSSHScript(p DeployParams, scriptURL string, useSudo bool) string {
	if scriptURL == "" {
		scriptURL = DefaultMihomoScriptURL
	}
	run := ""
	if useSudo {
		run = "sudo -n "
	}
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("command -v curl >/dev/null 2>&1 || { echo '缺少 curl，请先安装 curl' >&2; exit 1; }\n")
	b.WriteString("command -v bash >/dev/null 2>&1 || { echo '缺少 bash' >&2; exit 1; }\n")
	b.WriteString("tmp=$(mktemp /tmp/flvx-mihomo-XXXXXX.sh)\n")
	b.WriteString("trap 'rm -f \"$tmp\"' EXIT\n")
	fmt.Fprintf(&b, "curl -fsSL --retry 3 --connect-timeout 15 -m 120 %s -o \"$tmp\"\n", shellQuote(scriptURL))
	fmt.Fprintf(&b, "%sbash \"$tmp\" %s\n", run, quotedArgs(InstallArgs(p)))
	fmt.Fprintf(&b, "echo %s\n", ShareBeginMarker)
	fmt.Fprintf(&b, "%scat /etc/mihomo/share.txt\n", run)
	fmt.Fprintf(&b, "echo %s\n", ShareEndMarker)
	return b.String()
}

// ExtractShareLinks returns the share links printed between the markers (or,
// without markers, every line that looks like a supported share link).
func ExtractShareLinks(output string) []string {
	text := output
	if start := strings.Index(output, ShareBeginMarker); start >= 0 {
		text = output[start+len(ShareBeginMarker):]
		if end := strings.Index(text, ShareEndMarker); end >= 0 {
			text = text[:end]
		}
	}
	links := []string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		for _, scheme := range []string{"ss://", "mierus://", "mieru://", "sudoku://"} {
			if idx := strings.Index(line, scheme); idx >= 0 {
				links = append(links, strings.TrimSpace(line[idx:]))
				break
			}
		}
	}
	return links
}

// ApplyShareLinks updates params from links printed by the script (used to
// pick up a Sudoku key pair generated on the server). It returns the protocols
// that were found.
func ApplyShareLinks(p *DeployParams, links []string) []string {
	found := []string{}
	for _, link := range links {
		proxy, err := ParseShareLink(link)
		if err != nil {
			continue
		}
		switch Type(proxy) {
		case "sudoku":
			if key := GetString(proxy, "key"); key != "" {
				p.SudokuKey = key
				if p.SudokuKeyMode == "psk" {
					p.SudokuServerKey = key
				}
			}
			if port := Port(proxy); port > 0 {
				p.SudokuPort = port
			}
			found = append(found, DeployProtoSudoku)
		case "ss":
			if port := Port(proxy); port > 0 {
				p.SSPort = port
			}
			if c := GetString(proxy, "cipher"); c != "" {
				p.SSCipher = c
			}
			if pw := GetString(proxy, "password"); pw != "" {
				p.SSPassword = pw
			}
			found = append(found, DeployProtoSS)
		case "mieru":
			if port := Port(proxy); port > 0 {
				p.MieruPort = port
			}
			if u := GetString(proxy, "username"); u != "" {
				p.MieruUser = u
			}
			if pw := GetString(proxy, "password"); pw != "" {
				p.MieruPassword = pw
			}
			found = append(found, DeployProtoMieru)
		}
	}
	sort.Strings(found)
	return found
}

// ClientProxies returns the Clash client nodes for a deployment, keyed by
// protocol, using the same settings as mihomo_script's client configuration.
func ClientProxies(p DeployParams, namePrefix string) map[string]Proxy {
	out := map[string]Proxy{}
	host := strings.Trim(strings.TrimSpace(p.Host), "[]")
	if namePrefix == "" {
		namePrefix = host
	}
	if p.HasProtocol(DeployProtoSS) {
		out[DeployProtoSS] = Proxy{
			"name":     namePrefix + "-SS",
			"type":     "ss",
			"server":   host,
			"port":     p.SSPort,
			"cipher":   p.SSCipher,
			"password": p.SSPassword,
			"udp":      true,
		}
	}
	if p.HasProtocol(DeployProtoMieru) {
		out[DeployProtoMieru] = Proxy{
			"name":            namePrefix + "-mieru",
			"type":            "mieru",
			"server":          host,
			"port":            p.MieruPort,
			"transport":       "TCP",
			"udp":             true,
			"username":        p.MieruUser,
			"password":        p.MieruPassword,
			"multiplexing":    "MULTIPLEXING_LOW",
			"handshake-mode":  "HANDSHAKE_STANDARD",
			"traffic-pattern": MieruTrafficPattern(p.MieruLowEntropy),
		}
	}
	if p.HasProtocol(DeployProtoSudoku) && p.SudokuKey != "" {
		minV, maxV, _ := ParsePadding(p.SudokuPadding)
		proxy := Proxy{
			"name":                 namePrefix + "-Sudoku",
			"type":                 "sudoku",
			"server":               host,
			"port":                 p.SudokuPort,
			"key":                  p.SudokuKey,
			"aead-method":          "chacha20-poly1305",
			"padding-min":          minV,
			"padding-max":          maxV,
			"table-type":           p.SudokuTable,
			"enable-pure-downlink": p.SudokuPureDownlink,
			"httpmask":             map[string]any{"disable": true},
		}
		if tables := stringList(p.SudokuCustomTables); len(tables) > 0 {
			list := make([]any, 0, len(tables))
			for _, t := range tables {
				list = append(list, t)
			}
			proxy["custom-tables"] = list
		}
		out[DeployProtoSudoku] = proxy
	}
	return out
}

// ScriptSafeName returns name if it is accepted by mihomo_script's --name.
func ScriptSafeName(name string) string {
	name = strings.TrimSpace(name)
	if reDeployNodeName.MatchString(name) {
		return name
	}
	return ""
}
