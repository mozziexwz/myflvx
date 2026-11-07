package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/proxykit"
	"go-backend/internal/store/model"
)

// Node sources shown in 节点管理.
const (
	proxySourceManual = "manual"
	proxySourceImport = "import"
	proxySourceDeploy = "deploy"
	proxySourceRelay  = "relay"
)

type proxyNodeView struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Protocol    string         `json:"protocol"`
	Server      string         `json:"server"`
	Port        int            `json:"port"`
	Config      map[string]any `json:"config"`
	RawURL      string         `json:"rawUrl"`
	ShareLink   string         `json:"shareLink"`
	Tags        []string       `json:"tags"`
	Remark      string         `json:"remark"`
	Source      string         `json:"source"`
	SourceRef   string         `json:"sourceRef"`
	ParentID    int64          `json:"parentId"`
	ParentName  string         `json:"parentName,omitempty"`
	TunnelID    int64          `json:"tunnelId"`
	TunnelName  string         `json:"tunnelName,omitempty"`
	ForwardID   int64          `json:"forwardId"`
	ForwardName string         `json:"forwardName,omitempty"`
	RelayHost   string         `json:"relayHost"`
	RelayEntry  int64          `json:"relayEntry"`
	RelayStatus string         `json:"relayStatus,omitempty"`
	Enabled     bool           `json:"enabled"`
	Inx         int            `json:"inx"`
	CreatedTime int64          `json:"createdTime"`
	UpdatedTime int64          `json:"updatedTime"`
}

func splitTags(raw string) []string {
	out := []string{}
	for _, t := range strings.Split(raw, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func joinTags(tags []string) string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(strings.ReplaceAll(t, ",", " "))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return strings.Join(out, ",")
}

// proxyFromNode decodes the stored Clash config, using the node name column.
func proxyFromNode(n model.ProxyNode) (proxykit.Proxy, error) {
	var p proxykit.Proxy
	if err := json.Unmarshal([]byte(n.Config), &p); err != nil {
		return nil, fmt.Errorf("节点 %s 配置损坏: %w", n.Name, err)
	}
	p = proxykit.NormalizeNumbers(p)
	p["name"] = n.Name
	return p, nil
}

// applyProxyToNode copies a proxy definition into the node row.
func applyProxyToNode(n *model.ProxyNode, p proxykit.Proxy) error {
	p = proxykit.CloneProxy(p)
	if strings.TrimSpace(n.Name) != "" {
		p["name"] = strings.TrimSpace(n.Name)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	n.Name = proxykit.Name(p)
	n.Protocol = proxykit.Type(p)
	n.Server = proxykit.Server(p)
	n.Port = proxykit.Port(p)
	n.Config = string(raw)
	return nil
}

func (h *Handler) proxyNodeViews(nodes []model.ProxyNode) []proxyNodeView {
	tunnelNames, _ := h.repo.ListTunnelNames()
	forwardNames := map[int64]string{}
	if forwards, err := h.repo.ListForwardBriefs(); err == nil {
		for _, f := range forwards {
			forwardNames[f.ID] = f.Name
		}
	}
	byID := map[int64]model.ProxyNode{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	views := make([]proxyNodeView, 0, len(nodes))
	for _, n := range nodes {
		v := proxyNodeView{
			ID: n.ID, Name: n.Name, Protocol: n.Protocol, Server: n.Server, Port: n.Port, RawURL: n.RawURL,
			Tags: splitTags(n.Tags), Remark: n.Remark, Source: n.Source, SourceRef: n.SourceRef,
			ParentID: n.ParentID, TunnelID: n.TunnelID, ForwardID: n.ForwardID, RelayHost: n.RelayHost,
			RelayEntry: n.RelayEntry, Enabled: n.Enabled == 1, Inx: n.Inx, CreatedTime: n.CreatedTime, UpdatedTime: n.UpdatedTime,
		}
		if p, err := proxyFromNode(n); err == nil {
			v.Config = p
			if link, err := proxykit.ShareLink(p); err == nil {
				v.ShareLink = link
			}
		}
		if parent, ok := byID[n.ParentID]; ok {
			v.ParentName = parent.Name
		}
		if n.TunnelID > 0 {
			v.TunnelName = tunnelNames[n.TunnelID]
		}
		if n.Source == proxySourceRelay {
			if name, ok := forwardNames[n.ForwardID]; ok {
				v.ForwardName = name
				v.RelayStatus = "ok"
			} else {
				v.RelayStatus = "forward_missing"
			}
		}
		views = append(views, v)
	}
	return views
}

func (h *Handler) proxyNodeList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	nodes, err := h.repo.ListProxyNodes()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	nodes = h.refreshRelayNodes(r.Context(), nodes)
	tagSet := map[string]bool{}
	for _, n := range nodes {
		for _, t := range splitTags(n.Tags) {
			tagSet[t] = true
		}
	}
	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	response.WriteJSON(w, response.OK(map[string]interface{}{"nodes": h.proxyNodeViews(nodes), "tags": tags}))
}

type proxyNodeMutation struct {
	ID        int64          `json:"id"`
	Name      string         `json:"name"`
	Config    map[string]any `json:"config"`
	Link      string         `json:"link"`
	Tags      []string       `json:"tags"`
	Remark    string         `json:"remark"`
	Enabled   *bool          `json:"enabled"`
	RelayHost *string        `json:"relayHost"`
}

func proxyFromMutation(req proxyNodeMutation) (proxykit.Proxy, string, error) {
	link := strings.TrimSpace(req.Link)
	if link != "" {
		p, err := proxykit.ParseShareLink(link)
		if err != nil {
			return nil, "", fmt.Errorf("分享链接解析失败: %v", err)
		}
		if name := strings.TrimSpace(req.Name); name != "" {
			p["name"] = name
		}
		return p, link, nil
	}
	if len(req.Config) == 0 {
		return nil, "", fmt.Errorf("请填写分享链接或节点配置")
	}
	cfg := proxykit.Proxy(req.Config)
	if name := strings.TrimSpace(req.Name); name != "" {
		cfg["name"] = name
	}
	p, err := proxykit.PrepareProxy(cfg)
	if err != nil {
		return nil, "", err
	}
	return p, "", nil
}

func (h *Handler) proxyNodeCreate(w http.ResponseWriter, r *http.Request) {
	var req proxyNodeMutation
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	p, rawURL, err := proxyFromMutation(req)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	now := time.Now().UnixMilli()
	node := &model.ProxyNode{
		RawURL: rawURL, Tags: joinTags(req.Tags), Remark: strings.TrimSpace(req.Remark),
		Source: proxySourceManual, Enabled: 1, CreatedTime: now, UpdatedTime: now,
	}
	if req.Enabled != nil && !*req.Enabled {
		node.Enabled = 0
	}
	if err := applyProxyToNode(node, p); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if err := h.repo.CreateProxyNodes([]*model.ProxyNode{node}); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(h.proxyNodeViews([]model.ProxyNode{*node})[0]))
}

func (h *Handler) proxyNodeUpdate(w http.ResponseWriter, r *http.Request) {
	var req proxyNodeMutation
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	node, err := h.repo.GetProxyNode(req.ID)
	if err != nil || node == nil {
		response.WriteJSON(w, response.ErrDefault("节点不存在"))
		return
	}
	before := *node
	now := time.Now().UnixMilli()
	node.Tags = joinTags(req.Tags)
	node.Remark = strings.TrimSpace(req.Remark)
	if req.Enabled != nil {
		node.Enabled = 0
		if *req.Enabled {
			node.Enabled = 1
		}
	}
	node.UpdatedTime = now
	if name := strings.TrimSpace(req.Name); name != "" {
		node.Name = name
	}

	if node.Source == proxySourceRelay {
		// Relay nodes follow their source node and forward; only the display
		// fields and the connect address override are editable.
		if req.RelayHost != nil {
			node.RelayHost = strings.Trim(strings.TrimSpace(*req.RelayHost), "[]")
		}
		p, err := proxyFromNode(*node)
		if err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		if err := applyProxyToNode(node, p); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		if err := h.repo.SaveProxyNode(node); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
		refreshed := h.refreshRelayNodes(r.Context(), []model.ProxyNode{*node})
		response.WriteJSON(w, response.OK(h.proxyNodeViews(refreshed)[0]))
		return
	}

	if strings.TrimSpace(req.Link) != "" || len(req.Config) > 0 {
		p, rawURL, err := proxyFromMutation(req)
		if err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		if rawURL != "" {
			node.RawURL = rawURL
		}
		if err := applyProxyToNode(node, p); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
	} else if p, err := proxyFromNode(*node); err == nil {
		_ = applyProxyToNode(node, p)
	}
	if err := h.repo.SaveProxyNode(node); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	warnings := []string{}
	if claims, ok := requestClaims(r); ok {
		warnings = h.syncRelayChildren(r.Context(), claims, before, *node)
	}
	view := h.proxyNodeViews([]model.ProxyNode{*node})[0]
	response.WriteJSON(w, response.OK(map[string]interface{}{"node": view, "warnings": warnings}))
}

type idsRequest struct {
	IDs            []int64 `json:"ids"`
	Enabled        bool    `json:"enabled"`
	Tags           []string `json:"tags"`
	DeleteForwards bool    `json:"deleteForwards"`
}

func (h *Handler) proxyNodeDelete(w http.ResponseWriter, r *http.Request) {
	var req idsRequest
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	if len(req.IDs) == 0 {
		response.WriteJSON(w, response.ErrDefault("请选择节点"))
		return
	}
	all, err := h.repo.ListProxyNodes()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	// Deleting a source node also deletes its relay nodes.
	selected := map[int64]bool{}
	for _, id := range req.IDs {
		selected[id] = true
	}
	for _, n := range all {
		if n.ParentID > 0 && selected[n.ParentID] {
			selected[n.ID] = true
		}
	}
	ids := make([]int64, 0, len(selected))
	forwardIDs := map[int64]bool{}
	for _, n := range all {
		if selected[n.ID] {
			ids = append(ids, n.ID)
			if n.Source == proxySourceRelay && n.ForwardID > 0 {
				forwardIDs[n.ForwardID] = true
			}
		}
	}
	if err := h.repo.DeleteProxyNodes(ids); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	warnings := []string{}
	if req.DeleteForwards && len(forwardIDs) > 0 {
		claims, ok := requestClaims(r)
		if ok {
			for fid := range forwardIDs {
				if remaining, err := h.repo.ListProxyNodesByForward(fid); err == nil && len(remaining) > 0 {
					continue // still used by another relay node
				}
				if f, _ := h.repo.GetForwardBrief(fid); f == nil {
					continue
				}
				if _, err := h.invokeAsCaller(r.Context(), claims, h.forwardDelete, map[string]interface{}{"id": fid}); err != nil {
					warnings = append(warnings, fmt.Sprintf("删除转发 %d 失败: %v", fid, err))
				}
			}
		}
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"deleted": len(ids), "warnings": warnings}))
}

func (h *Handler) proxyNodeToggle(w http.ResponseWriter, r *http.Request) {
	var req idsRequest
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	enabled := 0
	if req.Enabled {
		enabled = 1
	}
	if err := h.repo.SetProxyNodesEnabled(req.IDs, enabled, time.Now().UnixMilli()); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) proxyNodeSetTags(w http.ResponseWriter, r *http.Request) {
	var req idsRequest
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	if err := h.repo.SetProxyNodesTags(req.IDs, joinTags(req.Tags), time.Now().UnixMilli()); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

type orderRequest struct {
	Items []struct {
		ID  int64 `json:"id"`
		Inx int   `json:"inx"`
	} `json:"items"`
}

func (h *Handler) proxyNodeUpdateOrder(w http.ResponseWriter, r *http.Request) {
	var req orderRequest
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	order := map[int64]int{}
	for _, item := range req.Items {
		order[item.ID] = item.Inx
	}
	if err := h.repo.UpdateProxyNodeOrder(order); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

type parseRequest struct {
	Content   string   `json:"content"`
	URL       string   `json:"url"`
	UserAgent string   `json:"userAgent"`
	Tags      []string `json:"tags"`
	Remark    string   `json:"remark"`
	SkipDup   bool     `json:"skipDuplicates"`
}

type parsedProxyBrief struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Server   string `json:"server"`
	Port     int    `json:"port"`
}

func (h *Handler) loadImportContent(ctx context.Context, req parseRequest) ([]byte, string, error) {
	if u := strings.TrimSpace(req.URL); u != "" {
		res, err := proxykit.FetchSubscription(ctx, u, req.UserAgent)
		if err != nil {
			return nil, "", err
		}
		return res.Content, u, nil
	}
	if strings.TrimSpace(req.Content) == "" {
		return nil, "", fmt.Errorf("请粘贴分享链接、订阅内容，或填写订阅地址")
	}
	return []byte(req.Content), "", nil
}

func (h *Handler) proxyNodeParse(w http.ResponseWriter, r *http.Request) {
	var req parseRequest
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	content, _, err := h.loadImportContent(r.Context(), req)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	res := proxykit.ParseContent(content)
	briefs := make([]parsedProxyBrief, 0, len(res.Proxies))
	for _, p := range res.Proxies {
		briefs = append(briefs, parsedProxyBrief{Name: proxykit.Name(p), Protocol: proxykit.Type(p), Server: proxykit.Server(p), Port: proxykit.Port(p)})
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"proxies": briefs, "failed": res.Failed, "format": res.Format}))
}

func proxyIdentity(p proxykit.Proxy) string {
	return strings.Join([]string{proxykit.Type(p), strings.ToLower(proxykit.Server(p)), fmt.Sprint(proxykit.Port(p)), proxykit.Name(p)}, "|")
}

func (h *Handler) proxyNodeImport(w http.ResponseWriter, r *http.Request) {
	var req parseRequest
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	content, sourceURL, err := h.loadImportContent(r.Context(), req)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	res := proxykit.ParseContent(content)
	if len(res.Proxies) == 0 {
		response.WriteJSON(w, response.ErrDefault("没有识别到可导入的节点"))
		return
	}
	existing := map[string]bool{}
	if req.SkipDup {
		if nodes, err := h.repo.ListProxyNodes(); err == nil {
			for _, n := range nodes {
				if p, err := proxyFromNode(n); err == nil {
					existing[proxyIdentity(p)] = true
				}
			}
		}
	}
	now := time.Now().UnixMilli()
	tags := joinTags(req.Tags)
	toCreate := []*model.ProxyNode{}
	skipped := 0
	for _, p := range res.Proxies {
		if req.SkipDup && existing[proxyIdentity(p)] {
			skipped++
			continue
		}
		existing[proxyIdentity(p)] = true
		node := &model.ProxyNode{
			Tags: tags, Remark: strings.TrimSpace(req.Remark), Source: proxySourceImport, SourceRef: sourceURL,
			Enabled: 1, CreatedTime: now, UpdatedTime: now,
		}
		if link, err := proxykit.ShareLink(p); err == nil {
			node.RawURL = link
		}
		if err := applyProxyToNode(node, p); err != nil {
			continue
		}
		toCreate = append(toCreate, node)
	}
	if err := h.repo.CreateProxyNodes(toCreate); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"created": len(toCreate), "skipped": skipped, "failed": res.Failed}))
}

func (h *Handler) proxyNodeShareLinks(w http.ResponseWriter, r *http.Request) {
	var req idsRequest
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	nodes, err := h.repo.GetProxyNodesByIDs(req.IDs)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	proxies := make([]proxykit.Proxy, 0, len(nodes))
	for _, n := range h.refreshRelayNodes(r.Context(), nodes) {
		if p, err := proxyFromNode(n); err == nil {
			proxies = append(proxies, p)
		}
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"links": proxykit.ShareLinks(proxies)}))
}
