package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go-backend/internal/auth"
	"go-backend/internal/http/middleware"
	"go-backend/internal/http/response"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

// registerEnhancedRoutes registers the myflvx subscription and deployment APIs.
// Access control lives in middleware.requiresAdmin: 部署节点 (/api/v1/deploy/*)
// is admin-only; proxy nodes and subscriptions are available to every user and
// always scoped to the caller's own rows.
func (h *Handler) registerEnhancedRoutes(mux *http.ServeMux) {
	// 代理节点
	mux.HandleFunc("/api/v1/proxy-node/list", h.proxyNodeList)
	mux.HandleFunc("/api/v1/proxy-node/create", h.proxyNodeCreate)
	mux.HandleFunc("/api/v1/proxy-node/update", h.proxyNodeUpdate)
	mux.HandleFunc("/api/v1/proxy-node/delete", h.proxyNodeDelete)
	mux.HandleFunc("/api/v1/proxy-node/toggle", h.proxyNodeToggle)
	mux.HandleFunc("/api/v1/proxy-node/tags", h.proxyNodeSetTags)
	mux.HandleFunc("/api/v1/proxy-node/update-order", h.proxyNodeUpdateOrder)
	mux.HandleFunc("/api/v1/proxy-node/parse", h.proxyNodeParse)
	mux.HandleFunc("/api/v1/proxy-node/import", h.proxyNodeImport)
	mux.HandleFunc("/api/v1/proxy-node/share-links", h.proxyNodeShareLinks)
	// 隧道中转
	mux.HandleFunc("/api/v1/proxy-node/relay/create", h.proxyNodeRelayCreate)
	mux.HandleFunc("/api/v1/proxy-node/relay/candidates", h.proxyNodeRelayCandidates)
	mux.HandleFunc("/api/v1/proxy-node/relay/bind", h.proxyNodeRelayBind)
	mux.HandleFunc("/api/v1/proxy-node/relay/refresh", h.proxyNodeRelayRefresh)
	// 订阅管理 / 生成订阅
	mux.HandleFunc("/api/v1/subscription/list", h.subscriptionList)
	mux.HandleFunc("/api/v1/subscription/get", h.subscriptionGet)
	mux.HandleFunc("/api/v1/subscription/create", h.subscriptionCreate)
	mux.HandleFunc("/api/v1/subscription/update", h.subscriptionUpdate)
	mux.HandleFunc("/api/v1/subscription/delete", h.subscriptionDelete)
	mux.HandleFunc("/api/v1/subscription/refresh", h.subscriptionRefresh)
	mux.HandleFunc("/api/v1/subscription/preview", h.subscriptionPreview)
	mux.HandleFunc("/api/v1/subscription/templates", h.subscriptionTemplates)
	mux.HandleFunc("/api/v1/subscription/update-order", h.subscriptionUpdateOrder)
	// 订阅下载
	mux.HandleFunc("/api/v1/subscription/my/list", h.subscriptionMyList)
	mux.HandleFunc("/api/v1/subscription/my/download", h.subscriptionMyDownload)
	// 部署节点（管理员）：面板通过节点端执行 mihomo_script
	mux.HandleFunc("/api/v1/deploy/list", h.deployList)
	mux.HandleFunc("/api/v1/deploy/get", h.deployGet)
	mux.HandleFunc("/api/v1/deploy/defaults", h.deployDefaults)
	mux.HandleFunc("/api/v1/deploy/create", h.deployCreate)
	mux.HandleFunc("/api/v1/deploy/update", h.deployUpdate)
	mux.HandleFunc("/api/v1/deploy/delete", h.deployDelete)
	mux.HandleFunc("/api/v1/deploy/copy", h.deployCopy)
	mux.HandleFunc("/api/v1/deploy/run", h.deployRun)
	mux.HandleFunc("/api/v1/deploy/refresh", h.deployRefresh)
	mux.HandleFunc("/api/v1/deploy/uninstall", h.deployUninstall)
	mux.HandleFunc("/api/v1/deploy/command", h.deployCommand)
	mux.HandleFunc("/api/v1/deploy/sync-nodes", h.deploySyncNodes)
	mux.HandleFunc("/api/v1/deploy/update-order", h.deployUpdateOrder)
	mux.HandleFunc("/api/v1/deploy/settings", h.deploySettings)
}

// callerID returns the logged-in user's id, writing a 401 response when the
// request carries no valid identity.
func callerID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	userID, _, err := userRoleFromRequest(r)
	if err != nil || userID <= 0 {
		response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
		return 0, false
	}
	return userID, true
}

// decodeEnhancedBody decodes a POST JSON body, writing an error response on failure.
func decodeEnhancedBody(w http.ResponseWriter, r *http.Request, out interface{}) bool {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return false
	}
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20))
	if err := decoder.Decode(out); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return false
	}
	return true
}

func requestClaims(r *http.Request) (auth.Claims, bool) {
	claims, ok := r.Context().Value(middleware.ClaimsContextKey).(auth.Claims)
	return claims, ok
}

// captureWriter collects the response of an internally invoked handler.
type captureWriter struct {
	header http.Header
	buf    bytes.Buffer
	status int
}

func (c *captureWriter) Header() http.Header {
	if c.header == nil {
		c.header = http.Header{}
	}
	return c.header
}

func (c *captureWriter) Write(b []byte) (int, error) { return c.buf.Write(b) }
func (c *captureWriter) WriteHeader(status int)      { c.status = status }

// invokeAsCaller runs an existing API handler with the caller's identity, so
// features such as tunnel relays reuse the exact forward create/update/delete
// logic (validation, port allocation, runtime dispatch) of the forward page.
func (h *Handler) invokeAsCaller(ctx context.Context, claims auth.Claims, fn http.HandlerFunc, payload interface{}) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(context.WithValue(ctx, middleware.ClaimsContextKey, claims), http.MethodPost, "/internal", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	w := &captureWriter{}
	fn(w, req)
	var out struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.buf.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("内部调用返回无法解析")
	}
	if out.Code != 0 {
		msg := strings.TrimSpace(out.Msg)
		if msg == "" {
			msg = "操作失败"
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return out.Data, nil
}

// convertBackupFormat projects a backup onto the schema an importer expects:
//   - full:     everything, including the myflvx sections
//   - upstream: upstream FLVX schema (myflvx sections dropped)
//   - alpha4:   FLVX 2.2.0-alpha4 schema (also drops fields added after alpha4)
func convertBackupFormat(data *repo.BackupData, format string) (interface{}, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "full":
		return data, nil
	case "upstream":
		data.ProxyNodes = nil
		data.Subscriptions = nil
		data.ProxyDeploys = nil
		return data, nil
	case "alpha4":
		data.ProxyNodes = nil
		data.Subscriptions = nil
		data.ProxyDeploys = nil
		raw, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		var legacy model.Alpha4BackupData
		if err := json.Unmarshal(raw, &legacy); err != nil {
			return nil, err
		}
		return &legacy, nil
	default:
		return nil, fmt.Errorf("不支持的导出格式: %s", format)
	}
}
