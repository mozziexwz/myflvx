package handler

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go-backend/internal/auth"
	"go-backend/internal/http/response"
	"go-backend/internal/proxykit"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

// 节点管理 → 选择隧道：通过隧道创建转发（目标为节点地址），并按转发入口生成中转节点。

type relayCreateRequest struct {
	NodeID    int64  `json:"nodeId"`
	TunnelID  int64  `json:"tunnelId"`
	ForwardID int64  `json:"forwardId"`
	InPort    int    `json:"inPort"`
	RelayHost string `json:"relayHost"`
	Name      string `json:"name"`
	AllEntry  bool   `json:"allEntries"`
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

// pickEntries returns the forward entries a relay should use.
func pickEntries(entries []repo.ForwardEntryEndpoint, all bool) []repo.ForwardEntryEndpoint {
	usable := make([]repo.ForwardEntryEndpoint, 0, len(entries))
	for _, e := range entries {
		if e.Port > 0 && e.Host != "" {
			usable = append(usable, e)
		}
	}
	if !all && len(usable) > 1 {
		return usable[:1]
	}
	return usable
}

// relayNodesForForward builds relay nodes for the entries of a forward.
func (h *Handler) relayNodesForForward(parent model.ProxyNode, tunnelID, forwardID int64, relayHost, baseName string, allEntries bool) ([]*model.ProxyNode, error) {
	entries, err := h.repo.ListForwardEntryEndpoints(forwardID)
	if err != nil {
		return nil, err
	}
	picked := pickEntries(entries, allEntries)
	if len(picked) == 0 {
		return nil, fmt.Errorf("转发没有可用的入口（请检查入口节点地址）")
	}
	parentProxy, err := proxyFromNode(parent)
	if err != nil {
		return nil, err
	}
	tunnelName, _ := h.repo.GetTunnelName(tunnelID)
	if baseName == "" {
		baseName = parent.Name
		if tunnelName != "" {
			baseName = parent.Name + " | " + tunnelName
		}
	}
	now := time.Now().UnixMilli()
	nodes := make([]*model.ProxyNode, 0, len(picked))
	for _, e := range picked {
		name := baseName
		if len(picked) > 1 {
			label := e.NodeName
			if label == "" {
				label = e.Host
			}
			name = baseName + " | " + label
		}
		host := e.Host
		if relayHost != "" {
			host = relayHost
		}
		node := &model.ProxyNode{
			UserID: parent.UserID, Name: name, Tags: parent.Tags, Remark: parent.Remark, Source: proxySourceRelay,
			SourceRef: fmt.Sprintf("forward:%d", forwardID), ParentID: parent.ID, TunnelID: tunnelID, ForwardID: forwardID,
			RelayHost: relayHost, RelayEntry: e.NodeID, Enabled: 1, CreatedTime: now, UpdatedTime: now,
		}
		if err := applyProxyToNode(node, proxykit.WithEndpoint(parentProxy, name, host, e.Port)); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (h *Handler) proxyNodeRelayCreate(w http.ResponseWriter, r *http.Request) {
	var req relayCreateRequest
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	claims, ok := requestClaims(r)
	if !ok {
		response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
		return
	}
	userID, ok := callerID(w, r)
	if !ok {
		return
	}
	parent, err := h.repo.GetUserProxyNode(userID, req.NodeID)
	if err != nil || parent == nil {
		response.WriteJSON(w, response.ErrDefault("节点不存在"))
		return
	}
	if parent.Source == proxySourceRelay {
		response.WriteJSON(w, response.ErrDefault("中转节点不能再次中转，请选择源节点"))
		return
	}
	if req.TunnelID <= 0 {
		response.WriteJSON(w, response.ErrDefault("请选择隧道"))
		return
	}
	if parent.Server == "" || parent.Port <= 0 {
		response.WriteJSON(w, response.ErrDefault("节点缺少地址或端口，无法创建转发"))
		return
	}
	remoteAddr := proxykit.HostPort(parent.Server, parent.Port)
	forwardName := truncateRunes("中转-"+parent.Name, 90)
	payload := map[string]interface{}{
		"name":       forwardName,
		"tunnelId":   req.TunnelID,
		"remoteAddr": remoteAddr,
		"strategy":   "fifo",
	}
	if req.InPort > 0 {
		payload["inPort"] = req.InPort
	}
	if _, err := h.invokeAsCaller(r.Context(), claims, h.forwardCreate, payload); err != nil {
		response.WriteJSON(w, response.ErrDefault("创建转发失败: "+err.Error()))
		return
	}
	forwardID, err := h.repo.FindLatestForwardID(userID, req.TunnelID, forwardName, remoteAddr)
	if err != nil || forwardID <= 0 {
		response.WriteJSON(w, response.ErrDefault("转发已创建，但未找到转发记录，请在转发列表中确认"))
		return
	}
	relayHost := strings.Trim(strings.TrimSpace(req.RelayHost), "[]")
	nodes, err := h.relayNodesForForward(*parent, req.TunnelID, forwardID, relayHost, strings.TrimSpace(req.Name), req.AllEntry)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault("转发已创建，但生成中转节点失败: "+err.Error()))
		return
	}
	if err := h.repo.CreateProxyNodes(nodes); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	created := make([]model.ProxyNode, 0, len(nodes))
	for _, n := range nodes {
		created = append(created, *n)
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"forwardId": forwardID, "nodes": h.proxyNodeViews(created)}))
}

// remoteAddrMatches reports whether a forward's remote address list contains host:port.
func remoteAddrMatches(remoteAddr, host string, port int) bool {
	want := strings.ToLower(proxykit.HostPort(host, port))
	for _, part := range strings.FieldsFunc(remoteAddr, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' }) {
		part = strings.TrimSpace(part)
		h, p, err := net.SplitHostPort(part)
		if err != nil {
			continue
		}
		pn, _ := strconv.Atoi(p)
		if strings.ToLower(proxykit.HostPort(h, pn)) == want {
			return true
		}
	}
	return false
}

func (h *Handler) proxyNodeRelayCandidates(w http.ResponseWriter, r *http.Request) {
	var req relayCreateRequest
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	userID, ok := callerID(w, r)
	if !ok {
		return
	}
	parent, err := h.repo.GetUserProxyNode(userID, req.NodeID)
	if err != nil || parent == nil {
		response.WriteJSON(w, response.ErrDefault("节点不存在"))
		return
	}
	// Only the caller's own forwards can be bound to their relay nodes.
	forwards, err := h.repo.ListForwardBriefsByUser(userID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	tunnelNames, _ := h.repo.ListTunnelNames()
	out := []map[string]interface{}{}
	for _, f := range forwards {
		if !remoteAddrMatches(f.RemoteAddr, parent.Server, parent.Port) {
			continue
		}
		entries, _ := h.repo.ListForwardEntryEndpoints(f.ID)
		out = append(out, map[string]interface{}{
			"id": f.ID, "name": f.Name, "tunnelId": f.TunnelID, "tunnelName": tunnelNames[f.TunnelID],
			"userName": f.UserName, "remoteAddr": f.RemoteAddr, "entries": entries, "status": f.Status,
		})
	}
	response.WriteJSON(w, response.OK(out))
}

func (h *Handler) proxyNodeRelayBind(w http.ResponseWriter, r *http.Request) {
	var req relayCreateRequest
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	userID, ok := callerID(w, r)
	if !ok {
		return
	}
	parent, err := h.repo.GetUserProxyNode(userID, req.NodeID)
	if err != nil || parent == nil {
		response.WriteJSON(w, response.ErrDefault("节点不存在"))
		return
	}
	if parent.Source == proxySourceRelay {
		response.WriteJSON(w, response.ErrDefault("中转节点不能再次中转，请选择源节点"))
		return
	}
	forward, err := h.repo.GetForwardBrief(req.ForwardID)
	if err != nil || forward == nil || forward.UserID != userID {
		response.WriteJSON(w, response.ErrDefault("转发不存在"))
		return
	}
	relayHost := strings.Trim(strings.TrimSpace(req.RelayHost), "[]")
	nodes, err := h.relayNodesForForward(*parent, forward.TunnelID, forward.ID, relayHost, strings.TrimSpace(req.Name), req.AllEntry)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if err := h.repo.CreateProxyNodes(nodes); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	created := make([]model.ProxyNode, 0, len(nodes))
	for _, n := range nodes {
		created = append(created, *n)
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"forwardId": forward.ID, "nodes": h.proxyNodeViews(created)}))
}

func (h *Handler) proxyNodeRelayRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	userID, ok := callerID(w, r)
	if !ok {
		return
	}
	nodes, err := h.repo.ListProxyNodesByUser(userID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	nodes = h.refreshRelayNodes(r.Context(), nodes)
	response.WriteJSON(w, response.OK(h.proxyNodeViews(nodes)))
}

// refreshRelayNodes re-derives relay nodes from their source node and the
// current forward entry (port or entry address may have been edited on the
// forward page) and persists any change. Non-relay nodes pass through.
func (h *Handler) refreshRelayNodes(_ context.Context, nodes []model.ProxyNode) []model.ProxyNode {
	parents := map[int64]*model.ProxyNode{}
	for i := range nodes {
		parents[nodes[i].ID] = &nodes[i]
	}
	entryCache := map[int64][]repo.ForwardEntryEndpoint{}
	out := make([]model.ProxyNode, len(nodes))
	for i, n := range nodes {
		out[i] = n
		if n.Source != proxySourceRelay || n.ForwardID <= 0 {
			continue
		}
		entries, ok := entryCache[n.ForwardID]
		if !ok {
			entries, _ = h.repo.ListForwardEntryEndpoints(n.ForwardID)
			entryCache[n.ForwardID] = entries
		}
		var entry *repo.ForwardEntryEndpoint
		for j := range entries {
			if entries[j].Port <= 0 {
				continue
			}
			if n.RelayEntry == 0 || entries[j].NodeID == n.RelayEntry {
				entry = &entries[j]
				break
			}
		}
		if entry == nil {
			continue
		}
		host := entry.Host
		if n.RelayHost != "" {
			host = n.RelayHost
		}
		if host == "" {
			continue
		}
		var base proxykit.Proxy
		parent := parents[n.ParentID]
		if parent == nil && n.ParentID > 0 {
			parent, _ = h.repo.GetProxyNode(n.ParentID)
		}
		if parent != nil {
			base, _ = proxyFromNode(*parent)
		}
		if base == nil {
			base, _ = proxyFromNode(n)
		}
		if base == nil {
			continue
		}
		updated := n
		if err := applyProxyToNode(&updated, proxykit.WithEndpoint(base, n.Name, host, entry.Port)); err != nil {
			continue
		}
		if updated.Config != n.Config || updated.Server != n.Server || updated.Port != n.Port || updated.Protocol != n.Protocol {
			updated.UpdatedTime = time.Now().UnixMilli()
			if err := h.repo.SaveProxyNode(&updated); err == nil {
				out[i] = updated
			}
		}
	}
	return out
}

// syncRelayChildren keeps relay nodes and their forwards in line after the
// source node changed: forwards are re-pointed at the new address and the
// relay node configs are rebuilt from the new source config.
func (h *Handler) syncRelayChildren(ctx context.Context, claims auth.Claims, before, after model.ProxyNode) []string {
	warnings := []string{}
	children, err := h.repo.ListProxyNodeChildren(after.ID)
	if err != nil || len(children) == 0 {
		return warnings
	}
	addressChanged := before.Server != after.Server || before.Port != after.Port
	if addressChanged && after.Server != "" && after.Port > 0 {
		newAddr := proxykit.HostPort(after.Server, after.Port)
		done := map[int64]bool{}
		for _, c := range children {
			if c.ForwardID <= 0 || done[c.ForwardID] {
				continue
			}
			done[c.ForwardID] = true
			f, _ := h.repo.GetForwardBrief(c.ForwardID)
			if f == nil || !remoteAddrMatches(f.RemoteAddr, before.Server, before.Port) {
				continue
			}
			if _, err := h.invokeAsCaller(ctx, claims, h.forwardUpdate, map[string]interface{}{"id": c.ForwardID, "remoteAddr": newAddr}); err != nil {
				warnings = append(warnings, fmt.Sprintf("更新转发「%s」目标地址失败: %v", f.Name, err))
			}
		}
	}
	all := append([]model.ProxyNode{after}, children...)
	h.refreshRelayNodes(ctx, all)
	return warnings
}
