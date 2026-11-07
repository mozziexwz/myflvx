package contract_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/security"
)

// Contract tests for the myflvx additions: 节点管理, 订阅管理/生成/下载,
// 隧道中转, 部署节点 and backup compatibility.

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
		ParentID    int64          `json:"parentId"`
		ForwardID   int64          `json:"forwardId"`
		RelayStatus string         `json:"relayStatus"`
		Config      map[string]any `json:"config"`
	} `json:"nodes"`
	Tags []string `json:"tags"`
}

const testShareLinks = "trojan://secret@example.com:443?sni=example.com#Trojan-01\n" +
	"ss://YWVzLTEyOC1nY206cHcxMjM0NTY@203.0.113.10:8388#SS-01\n"

func TestMyflvxProxyNodeImportListAndPermissions(t *testing.T) {
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

	var list proxyNodeListData
	mustAPI(t, router, admin, "/api/v1/proxy-node/list", map[string]interface{}{}, &list)
	if len(list.Nodes) != 2 || len(list.Tags) != 1 || list.Tags[0] != "测试" {
		t.Fatalf("unexpected node list: %+v", list)
	}
	for _, n := range list.Nodes {
		if n.ShareLink == "" || n.Source != "import" {
			t.Fatalf("node should carry share link and import source: %+v", n)
		}
	}

	if res := callAPI(t, router, user, "/api/v1/proxy-node/list", map[string]interface{}{}); res.Code != 403 {
		t.Fatalf("non-admin must not list proxy nodes, got code=%d", res.Code)
	}
	if res := callAPI(t, router, user, "/api/v1/deploy/list", map[string]interface{}{}); res.Code != 403 {
		t.Fatalf("non-admin must not list deployments, got code=%d", res.Code)
	}
}

func TestMyflvxGeneratedSubscriptionDownloadAccess(t *testing.T) {
	secret := "myflvx-sub-secret"
	router, r := setupContractRouter(t, secret)
	seedContractUser(t, r, 2, "sub_user", 1, 1)
	seedContractUser(t, r, 3, "other_user", 1, 1)
	admin := tokenFor(t, 1, "admin_user", 0, secret)
	user := tokenFor(t, 2, "sub_user", 1, secret)
	other := tokenFor(t, 3, "other_user", 1, secret)

	mustAPI(t, router, admin, "/api/v1/proxy-node/import", map[string]interface{}{"content": testShareLinks}, nil)

	var created struct {
		ID int64 `json:"id"`
	}
	mustAPI(t, router, admin, "/api/v1/subscription/create", map[string]interface{}{
		"name": "家庭订阅", "type": "generate", "userIds": []int64{2},
		"genConfig": map[string]interface{}{"allNodes": true, "template": "minimal"},
	}, &created)
	if created.ID <= 0 {
		t.Fatal("subscription not created")
	}

	var mine []struct {
		ID        int64  `json:"id"`
		SourceURL string `json:"sourceUrl"`
	}
	mustAPI(t, router, user, "/api/v1/subscription/my/list", map[string]interface{}{}, &mine)
	if len(mine) != 1 || mine[0].ID != created.ID {
		t.Fatalf("assigned user should see the subscription: %+v", mine)
	}
	mustAPI(t, router, other, "/api/v1/subscription/my/list", map[string]interface{}{}, &mine)
	if len(mine) != 0 {
		t.Fatalf("unassigned user should see nothing: %+v", mine)
	}

	var file struct {
		FileName string `json:"fileName"`
		Content  string `json:"content"`
	}
	mustAPI(t, router, user, "/api/v1/subscription/my/download", map[string]interface{}{"id": created.ID, "format": "clash"}, &file)
	if file.FileName != "家庭订阅.yaml" || !strings.Contains(file.Content, "proxy-groups:") || !strings.Contains(file.Content, "Trojan-01") {
		t.Fatalf("unexpected clash download: %s\n%s", file.FileName, file.Content)
	}
	mustAPI(t, router, user, "/api/v1/subscription/my/download", map[string]interface{}{"id": created.ID, "format": "base64"}, &file)
	decoded, err := base64.StdEncoding.DecodeString(file.Content)
	if err != nil || !strings.Contains(string(decoded), "trojan://") || !strings.Contains(string(decoded), "ss://") {
		t.Fatalf("base64 download should contain share links: %v %q", err, string(decoded))
	}

	if res := callAPI(t, router, other, "/api/v1/subscription/my/download", map[string]interface{}{"id": created.ID}); res.Code != 403 {
		t.Fatalf("unassigned user must not download, got code=%d", res.Code)
	}
	if res := callAPI(t, router, user, "/api/v1/subscription/list", map[string]interface{}{}); res.Code != 403 {
		t.Fatalf("non-admin must not manage subscriptions, got code=%d", res.Code)
	}

	disabled := false
	mustAPI(t, router, admin, "/api/v1/subscription/update", map[string]interface{}{
		"id": created.ID, "name": "家庭订阅", "enabled": disabled, "userIds": []int64{2},
		"genConfig": map[string]interface{}{"allNodes": true, "template": "minimal"},
	}, nil)
	if res := callAPI(t, router, user, "/api/v1/subscription/my/download", map[string]interface{}{"id": created.ID}); res.Code != 403 {
		t.Fatalf("disabled subscription must not be downloadable, got code=%d", res.Code)
	}

	// Upload type keeps the uploaded YAML verbatim.
	upload := "mixed-port: 7890\nproxies:\n  - {name: up, type: socks5, server: 1.1.1.1, port: 1080}\nproxy-groups: []\nrules:\n  - MATCH,DIRECT\n"
	mustAPI(t, router, admin, "/api/v1/subscription/create", map[string]interface{}{"name": "上传", "type": "upload", "content": upload, "userIds": []int64{3}}, &created)
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

	mustAPI(t, router, admin, "/api/v1/proxy-node/import", map[string]interface{}{"content": testShareLinks}, nil)
	mustAPI(t, router, admin, "/api/v1/subscription/create", map[string]interface{}{
		"name": "备份订阅", "type": "generate", "userIds": []int64{2},
		"genConfig": map[string]interface{}{"allNodes": true, "template": "standard"},
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
	if _, ok := full["proxyNodes"]; !ok {
		t.Fatal("full export must contain proxyNodes")
	}
	if _, ok := full["subscriptions"]; !ok {
		t.Fatal("full export must contain subscriptions")
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

	// Restore the full backup into an empty panel.
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
	user := tokenFor(t, 2, "backup_user", 1, secret)
	var mine []map[string]interface{}
	mustAPI(t, router2, user, "/api/v1/subscription/my/list", map[string]interface{}{}, &mine)
	if len(mine) != 1 {
		t.Fatalf("restored subscription assignment lost: %+v", mine)
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

func TestMyflvxDeployLifecycle(t *testing.T) {
	secret := "myflvx-deploy-secret"
	router, _ := setupContractRouter(t, secret)
	admin := tokenFor(t, 1, "admin_user", 0, secret)

	var defaults struct {
		Params map[string]interface{} `json:"params"`
	}
	mustAPI(t, router, admin, "/api/v1/deploy/defaults", map[string]interface{}{"host": "203.0.113.9"}, &defaults)
	if defaults.Params["host"] != "203.0.113.9" || defaults.Params["ssPassword"] == "" {
		t.Fatalf("unexpected defaults: %+v", defaults.Params)
	}

	var deploy struct {
		ID      int64  `json:"id"`
		Command string `json:"command"`
		Status  string `json:"status"`
	}
	mustAPI(t, router, admin, "/api/v1/deploy/create", map[string]interface{}{"name": "HK-01", "params": defaults.Params}, &deploy)
	if deploy.ID <= 0 || deploy.Status != "pending" || !strings.Contains(deploy.Command, "install -y --protocols ss,mieru,sudoku") || !strings.Contains(deploy.Command, "--name HK-01") {
		t.Fatalf("unexpected deployment: %+v", deploy)
	}

	var synced struct {
		Nodes int `json:"nodes"`
	}
	mustAPI(t, router, admin, "/api/v1/deploy/sync-nodes", map[string]interface{}{"id": deploy.ID}, &synced)
	if synced.Nodes != 3 {
		t.Fatalf("expected 3 deployed nodes, got %d", synced.Nodes)
	}
	var list proxyNodeListData
	mustAPI(t, router, admin, "/api/v1/proxy-node/list", map[string]interface{}{}, &list)
	names := map[string]bool{}
	for _, n := range list.Nodes {
		names[n.Name] = true
		if n.Source != "deploy" || n.Server != "203.0.113.9" || n.ShareLink == "" {
			t.Fatalf("unexpected deployed node: %+v", n)
		}
	}
	if !names["HK-01-SS"] || !names["HK-01-mieru"] || !names["HK-01-Sudoku"] {
		t.Fatalf("deployed node names wrong: %v", names)
	}

	// Pasting the script output updates the Sudoku key (server generated pair).
	payload := `{"n":"x","h":"203.0.113.9","p":24001,"k":"0123456789abcdef0123456789abcdef","a":"entropy","e":"chacha20-poly1305","m":1080,"x":true,"hd":true}`
	output := "==> 安装完成\nsudoku://" + base64.RawURLEncoding.EncodeToString([]byte(payload)) + "\n"
	mustAPI(t, router, admin, "/api/v1/deploy/apply-output", map[string]interface{}{"id": deploy.ID, "output": output}, nil)
	mustAPI(t, router, admin, "/api/v1/proxy-node/list", map[string]interface{}{}, &list)
	for _, n := range list.Nodes {
		if n.Protocol == "sudoku" && (n.Port != 24001 || n.Config["key"] != "0123456789abcdef0123456789abcdef") {
			t.Fatalf("sudoku node not updated from output: %+v", n)
		}
	}

	mustAPI(t, router, admin, "/api/v1/deploy/delete", map[string]interface{}{"id": deploy.ID, "deleteNodes": true}, nil)
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
	if err := r.DB().Exec(`INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
		VALUES('relay-entry', 'relay-node-secret', '198.51.100.20', '198.51.100.20', '', '32000-32010', '', 'v1', 1, 1, 1, ?, ?, 1, '[::]', '[::]', 0)`, now, now).Error; err != nil {
		t.Fatalf("insert node: %v", err)
	}
	nodeID := mustLastInsertID(t, r, "node")
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

	// Deleting the source node removes its relay and, on request, the forward.
	mustAPI(t, router, admin, "/api/v1/proxy-node/delete", map[string]interface{}{"ids": []int64{created.ID}, "deleteForwards": true}, nil)
	if n := mustQueryInt(t, r, `SELECT COUNT(*) FROM proxy_node`); n != 0 {
		t.Fatalf("source and relay nodes should be deleted, %d left", n)
	}
	if n := mustQueryInt(t, r, `SELECT COUNT(*) FROM forward WHERE id = ?`, relay.ForwardID); n != 0 {
		t.Fatalf("relay forward should be deleted")
	}
}
