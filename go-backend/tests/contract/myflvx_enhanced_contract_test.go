package contract_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"go-backend/internal/auth"
	"go-backend/internal/security"
	"go-backend/internal/store/repo"
)

// Contract tests for the myflvx additions: 代理节点, 订阅管理/生成/下载 (per
// user), 隧道中转, 部署节点 (through the node agent) and backup compatibility.

type apiResult struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func callAPI(t *testing.T, router http.Handler, token, path string, payload interface{}) apiResult {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s payload: %v", path, err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var out apiResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s response %q: %v", path, rec.Body.String(), err)
	}
	return out
}

func mustAPI(t *testing.T, router http.Handler, token, path string, payload interface{}, out interface{}) {
	t.Helper()
	res := callAPI(t, router, token, path, payload)
	if res.Code != 0 {
		t.Fatalf("%s failed: code=%d msg=%s", path, res.Code, res.Msg)
	}
	if out != nil && len(res.Data) > 0 {
		if err := json.Unmarshal(res.Data, out); err != nil {
			t.Fatalf("decode %s data: %v (%s)", path, err, string(res.Data))
		}
	}
}

func tokenFor(t *testing.T, userID int64, name string, roleID int, secret string) string {
	t.Helper()
	token, err := auth.GenerateToken(userID, name, roleID, secret)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return token
}

type proxyNodeListData struct {
	Nodes []struct {
		ID          int64          `json:"id"`
		Name        string         `json:"name"`
		Protocol    string         `json:"protocol"`
		Server      string         `json:"server"`
		Port        int            `json:"port"`
		Source      string         `json:"source"`
		ShareLink   string         `json:"shareLink"`
		Tags        []string       `json:"tags"`
		Enabled     bool           `json:"enabled"`
		ParentID    int64          `json:"parentId"`
		ForwardID   int64          `json:"forwardId"`
		RelayStatus string         `json:"relayStatus"`
		Config      map[string]any `json:"config"`
	} `json:"nodes"`
	Tags []string `json:"tags"`
}

const testShareLinks = "trojan://secret@example.com:443?sni=example.com#Trojan-01\n" +
	"ss://YWVzLTEyOC1nY206cHcxMjM0NTY@203.0.113.10:8388#SS-01\n"

func TestMyflvxProxyNodesArePerUser(t *testing.T) {
	secret := "myflvx-node-secret"
	router, r := setupContractRouter(t, secret)
	seedContractUser(t, r, 2, "plain_user", 1, 1)
	admin := tokenFor(t, 1, "admin_user", 0, secret)
	user := tokenFor(t, 2, "plain_user", 1, secret)

	var imported struct {
		Created int `json:"created"`
		Skipped int `json:"skipped"`
	}
	mustAPI(t, router, admin, "/api/v1/proxy-node/import", map[string]interface{}{"content": testShareLinks, "tags": []string{"测试"}}, &imported)
	if imported.Created != 2 {
		t.Fatalf("expected 2 imported nodes, got %+v", imported)
	}
	mustAPI(t, router, admin, "/api/v1/proxy-node/import", map[string]interface{}{"content": testShareLinks, "skipDuplicates": true}, &imported)
	if imported.Created != 0 || imported.Skipped != 2 {
		t.Fatalf("duplicates should be skipped, got %+v", imported)
	}

	// A regular user can use 代理节点 too; duplicates are only checked against their own nodes.
	mustAPI(t, router, user, "/api/v1/proxy-node/import", map[string]interface{}{"content": testShareLinks, "skipDuplicates": true, "tags": []string{"我的"}}, &imported)
	if imported.Created != 2 {
		t.Fatalf("user import should not see the admin's nodes as duplicates, got %+v", imported)
	}

	var adminList, userList proxyNodeListData
	mustAPI(t, router, admin, "/api/v1/proxy-node/list", map[string]interface{}{}, &adminList)
	mustAPI(t, router, user, "/api/v1/proxy-node/list", map[string]interface{}{}, &userList)
	if len(adminList.Nodes) != 2 || len(adminList.Tags) != 1 || adminList.Tags[0] != "测试" {
		t.Fatalf("admin should only see own nodes: %+v", adminList)
	}
	if len(userList.Nodes) != 2 || len(userList.Tags) != 1 || userList.Tags[0] != "我的" {
		t.Fatalf("user should only see own nodes: %+v", userList)
	}
	for _, n := range adminList.Nodes {
		if n.ShareLink == "" || n.Source != "import" {
			t.Fatalf("node should carry share link and import source: %+v", n)
		}
	}

	adminNode := adminList.Nodes[0].ID
	// The user cannot touch the admin's nodes.
	if res := callAPI(t, router, user, "/api/v1/proxy-node/update", map[string]interface{}{"id": adminNode, "name": "hijack"}); res.Code == 0 {
		t.Fatalf("user must not update another user's node")
	}
	mustAPI(t, router, user, "/api/v1/proxy-node/toggle", map[string]interface{}{"ids": []int64{adminNode}, "enabled": false}, nil)
	mustAPI(t, router, user, "/api/v1/proxy-node/tags", map[string]interface{}{"ids": []int64{adminNode}, "tags": []string{"x"}}, nil)
	if res := callAPI(t, router, user, "/api/v1/proxy-node/delete", map[string]interface{}{"ids": []int64{adminNode}}); res.Code == 0 {
		t.Fatalf("user must not delete another user's node")
	}
	var links struct {
		Links string `json:"links"`
	}
	mustAPI(t, router, user, "/api/v1/proxy-node/share-links", map[string]interface{}{"ids": []int64{adminNode}}, &links)
	if links.Links != "" {
		t.Fatalf("user must not export another user's share links: %q", links.Links)
	}
	if res := callAPI(t, router, user, "/api/v1/proxy-node/relay/create", map[string]interface{}{"nodeId": adminNode, "tunnelId": 1}); res.Code == 0 {
		t.Fatalf("user must not relay another user's node")
	}
	mustAPI(t, router, admin, "/api/v1/proxy-node/list", map[string]interface{}{}, &adminList)
	for _, n := range adminList.Nodes {
		if n.ID == adminNode && (!n.Enabled || n.Name == "hijack" || len(n.Tags) != 1 || n.Tags[0] != "测试") {
			t.Fatalf("admin node was changed by another user: %+v", n)
		}
	}

	if res := callAPI(t, router, user, "/api/v1/deploy/list", map[string]interface{}{}); res.Code != 403 {
		t.Fatalf("non-admin must not list deployments, got code=%d", res.Code)
	}

	// Deleting a user removes their nodes.
	mustAPI(t, router, admin, "/api/v1/user/delete", map[string]interface{}{"id": 2}, nil)
	if n := mustQueryInt(t, r, `SELECT COUNT(*) FROM proxy_node WHERE user_id = 2`); n != 0 {
		t.Fatalf("deleted user's nodes should be removed, %d left", n)
	}
}

func TestMyflvxSubscriptionsArePerUser(t *testing.T) {
	secret := "myflvx-sub-secret"
	router, r := setupContractRouter(t, secret)
	seedContractUser(t, r, 2, "sub_user", 1, 1)
	seedContractUser(t, r, 3, "other_user", 1, 1)
	admin := tokenFor(t, 1, "admin_user", 0, secret)
	user := tokenFor(t, 2, "sub_user", 1, secret)
	other := tokenFor(t, 3, "other_user", 1, secret)

	mustAPI(t, router, admin, "/api/v1/proxy-node/import", map[string]interface{}{"content": "ss://YWVzLTEyOC1nY206cHcxMjM0NTY@203.0.113.99:8388#Admin-Only\n"}, nil)
	mustAPI(t, router, user, "/api/v1/proxy-node/import", map[string]interface{}{"content": testShareLinks}, nil)

	var created struct {
		ID int64 `json:"id"`
	}
	mustAPI(t, router, user, "/api/v1/subscription/create", map[string]interface{}{
		"name": "家庭订阅", "type": "generate",
		"genConfig": map[string]interface{}{"allNodes": true, "template": "minimal"},
	}, &created)
	if created.ID <= 0 {
		t.Fatal("subscription not created")
	}

	var mine []struct {
		ID int64 `json:"id"`
	}
	mustAPI(t, router, user, "/api/v1/subscription/my/list", map[string]interface{}{}, &mine)
	if len(mine) != 1 || mine[0].ID != created.ID {
		t.Fatalf("owner should see the subscription: %+v", mine)
	}
	for _, token := range []string{other, admin} {
		mustAPI(t, router, token, "/api/v1/subscription/my/list", map[string]interface{}{}, &mine)
		if len(mine) != 0 {
			t.Fatalf("other users (including the admin) must not see the subscription: %+v", mine)
		}
		mustAPI(t, router, token, "/api/v1/subscription/list", map[string]interface{}{}, &mine)
		if len(mine) != 0 {
			t.Fatalf("other users (including the admin) must not manage the subscription: %+v", mine)
		}
		if res := callAPI(t, router, token, "/api/v1/subscription/my/download", map[string]interface{}{"id": created.ID}); res.Code == 0 {
			t.Fatalf("other users must not download the subscription")
		}
		if res := callAPI(t, router, token, "/api/v1/subscription/get", map[string]interface{}{"id": created.ID}); res.Code == 0 {
			t.Fatalf("other users must not read the subscription")
		}
	}
	mustAPI(t, router, other, "/api/v1/subscription/delete", map[string]interface{}{"ids": []int64{created.ID}}, nil)
	if n := mustQueryInt(t, r, `SELECT COUNT(*) FROM sub_file WHERE id = ?`, created.ID); n != 1 {
		t.Fatal("other users must not delete the subscription")
	}

	var file struct {
		FileName string `json:"fileName"`
		Content  string `json:"content"`
	}
	mustAPI(t, router, user, "/api/v1/subscription/my/download", map[string]interface{}{"id": created.ID, "format": "clash"}, &file)
	if file.FileName != "家庭订阅.yaml" || !strings.Contains(file.Content, "proxy-groups:") || !strings.Contains(file.Content, "Trojan-01") {
		t.Fatalf("unexpected clash download: %s\n%s", file.FileName, file.Content)
	}
	if strings.Contains(file.Content, "Admin-Only") {
		t.Fatalf("generated subscription must only use the owner's nodes:\n%s", file.Content)
	}
	if strings.Contains(file.Content, "sniffer") || strings.Contains(file.Content, "global-client-fingerprint") {
		t.Fatalf("default base config must not enable sniffer / client fingerprint:\n%s", file.Content)
	}
	mustAPI(t, router, user, "/api/v1/subscription/my/download", map[string]interface{}{"id": created.ID, "format": "base64"}, &file)
	decoded, err := base64.StdEncoding.DecodeString(file.Content)
	if err != nil || !strings.Contains(string(decoded), "trojan://") || !strings.Contains(string(decoded), "ss://") {
		t.Fatalf("base64 download should contain share links: %v %q", err, string(decoded))
	}

	// A disabled subscription leaves 订阅下载 but its owner can still download it.
	disabled := false
	mustAPI(t, router, user, "/api/v1/subscription/update", map[string]interface{}{
		"id": created.ID, "name": "家庭订阅", "enabled": disabled,
		"genConfig": map[string]interface{}{"allNodes": true, "template": "minimal"},
	}, nil)
	mustAPI(t, router, user, "/api/v1/subscription/my/list", map[string]interface{}{}, &mine)
	if len(mine) != 0 {
		t.Fatalf("disabled subscription should not be listed in 订阅下载: %+v", mine)
	}
	mustAPI(t, router, user, "/api/v1/subscription/my/download", map[string]interface{}{"id": created.ID}, &file)

	// Upload type keeps the uploaded YAML verbatim.
	upload := "mixed-port: 7890\nproxies:\n  - {name: up, type: socks5, server: 1.1.1.1, port: 1080}\nproxy-groups: []\nrules:\n  - MATCH,DIRECT\n"
	mustAPI(t, router, other, "/api/v1/subscription/create", map[string]interface{}{"name": "上传", "type": "upload", "content": upload}, &created)
	mustAPI(t, router, other, "/api/v1/subscription/my/download", map[string]interface{}{"id": created.ID}, &file)
	if strings.TrimSpace(file.Content) != strings.TrimSpace(upload) {
		t.Fatalf("uploaded content changed:\n%s", file.Content)
	}
}

func TestMyflvxBackupFormatsAndRestore(t *testing.T) {
	secret := "myflvx-backup-secret"
	router, r := setupContractRouter(t, secret)
	seedContractUser(t, r, 2, "backup_user", 1, 1)
	admin := tokenFor(t, 1, "admin_user", 0, secret)
	user := tokenFor(t, 2, "backup_user", 1, secret)

	mustAPI(t, router, admin, "/api/v1/proxy-node/import", map[string]interface{}{"content": testShareLinks}, nil)
	mustAPI(t, router, user, "/api/v1/subscription/create", map[string]interface{}{
		"name": "备份订阅", "type": "upload",
		"content": "proxies:\n  - {name: up, type: socks5, server: 1.1.1.1, port: 1080}\n",
	}, nil)
	now := time.Now().UnixMilli()
	if err := r.DB().Exec(`INSERT INTO forward(user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, created_time, updated_time, status, inx)
		VALUES(1, 'admin_user', 'fwd', 1, '1.1.1.1:443', 'fifo', 0, 0, ?, ?, 1, 0)`, now, now).Error; err != nil {
		t.Fatalf("seed forward: %v", err)
	}

	export := func(types []string, format string) map[string]json.RawMessage {
		t.Helper()
		body, _ := json.Marshal(map[string]interface{}{"types": types, "format": format})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/backup/export", bytes.NewReader(body))
		req.Header.Set("Authorization", admin)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var out map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode export: %v (%s)", err, rec.Body.String())
		}
		return out
	}

	all := []string{"users", "forwards", "proxyNodes", "subscriptions", "proxyDeploys"}
	full := export(all, "")
	if !strings.Contains(string(full["proxyNodes"]), `"userId":1`) || !strings.Contains(string(full["subscriptions"]), `"userId":2`) {
		t.Fatalf("full export must keep owners: %s / %s", full["proxyNodes"], full["subscriptions"])
	}
	upstream := export(all, "upstream")
	if _, ok := upstream["proxyNodes"]; ok {
		t.Fatal("upstream export must not contain myflvx sections")
	}
	alpha4 := export(all, "alpha4")
	if strings.Contains(string(alpha4["forwards"]), "proxyProtocol") {
		t.Fatalf("alpha4 export must drop fields alpha4 rejects: %s", alpha4["forwards"])
	}
	if _, ok := alpha4["subscriptions"]; ok {
		t.Fatal("alpha4 export must not contain myflvx sections")
	}

	// Restore the full backup into an empty panel: owners are kept.
	router2, r2 := setupContractRouter(t, secret)
	seedContractUser(t, r2, 2, "backup_user", 1, 1)
	payload := map[string]interface{}{"types": []string{"proxyNodes", "subscriptions"}}
	for k, v := range full {
		payload[k] = v
	}
	var result struct {
		ProxyNodesImported    int `json:"proxyNodesImported"`
		SubscriptionsImported int `json:"subscriptionsImported"`
	}
	mustAPI(t, router2, admin, "/api/v1/backup/import", payload, &result)
	if result.ProxyNodesImported != 2 || result.SubscriptionsImported != 1 {
		t.Fatalf("unexpected import result: %+v", result)
	}
	var mine []map[string]interface{}
	mustAPI(t, router2, user, "/api/v1/subscription/list", map[string]interface{}{}, &mine)
	if len(mine) != 1 {
		t.Fatalf("restored subscription should belong to its owner: %+v", mine)
	}

	// A myflvx 2.2.1 backup has no owners (and still carries userIds): the
	// rows go to the first administrator.
	router3, r3 := setupContractRouter(t, secret)
	legacy := map[string]interface{}{
		"types":      []string{"proxyNodes", "subscriptions"},
		"version":    "1.0",
		"exportedAt": now,
		"proxyNodes": []map[string]interface{}{{
			"id": 5, "name": "旧节点", "protocol": "ss", "server": "203.0.113.10", "port": 8388,
			"config": `{"name":"旧节点","type":"ss","server":"203.0.113.10","port":8388,"cipher":"aes-128-gcm","password":"pw123456"}`,
			"source": "manual", "enabled": 1, "inx": 0, "createdTime": now, "updatedTime": now,
		}},
		"subscriptions": []map[string]interface{}{{
			"id": 9, "name": "旧订阅", "type": "upload", "content": "proxies: []\n", "enabled": 1, "inx": 0,
			"createdTime": now, "updatedTime": now, "userIds": []int64{2},
		}},
	}
	mustAPI(t, router3, admin, "/api/v1/backup/import", legacy, nil)
	if owner := mustQueryInt(t, r3, `SELECT user_id FROM proxy_node WHERE id = 5`); owner != 1 {
		t.Fatalf("legacy node should belong to the admin, got %d", owner)
	}
	if owner := mustQueryInt(t, r3, `SELECT user_id FROM sub_file WHERE id = 9`); owner != 1 {
		t.Fatalf("legacy subscription should belong to the admin, got %d", owner)
	}
}

func TestMyflvxImportAlpha4BackupKeepsLegacyPasswords(t *testing.T) {
	secret := "myflvx-legacy-secret"
	router, r := setupContractRouter(t, secret)
	admin := tokenFor(t, 1, "admin_user", 0, secret)

	// A users section exactly as exported by FLVX 2.2.0-alpha4 (MD5 password).
	backup := map[string]interface{}{
		"types":      []string{"users"},
		"version":    "1.0",
		"exportedAt": time.Now().UnixMilli(),
		"users": []map[string]interface{}{{
			"id": 77, "user": "legacy_alpha4", "pwd": security.MD5("alpha4-pass"), "roleId": 1,
			"expTime": 2727251700000, "flow": 100, "inFlow": 0, "outFlow": 0, "flowResetTime": 1,
			"num": 10, "createdTime": time.Now().UnixMilli(), "status": 1,
		}},
	}
	mustAPI(t, router, admin, "/api/v1/backup/import", backup, nil)
	if status := mustQueryInt(t, r, `SELECT status FROM user WHERE id = 77`); status != 1 {
		t.Fatalf("restored alpha4 user must stay enabled, got status=%d", status)
	}
	var login struct {
		Token string `json:"token"`
	}
	mustAPI(t, router, "", "/api/v1/user/login", map[string]interface{}{"username": "legacy_alpha4", "password": "alpha4-pass"}, &login)
	if login.Token == "" {
		t.Fatal("legacy password should log in after restore")
	}
	if pwd := mustQueryString(t, r, `SELECT pwd FROM user WHERE id = 77`); !strings.HasPrefix(pwd, "$2") {
		t.Fatalf("legacy hash should be upgraded to bcrypt after login, got %q", pwd)
	}
}

// mockMihomoAgent answers MihomoScript commands like the node agent does.
type mockMihomoAgent struct {
	mu        sync.Mutex
	starts    []map[string]interface{}
	polls     int
	share     string
	fail      string // when set, every command fails with this message
	finishLog string
}

func (m *mockMihomoAgent) handle(cmdType string, data json.RawMessage) (bool, string, interface{}) {
	if cmdType != "MihomoScript" {
		return true, "OK", nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != "" {
		return false, m.fail, nil
	}
	var req map[string]interface{}
	_ = json.Unmarshal(data, &req)
	switch req["action"] {
	case "start":
		m.starts = append(m.starts, req)
		m.polls = 0
		return true, "OK", map[string]interface{}{"taskId": fmt.Sprintf("task-%d", len(m.starts)), "op": req["op"], "state": "running", "nextOffset": 0}
	case "poll":
		m.polls++
		if m.polls == 1 {
			return true, "OK", map[string]interface{}{"taskId": req["taskId"], "state": "running", "log": "\x1b[32m==>\x1b[0m 正在安装\n", "nextOffset": 20}
		}
		res := map[string]interface{}{"taskId": req["taskId"], "state": "success", "log": m.finishLog, "nextOffset": 20}
		if op, _ := m.starts[len(m.starts)-1]["op"].(string); op == "install" || op == "info" {
			res["share"] = m.share
		}
		return true, "OK", res
	}
	return false, "unknown action", nil
}

func (m *mockMihomoAgent) lastStart() map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.starts) == 0 {
		return nil
	}
	return m.starts[len(m.starts)-1]
}

// startMockAgentWithData connects a mock node agent whose handler can return response data.
func startMockAgentWithData(t *testing.T, baseURL, nodeSecret string, onCommand func(cmdType string, data json.RawMessage) (bool, string, interface{})) func() {
	t.Helper()
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	u.Scheme = "ws"
	u.Path = "/system-info"
	q := u.Query()
	q.Set("type", "1")
	q.Set("secret", nodeSecret)
	q.Set("version", "2.2.2")
	q.Set("http", "1")
	q.Set("tls", "1")
	q.Set("socks", "1")
	u.RawQuery = q.Encode()
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial mock agent: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			_, raw, readErr := conn.ReadMessage()
			if readErr != nil {
				return
			}
			plain := raw
			var wrap struct {
				Encrypted bool   `json:"encrypted"`
				Data      string `json:"data"`
			}
			if err := json.Unmarshal(raw, &wrap); err == nil && wrap.Encrypted && wrap.Data != "" {
				if crypto, err := security.NewAESCrypto(nodeSecret); err == nil {
					if dec, err := crypto.Decrypt(wrap.Data); err == nil {
						plain = []byte(dec)
					}
				}
			}
			var cmd struct {
				Type      string          `json:"type"`
				RequestID string          `json:"requestId"`
				Data      json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(plain, &cmd); err != nil || cmd.RequestID == "" {
				continue
			}
			ok, msg, data := onCommand(cmd.Type, cmd.Data)
			resp := map[string]interface{}{"type": cmd.Type + "Response", "success": ok, "message": msg, "requestId": cmd.RequestID}
			if data != nil {
				resp["data"] = data
			}
			b, _ := json.Marshal(resp)
			_ = conn.WriteMessage(websocket.TextMessage, b)
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = conn.Close()
			wg.Wait()
		})
	}
}

func insertMyflvxNode(t *testing.T, r *repo.Repository, name, secret, ip string) int64 {
	t.Helper()
	now := time.Now().UnixMilli()
	if err := r.DB().Exec(`INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
		VALUES(?, ?, ?, ?, '', '32000-32010', '', 'v1', 1, 1, 1, ?, ?, 1, '[::]', '[::]', 0)`, name, secret, ip, ip, now, now).Error; err != nil {
		t.Fatalf("insert node %s: %v", name, err)
	}
	return mustLastInsertID(t, r, "node")
}

type deployViewData struct {
	ID         int64                  `json:"id"`
	Name       string                 `json:"name"`
	NodeID     int64                  `json:"nodeId"`
	Host       string                 `json:"host"`
	Status     string                 `json:"status"`
	LastLog    string                 `json:"lastLog"`
	ProxyNodes int                    `json:"proxyNodes"`
	Params     map[string]interface{} `json:"params"`
}

func waitDeployStatus(t *testing.T, router http.Handler, token string, id int64, want string) deployViewData {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var view deployViewData
	for time.Now().Before(deadline) {
		mustAPI(t, router, token, "/api/v1/deploy/get", map[string]interface{}{"id": id}, &view)
		if view.Status == want {
			return view
		}
		if view.Status != "running" && view.Status != "pending" {
			t.Fatalf("deployment ended with %q (want %q):\n%s", view.Status, want, view.LastLog)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("deployment %d did not reach %q, last status %q:\n%s", id, want, view.Status, view.LastLog)
	return view
}

func TestMyflvxDeployRunsThroughNodeAgent(t *testing.T) {
	secret := "myflvx-deploy-secret"
	router, r := setupContractRouter(t, secret)
	admin := tokenFor(t, 1, "admin_user", 0, secret)
	seedContractUser(t, r, 2, "plain_user", 1, 1)
	user := tokenFor(t, 2, "plain_user", 1, secret)

	nodeA := insertMyflvxNode(t, r, "香港A", "deploy-node-a", "198.51.100.30")
	nodeB := insertMyflvxNode(t, r, "香港B", "deploy-node-b", "198.51.100.31")
	server := httptest.NewServer(router)
	defer server.Close()

	sudokuPayload := `{"n":"x","h":"198.51.100.30","p":24001,"k":"0123456789abcdef0123456789abcdef","a":"entropy","e":"chacha20-poly1305","m":1080,"x":true,"hd":true}`
	agent := &mockMihomoAgent{share: "sudoku://" + base64.RawURLEncoding.EncodeToString([]byte(sudokuPayload)) + "\n", finishLog: "安装完成\n"}
	stop := startMockAgentWithData(t, server.URL, "deploy-node-a", agent.handle)
	defer stop()
	oldAgent := &mockMihomoAgent{fail: "未知命令类型: MihomoScript"}
	stopB := startMockAgentWithData(t, server.URL, "deploy-node-b", oldAgent.handle)
	defer stopB()
	time.Sleep(300 * time.Millisecond)

	if res := callAPI(t, router, user, "/api/v1/deploy/create", map[string]interface{}{"nodeIds": []int64{nodeA}}); res.Code != 403 {
		t.Fatalf("non-admin must not deploy, got code=%d", res.Code)
	}

	var defaults struct {
		Params map[string]interface{} `json:"params"`
	}
	mustAPI(t, router, admin, "/api/v1/deploy/defaults", map[string]interface{}{}, &defaults)
	if defaults.Params["ssPassword"] == "" || defaults.Params["sudokuKey"] == "" {
		t.Fatalf("unexpected defaults: %+v", defaults.Params)
	}

	var created struct {
		Items    []deployViewData `json:"items"`
		Warnings []string         `json:"warnings"`
	}
	mustAPI(t, router, admin, "/api/v1/deploy/create", map[string]interface{}{
		"nodeIds": []int64{nodeA}, "params": defaults.Params, "run": true,
	}, &created)
	if len(created.Items) != 1 || len(created.Warnings) != 0 {
		t.Fatalf("unexpected create result: %+v", created)
	}
	deployA := created.Items[0]
	if deployA.Host != "198.51.100.30" || deployA.NodeID != nodeA || deployA.Name != "香港A" {
		t.Fatalf("deployment should use the FLVX node name and address: %+v", deployA)
	}
	start := agent.lastStart()
	if start == nil || start["op"] != "install" || !strings.HasPrefix(fmt.Sprint(start["scriptUrl"]), "https://") {
		t.Fatalf("agent should receive the install: %+v", start)
	}
	args := fmt.Sprint(start["args"])
	for _, want := range []string{"-y", "--protocols ss,mieru,sudoku", "--host 198.51.100.30"} {
		if !strings.Contains(args, want) {
			t.Fatalf("install args missing %q: %s", want, args)
		}
	}
	// Each FLVX node can only have one deployment.
	if res := callAPI(t, router, admin, "/api/v1/deploy/create", map[string]interface{}{"nodeIds": []int64{nodeA}, "params": defaults.Params}); res.Code == 0 || !strings.Contains(res.Msg, "已有部署") {
		t.Fatalf("a second deployment on the same node must be rejected, got code=%d msg=%s", res.Code, res.Msg)
	}

	done := waitDeployStatus(t, router, admin, deployA.ID, "success")
	if done.ProxyNodes != 3 || !strings.Contains(done.LastLog, "正在安装") || strings.Contains(done.LastLog, "\x1b[") {
		t.Fatalf("unexpected finished deployment: %+v", done)
	}
	var list proxyNodeListData
	mustAPI(t, router, admin, "/api/v1/proxy-node/list", map[string]interface{}{}, &list)
	names := map[string]bool{}
	for _, n := range list.Nodes {
		names[n.Name] = true
		if n.Source != "deploy" || n.Server != "198.51.100.30" || n.ShareLink == "" {
			t.Fatalf("unexpected deployed node: %+v", n)
		}
		if n.Protocol == "sudoku" && (n.Port != 24001 || n.Config["key"] != "0123456789abcdef0123456789abcdef") {
			t.Fatalf("sudoku node should use the share link read back from the node: %+v", n)
		}
	}
	if !names["香港A-SS"] || !names["香港A-mieru"] || !names["香港A-Sudoku"] {
		t.Fatalf("deployed node names wrong: %v", names)
	}
	var userList proxyNodeListData
	mustAPI(t, router, user, "/api/v1/proxy-node/list", map[string]interface{}{}, &userList)
	if len(userList.Nodes) != 0 {
		t.Fatalf("deployed nodes belong to the admin only: %+v", userList.Nodes)
	}

	// Copy the parameters to another FLVX node: same ports and secrets, its own address.
	var copied struct {
		Items    []deployViewData `json:"items"`
		Warnings []string         `json:"warnings"`
	}
	mustAPI(t, router, admin, "/api/v1/deploy/copy", map[string]interface{}{"id": deployA.ID, "nodeIds": []int64{nodeA, nodeB}, "run": true}, &copied)
	if len(copied.Items) != 1 || copied.Items[0].NodeID != nodeB || copied.Items[0].Host != "198.51.100.31" || copied.Items[0].Name != "香港B" {
		t.Fatalf("copy should create one deployment for the other node: %+v", copied)
	}
	if copied.Items[0].Params["ssPassword"] != done.Params["ssPassword"] || copied.Items[0].Params["ssPort"] != done.Params["ssPort"] {
		t.Fatalf("copy should keep the parameters: %+v vs %+v", copied.Items[0].Params, done.Params)
	}
	if len(copied.Warnings) != 1 || !strings.Contains(copied.Warnings[0], "升级") {
		t.Fatalf("an old agent should be reported with an upgrade hint: %+v", copied.Warnings)
	}

	// Uninstall disables the generated nodes.
	mustAPI(t, router, admin, "/api/v1/deploy/uninstall", map[string]interface{}{"id": deployA.ID}, nil)
	if op := agent.lastStart()["op"]; op != "uninstall" {
		t.Fatalf("agent should receive the uninstall, got %v", op)
	}
	waitDeployStatus(t, router, admin, deployA.ID, "uninstalled")
	mustAPI(t, router, admin, "/api/v1/proxy-node/list", map[string]interface{}{}, &list)
	for _, n := range list.Nodes {
		if n.Enabled {
			t.Fatalf("nodes of an uninstalled deployment should be disabled: %+v", n)
		}
	}

	var command struct {
		Command string `json:"command"`
	}
	mustAPI(t, router, admin, "/api/v1/deploy/command", map[string]interface{}{"id": deployA.ID}, &command)
	if !strings.Contains(command.Command, "install -y --protocols ss,mieru,sudoku") {
		t.Fatalf("manual command missing install options: %s", command.Command)
	}

	mustAPI(t, router, admin, "/api/v1/deploy/delete", map[string]interface{}{"id": deployA.ID, "deleteNodes": true}, nil)
	mustAPI(t, router, admin, "/api/v1/proxy-node/list", map[string]interface{}{}, &list)
	if len(list.Nodes) != 0 {
		t.Fatalf("deployment nodes should be deleted: %+v", list.Nodes)
	}
}

func TestMyflvxTunnelRelayCreatesForwardAndRelayNode(t *testing.T) {
	secret := "myflvx-relay-secret"
	router, r := setupContractRouter(t, secret)
	admin := tokenFor(t, 1, "admin_user", 0, secret)
	now := time.Now().UnixMilli()

	if err := r.DB().Exec(`INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES('香港入口', 1.0, 1, 'tls', 99999, ?, ?, 1, NULL, 0)`, now, now).Error; err != nil {
		t.Fatalf("insert tunnel: %v", err)
	}
	tunnelID := mustLastInsertID(t, r, "tunnel")
	nodeID := insertMyflvxNode(t, r, "relay-entry", "relay-node-secret", "198.51.100.20")
	if err := r.DB().Exec(`INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol) VALUES(?, 1, ?, 32001, 'round', 1, 'tls')`, tunnelID, nodeID).Error; err != nil {
		t.Fatalf("insert chain_tunnel: %v", err)
	}

	server := httptest.NewServer(router)
	defer server.Close()
	stop := startMockNodeSession(t, server.URL, "relay-node-secret")
	defer stop()
	time.Sleep(200 * time.Millisecond)

	var created struct {
		ID int64 `json:"id"`
	}
	mustAPI(t, router, admin, "/api/v1/proxy-node/create", map[string]interface{}{"link": "ss://YWVzLTEyOC1nY206cHcxMjM0NTY@203.0.113.10:8388#落地"}, &created)

	var relay struct {
		ForwardID int64 `json:"forwardId"`
		Nodes     []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Server string `json:"server"`
			Port   int    `json:"port"`
		} `json:"nodes"`
	}
	mustAPI(t, router, admin, "/api/v1/proxy-node/relay/create", map[string]interface{}{"nodeId": created.ID, "tunnelId": tunnelID, "inPort": 32005}, &relay)
	if relay.ForwardID <= 0 || len(relay.Nodes) != 1 {
		t.Fatalf("relay not created: %+v", relay)
	}
	rn := relay.Nodes[0]
	if rn.Server != "198.51.100.20" || rn.Port != 32005 || rn.Name != "落地 | 香港入口" {
		t.Fatalf("relay node should use the forward entry: %+v", rn)
	}
	if addr := mustQueryString(t, r, `SELECT remote_addr FROM forward WHERE id = ?`, relay.ForwardID); addr != "203.0.113.10:8388" {
		t.Fatalf("forward should target the source node, got %q", addr)
	}

	// Moving the source node re-points the forward; the relay keeps the entry.
	mustAPI(t, router, admin, "/api/v1/proxy-node/update", map[string]interface{}{
		"id": created.ID, "link": "ss://YWVzLTEyOC1nY206cHcxMjM0NTY@203.0.113.11:9999#落地",
	}, nil)
	if addr := mustQueryString(t, r, `SELECT remote_addr FROM forward WHERE id = ?`, relay.ForwardID); addr != "203.0.113.11:9999" {
		t.Fatalf("forward should follow the source node, got %q", addr)
	}
	var list proxyNodeListData
	mustAPI(t, router, admin, "/api/v1/proxy-node/list", map[string]interface{}{}, &list)
	for _, n := range list.Nodes {
		if n.ID == rn.ID && (n.Server != "198.51.100.20" || n.Port != 32005 || n.RelayStatus != "ok") {
			t.Fatalf("relay node should keep the entry endpoint: %+v", n)
		}
	}

	// Another user's forwards are never offered for binding.
	seedContractUser(t, r, 2, "relay_user", 1, 1)
	user := tokenFor(t, 2, "relay_user", 1, secret)
	var userNode struct {
		ID int64 `json:"id"`
	}
	mustAPI(t, router, user, "/api/v1/proxy-node/create", map[string]interface{}{"link": "ss://YWVzLTEyOC1nY206cHcxMjM0NTY@203.0.113.11:9999#用户节点"}, &userNode)
	var candidates []map[string]interface{}
	mustAPI(t, router, user, "/api/v1/proxy-node/relay/candidates", map[string]interface{}{"nodeId": userNode.ID}, &candidates)
	if len(candidates) != 0 {
		t.Fatalf("the admin's forward must not be offered to another user: %+v", candidates)
	}
	if res := callAPI(t, router, user, "/api/v1/proxy-node/relay/bind", map[string]interface{}{"nodeId": userNode.ID, "forwardId": relay.ForwardID}); res.Code == 0 {
		t.Fatalf("user must not bind the admin's forward")
	}

	// Deleting the source node removes its relay and, on request, the forward.
	mustAPI(t, router, admin, "/api/v1/proxy-node/delete", map[string]interface{}{"ids": []int64{created.ID}, "deleteForwards": true}, nil)
	if n := mustQueryInt(t, r, `SELECT COUNT(*) FROM proxy_node WHERE user_id = 1`); n != 0 {
		t.Fatalf("source and relay nodes should be deleted, %d left", n)
	}
	if n := mustQueryInt(t, r, `SELECT COUNT(*) FROM forward WHERE id = ?`, relay.ForwardID); n != 0 {
		t.Fatalf("relay forward should be deleted")
	}
}

func TestMyflvxDragSortOrders(t *testing.T) {
	secret := "myflvx-order-secret"
	router, r := setupContractRouter(t, secret)
	seedContractUser(t, r, 2, "order_user", 1, 1)
	admin := tokenFor(t, 1, "admin_user", 0, secret)
	user := tokenFor(t, 2, "order_user", 1, secret)

	nodeNames := func(token string) []string {
		t.Helper()
		var list proxyNodeListData
		mustAPI(t, router, token, "/api/v1/proxy-node/list", map[string]interface{}{}, &list)
		names := []string{}
		for _, n := range list.Nodes {
			names = append(names, n.Name)
		}
		return names
	}
	mustAPI(t, router, admin, "/api/v1/proxy-node/import", map[string]interface{}{"content": testShareLinks}, nil)
	mustAPI(t, router, user, "/api/v1/proxy-node/import", map[string]interface{}{"content": testShareLinks}, nil)
	var adminList proxyNodeListData
	mustAPI(t, router, admin, "/api/v1/proxy-node/list", map[string]interface{}{}, &adminList)
	if got := strings.Join(nodeNames(admin), ","); got != "Trojan-01,SS-01" {
		t.Fatalf("unexpected initial order: %s", got)
	}
	first, second := adminList.Nodes[0].ID, adminList.Nodes[1].ID
	reversed := map[string]interface{}{"items": []map[string]interface{}{{"id": second, "inx": 0}, {"id": first, "inx": 1}}}
	mustAPI(t, router, admin, "/api/v1/proxy-node/update-order", reversed, nil)
	if got := strings.Join(nodeNames(admin), ","); got != "SS-01,Trojan-01" {
		t.Fatalf("admin order not saved: %s", got)
	}
	// Another user's order request must not touch the admin's nodes.
	mustAPI(t, router, user, "/api/v1/proxy-node/update-order", map[string]interface{}{"items": []map[string]interface{}{{"id": first, "inx": 0}, {"id": second, "inx": 1}}}, nil)
	if got := strings.Join(nodeNames(admin), ","); got != "SS-01,Trojan-01" {
		t.Fatalf("another user changed the admin's order: %s", got)
	}

	// Subscriptions: 订阅管理 and 订阅下载 share the order.
	var a, b struct {
		ID int64 `json:"id"`
	}
	upload := "proxies:\n  - {name: up, type: socks5, server: 1.1.1.1, port: 1080}\n"
	mustAPI(t, router, user, "/api/v1/subscription/create", map[string]interface{}{"name": "A", "type": "upload", "content": upload}, &a)
	mustAPI(t, router, user, "/api/v1/subscription/create", map[string]interface{}{"name": "B", "type": "upload", "content": upload}, &b)
	mustAPI(t, router, user, "/api/v1/subscription/update-order", map[string]interface{}{"items": []map[string]interface{}{{"id": b.ID, "inx": 0}, {"id": a.ID, "inx": 1}}}, nil)
	for _, path := range []string{"/api/v1/subscription/list", "/api/v1/subscription/my/list"} {
		var subs []struct {
			Name string `json:"name"`
		}
		mustAPI(t, router, user, path, map[string]interface{}{}, &subs)
		if len(subs) != 2 || subs[0].Name != "B" || subs[1].Name != "A" {
			t.Fatalf("%s should follow the saved order: %+v", path, subs)
		}
	}

	// Deployments: new ones are appended, update-order is admin-only.
	nodeA := insertMyflvxNode(t, r, "排序A", "order-node-a", "198.51.100.40")
	nodeB := insertMyflvxNode(t, r, "排序B", "order-node-b", "198.51.100.41")
	var defaults struct {
		Params map[string]interface{} `json:"params"`
	}
	mustAPI(t, router, admin, "/api/v1/deploy/defaults", map[string]interface{}{}, &defaults)
	mustAPI(t, router, admin, "/api/v1/deploy/create", map[string]interface{}{"nodeIds": []int64{nodeA, nodeB}, "params": defaults.Params}, nil)
	var deploys []deployViewData
	mustAPI(t, router, admin, "/api/v1/deploy/list", map[string]interface{}{}, &deploys)
	if len(deploys) != 2 || deploys[0].Name != "排序A" || deploys[1].Name != "排序B" {
		t.Fatalf("unexpected initial deployment order: %+v", deploys)
	}
	order := map[string]interface{}{"items": []map[string]interface{}{{"id": deploys[1].ID, "inx": 0}, {"id": deploys[0].ID, "inx": 1}}}
	if res := callAPI(t, router, user, "/api/v1/deploy/update-order", order); res.Code != 403 {
		t.Fatalf("non-admin must not reorder deployments, got code=%d", res.Code)
	}
	mustAPI(t, router, admin, "/api/v1/deploy/update-order", order, nil)
	mustAPI(t, router, admin, "/api/v1/deploy/list", map[string]interface{}{}, &deploys)
	if deploys[0].Name != "排序B" || deploys[1].Name != "排序A" {
		t.Fatalf("deployment order not saved: %+v", deploys)
	}
	nodeC := insertMyflvxNode(t, r, "排序C", "order-node-c", "198.51.100.42")
	mustAPI(t, router, admin, "/api/v1/deploy/create", map[string]interface{}{"nodeIds": []int64{nodeC}, "params": defaults.Params}, nil)
	mustAPI(t, router, admin, "/api/v1/deploy/list", map[string]interface{}{}, &deploys)
	if len(deploys) != 3 || deploys[2].Name != "排序C" {
		t.Fatalf("a new deployment should be appended at the end: %+v", deploys)
	}
	// Copying to a node that already has a deployment is rejected as well.
	if res := callAPI(t, router, admin, "/api/v1/deploy/copy", map[string]interface{}{"id": deploys[2].ID, "nodeIds": []int64{nodeA}}); res.Code == 0 || !strings.Contains(res.Msg, "已有部署") {
		t.Fatalf("copying onto a deployed node must be rejected, got code=%d msg=%s", res.Code, res.Msg)
	}
	// Renaming the FLVX node renames the deployment.
	if err := r.DB().Exec(`UPDATE node SET name = ? WHERE id = ?`, "排序C-新", nodeC).Error; err != nil {
		t.Fatalf("rename node: %v", err)
	}
	mustAPI(t, router, admin, "/api/v1/deploy/list", map[string]interface{}{}, &deploys)
	if deploys[2].Name != "排序C-新" {
		t.Fatalf("deployment name should follow the node name: %+v", deploys[2])
	}
}
