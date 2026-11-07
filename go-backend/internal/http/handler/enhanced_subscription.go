package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/proxykit"
	"go-backend/internal/store/model"
)

// Subscription types in 订阅管理.
const (
	subTypeUpload   = "upload"
	subTypeImport   = "import"
	subTypeGenerate = "generate"
)

type subFileView struct {
	ID           int64                      `json:"id"`
	Name         string                     `json:"name"`
	Remark       string                     `json:"remark"`
	Type         string                     `json:"type"`
	SourceURL    string                     `json:"sourceUrl,omitempty"`
	UserAgent    string                     `json:"userAgent,omitempty"`
	FileName     string                     `json:"fileName"`
	SubInfo      *proxykit.SubscriptionInfo `json:"subInfo,omitempty"`
	AutoUpdate   int                        `json:"autoUpdate"`
	Enabled      bool                       `json:"enabled"`
	ExpireTime   int64                      `json:"expireTime"`
	LastSyncTime int64                      `json:"lastSyncTime"`
	Inx          int                        `json:"inx"`
	CreatedTime  int64                      `json:"createdTime"`
	UpdatedTime  int64                      `json:"updatedTime"`
	UserIDs      []int64                    `json:"userIds,omitempty"`
	NodeCount    int                        `json:"nodeCount"`
	GenConfig    *proxykit.GeneratorConfig  `json:"genConfig,omitempty"`
	Content      string                     `json:"content,omitempty"`
}

func subFileToView(s model.SubFile, userIDs []int64, withContent bool) subFileView {
	v := subFileView{
		ID: s.ID, Name: s.Name, Remark: s.Remark, Type: s.Type, SourceURL: s.SourceURL, UserAgent: s.UserAgent,
		FileName: subscriptionFileName(s, "clash"), SubInfo: proxykit.ParseSubscriptionUserInfo(s.SubInfo),
		AutoUpdate: s.AutoUpdate, Enabled: s.Enabled == 1, ExpireTime: s.ExpireTime, LastSyncTime: s.LastSyncTime,
		Inx: s.Inx, CreatedTime: s.CreatedTime, UpdatedTime: s.UpdatedTime, UserIDs: userIDs,
	}
	if s.Type == subTypeGenerate {
		if cfg, err := decodeGenConfig(s.GenConfig); err == nil {
			if withContent {
				v.GenConfig = &cfg
			}
			if cfg.AllNodes {
				v.NodeCount = -1
			} else {
				v.NodeCount = len(cfg.NodeIDs)
			}
		}
	} else if proxies, err := proxykit.ParseClashProxies([]byte(s.Content)); err == nil {
		v.NodeCount = len(proxies)
	}
	if withContent {
		v.Content = s.Content
	}
	return v
}

func decodeGenConfig(raw string) (proxykit.GeneratorConfig, error) {
	var cfg proxykit.GeneratorConfig
	if strings.TrimSpace(raw) == "" {
		return cfg, fmt.Errorf("缺少生成配置")
	}
	err := json.Unmarshal([]byte(raw), &cfg)
	return cfg, err
}

var unsafeFileNameChars = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]+`)

func subscriptionFileName(s model.SubFile, format string) string {
	base := strings.TrimSpace(s.FileName)
	if base == "" {
		base = strings.TrimSpace(s.Name)
	}
	base = strings.TrimSpace(unsafeFileNameChars.ReplaceAllString(base, "_"))
	if base == "" {
		base = "subscription"
	}
	lower := strings.ToLower(base)
	for _, ext := range []string{".yaml", ".yml", ".txt"} {
		if strings.HasSuffix(lower, ext) {
			base = base[:len(base)-len(ext)]
			break
		}
	}
	if format == "clash" {
		return base + ".yaml"
	}
	return base + ".txt"
}

func (h *Handler) subscriptionList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	items, err := h.repo.ListSubFiles()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	users, _ := h.repo.SubFileUserMap()
	views := make([]subFileView, 0, len(items))
	for _, s := range items {
		views = append(views, subFileToView(s, users[s.ID], false))
	}
	response.WriteJSON(w, response.OK(views))
}

func (h *Handler) subscriptionGet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	s, err := h.repo.GetSubFile(req.ID)
	if err != nil || s == nil {
		response.WriteJSON(w, response.ErrDefault("订阅不存在"))
		return
	}
	users, _ := h.repo.SubFileUserMap()
	response.WriteJSON(w, response.OK(subFileToView(*s, users[s.ID], true)))
}

type subscriptionMutation struct {
	ID         int64                     `json:"id"`
	Name       string                    `json:"name"`
	Remark     string                    `json:"remark"`
	Type       string                    `json:"type"`
	Content    string                    `json:"content"`
	SourceURL  string                    `json:"sourceUrl"`
	UserAgent  string                    `json:"userAgent"`
	GenConfig  *proxykit.GeneratorConfig `json:"genConfig"`
	FileName   string                    `json:"fileName"`
	UserIDs    []int64                   `json:"userIds"`
	Enabled    *bool                     `json:"enabled"`
	AutoUpdate int                       `json:"autoUpdate"`
	ExpireTime int64                     `json:"expireTime"`
	Refetch    bool                      `json:"refetch"`
}

// normalizeUploadedContent accepts a Clash YAML document, or a share-link /
// base64 list which is turned into a full config with the standard template.
func normalizeSubscriptionContent(content []byte) (string, error) {
	text := strings.TrimSpace(strings.TrimPrefix(string(content), "\xef\xbb\xbf"))
	if text == "" {
		return "", fmt.Errorf("订阅内容为空")
	}
	if _, err := proxykit.ValidateClashDocument([]byte(text)); err == nil {
		return text + "\n", nil
	}
	parsed := proxykit.ParseContent([]byte(text))
	if len(parsed.Proxies) == 0 {
		return "", fmt.Errorf("无法识别订阅内容：需要 Clash/mihomo YAML、分享链接列表或 base64 订阅")
	}
	res, err := proxykit.Render(proxykit.GeneratorConfig{Template: "standard"}, parsed.Proxies)
	if err != nil {
		return "", err
	}
	return res.Content, nil
}

func (h *Handler) fetchImportedSubscription(ctx context.Context, s *model.SubFile) error {
	res, err := proxykit.FetchSubscription(ctx, s.SourceURL, s.UserAgent)
	if err != nil {
		return err
	}
	content, err := normalizeSubscriptionContent(res.Content)
	if err != nil {
		return err
	}
	s.Content = content
	s.SubInfo = strings.TrimSpace(res.UserInfo)
	s.LastSyncTime = time.Now().UnixMilli()
	return nil
}

func (h *Handler) applySubscriptionMutation(ctx context.Context, s *model.SubFile, req subscriptionMutation, creating bool) error {
	if name := strings.TrimSpace(req.Name); name != "" {
		s.Name = name
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("订阅名称不能为空")
	}
	s.Remark = strings.TrimSpace(req.Remark)
	s.FileName = strings.TrimSpace(req.FileName)
	if req.AutoUpdate >= 0 {
		s.AutoUpdate = req.AutoUpdate
	}
	if req.ExpireTime >= 0 {
		s.ExpireTime = req.ExpireTime
	}
	if req.Enabled != nil {
		s.Enabled = 0
		if *req.Enabled {
			s.Enabled = 1
		}
	}

	switch s.Type {
	case subTypeUpload:
		if strings.TrimSpace(req.Content) != "" || creating {
			content, err := normalizeSubscriptionContent([]byte(req.Content))
			if err != nil {
				return err
			}
			s.Content = content
		}
	case subTypeImport:
		newURL := strings.TrimSpace(req.SourceURL)
		if newURL == "" && creating {
			return fmt.Errorf("请填写订阅地址")
		}
		changed := creating || req.Refetch || (newURL != "" && newURL != s.SourceURL) || strings.TrimSpace(req.UserAgent) != s.UserAgent
		if newURL != "" {
			if _, err := proxykit.ValidateSubscriptionURL(newURL); err != nil {
				return err
			}
			s.SourceURL = newURL
		}
		s.UserAgent = strings.TrimSpace(req.UserAgent)
		if changed {
			if err := h.fetchImportedSubscription(ctx, s); err != nil {
				return err
			}
		}
	case subTypeGenerate:
		if req.GenConfig == nil {
			if creating {
				return fmt.Errorf("缺少生成配置")
			}
			break
		}
		raw, err := json.Marshal(req.GenConfig)
		if err != nil {
			return err
		}
		s.GenConfig = string(raw)
		res, err := h.renderGeneratedSubscription(ctx, *req.GenConfig)
		if err != nil {
			return err
		}
		s.Content = res.Content
	default:
		return fmt.Errorf("订阅类型无效")
	}
	return nil
}

func (h *Handler) subscriptionCreate(w http.ResponseWriter, r *http.Request) {
	var req subscriptionMutation
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	now := time.Now().UnixMilli()
	s := &model.SubFile{Type: strings.TrimSpace(req.Type), Enabled: 1, CreatedTime: now, UpdatedTime: now}
	if err := h.applySubscriptionMutation(r.Context(), s, req, true); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if err := h.repo.CreateSubFile(s, req.UserIDs); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(subFileToView(*s, req.UserIDs, false)))
}

func (h *Handler) subscriptionUpdate(w http.ResponseWriter, r *http.Request) {
	var req subscriptionMutation
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	s, err := h.repo.GetSubFile(req.ID)
	if err != nil || s == nil {
		response.WriteJSON(w, response.ErrDefault("订阅不存在"))
		return
	}
	if err := h.applySubscriptionMutation(r.Context(), s, req, false); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	s.UpdatedTime = time.Now().UnixMilli()
	if err := h.repo.SaveSubFile(s, req.UserIDs, req.UserIDs != nil); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	users, _ := h.repo.SubFileUserMap()
	response.WriteJSON(w, response.OK(subFileToView(*s, users[s.ID], false)))
}

func (h *Handler) subscriptionDelete(w http.ResponseWriter, r *http.Request) {
	var req idsRequest
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	if err := h.repo.DeleteSubFiles(req.IDs); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) subscriptionRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	s, err := h.repo.GetSubFile(req.ID)
	if err != nil || s == nil {
		response.WriteJSON(w, response.ErrDefault("订阅不存在"))
		return
	}
	switch s.Type {
	case subTypeImport:
		if err := h.fetchImportedSubscription(r.Context(), s); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		if err := h.repo.UpdateSubFileSync(s.ID, s.Content, s.SubInfo, "", s.LastSyncTime); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	case subTypeGenerate:
		cfg, err := decodeGenConfig(s.GenConfig)
		if err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		res, err := h.renderGeneratedSubscription(r.Context(), cfg)
		if err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		_ = h.repo.UpdateSubFileCache(s.ID, res.Content)
		s.Content = res.Content
	default:
		response.WriteJSON(w, response.ErrDefault("上传的订阅无需刷新"))
		return
	}
	users, _ := h.repo.SubFileUserMap()
	response.WriteJSON(w, response.OK(subFileToView(*s, users[s.ID], false)))
}

func (h *Handler) subscriptionUpdateOrder(w http.ResponseWriter, r *http.Request) {
	var req orderRequest
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	order := map[int64]int{}
	for _, item := range req.Items {
		order[item.ID] = item.Inx
	}
	if err := h.repo.UpdateSubFileOrder(order); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

// resolveGeneratorNodes returns the enabled nodes selected by a generator config.
func (h *Handler) resolveGeneratorNodes(ctx context.Context, cfg proxykit.GeneratorConfig) ([]proxykit.Proxy, error) {
	nodes, err := h.repo.ListProxyNodes()
	if err != nil {
		return nil, err
	}
	nodes = h.refreshRelayNodes(ctx, nodes)
	byID := map[int64]model.ProxyNode{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	selected := []model.ProxyNode{}
	if cfg.AllNodes {
		tagFilter := map[string]bool{}
		for _, t := range cfg.Tags {
			if t = strings.TrimSpace(t); t != "" {
				tagFilter[t] = true
			}
		}
		for _, n := range nodes {
			if n.Enabled != 1 {
				continue
			}
			if len(tagFilter) > 0 {
				match := false
				for _, t := range splitTags(n.Tags) {
					if tagFilter[t] {
						match = true
						break
					}
				}
				if !match {
					continue
				}
			}
			selected = append(selected, n)
		}
	} else {
		seen := map[int64]bool{}
		for _, id := range cfg.NodeIDs {
			n, ok := byID[id]
			if !ok || n.Enabled != 1 || seen[id] {
				continue
			}
			seen[id] = true
			selected = append(selected, n)
		}
	}
	proxies := make([]proxykit.Proxy, 0, len(selected))
	for _, n := range selected {
		p, err := proxyFromNode(n)
		if err != nil {
			continue
		}
		proxies = append(proxies, p)
	}
	return proxies, nil
}

func (h *Handler) renderGeneratedSubscription(ctx context.Context, cfg proxykit.GeneratorConfig) (proxykit.RenderResult, error) {
	proxies, err := h.resolveGeneratorNodes(ctx, cfg)
	if err != nil {
		return proxykit.RenderResult{}, err
	}
	if len(proxies) == 0 {
		return proxykit.RenderResult{}, fmt.Errorf("没有可用的节点（节点未启用或未选择）")
	}
	return proxykit.Render(cfg, proxies)
}

func (h *Handler) subscriptionPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GenConfig proxykit.GeneratorConfig `json:"genConfig"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	proxies, err := h.resolveGeneratorNodes(r.Context(), req.GenConfig)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if len(proxies) == 0 {
		response.WriteJSON(w, response.ErrDefault("没有可用的节点（节点未启用或未选择）"))
		return
	}
	res, err := proxykit.Render(req.GenConfig, proxies)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"content": res.Content, "warnings": res.Warnings, "nodeCount": len(proxies)}))
}

func (h *Handler) subscriptionTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"templates": proxykit.Templates(), "defaultBaseConfig": proxykit.DefaultBaseConfig}))
}

func (h *Handler) subscriptionUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	users, err := h.repo.ListUserBriefs()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(users))
}

// ─── 订阅下载 ─────────────────────────────────────────────────────────
// Users download the subscription file from inside the panel with their
// login token; there is deliberately no public subscription URL.

func (h *Handler) subscriptionMyList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	claims, ok := requestClaims(r)
	if !ok {
		response.WriteJSON(w, response.Err(401, "未登录或token已过期"))
		return
	}
	var items []model.SubFile
	var err error
	if claims.RoleID == 0 {
		items, err = h.repo.ListSubFiles()
	} else {
		userID, _ := strconv.ParseInt(claims.Sub, 10, 64)
		items, err = h.repo.ListSubFilesForUser(userID)
	}
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	views := make([]subFileView, 0, len(items))
	for _, s := range items {
		v := subFileToView(s, nil, false)
		if claims.RoleID != 0 {
			v.SourceURL = ""
			v.UserAgent = ""
		}
		views = append(views, v)
	}
	response.WriteJSON(w, response.OK(views))
}

func (h *Handler) subscriptionMyDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     int64  `json:"id"`
		Format string `json:"format"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	claims, ok := requestClaims(r)
	if !ok {
		response.WriteJSON(w, response.Err(401, "未登录或token已过期"))
		return
	}
	s, err := h.repo.GetSubFile(req.ID)
	if err != nil || s == nil {
		response.WriteJSON(w, response.ErrDefault("订阅不存在"))
		return
	}
	if claims.RoleID != 0 {
		userID, _ := strconv.ParseInt(claims.Sub, 10, 64)
		allowed, err := h.repo.UserHasSubFile(userID, s.ID)
		if err != nil || !allowed || s.Enabled != 1 {
			response.WriteJSON(w, response.Err(403, "无权下载该订阅"))
			return
		}
		if s.ExpireTime > 0 && time.Now().UnixMilli() > s.ExpireTime {
			response.WriteJSON(w, response.Err(403, "该订阅已过期"))
			return
		}
	}

	content, proxies, err := h.subscriptionContent(r.Context(), s)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	switch format {
	case "", "clash":
		format = "clash"
	case "base64", "uri":
		if proxies == nil {
			proxies, _ = proxykit.ParseClashProxies([]byte(content))
		}
		links := proxykit.ShareLinks(proxies)
		if links == "" {
			response.WriteJSON(w, response.ErrDefault("该订阅中没有可导出为分享链接的节点"))
			return
		}
		if format == "base64" {
			content = base64.StdEncoding.EncodeToString([]byte(links + "\n"))
		} else {
			content = links + "\n"
		}
	default:
		response.WriteJSON(w, response.ErrDefault("不支持的下载格式"))
		return
	}
	mime := "text/plain; charset=utf-8"
	if format == "clash" {
		mime = "application/x-yaml; charset=utf-8"
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{
		"fileName": subscriptionFileName(*s, format),
		"content":  content,
		"mime":     mime,
	}))
}

// subscriptionContent returns the up-to-date content of a subscription. For
// generated subscriptions it also returns the node list it was built from.
func (h *Handler) subscriptionContent(ctx context.Context, s *model.SubFile) (string, []proxykit.Proxy, error) {
	switch s.Type {
	case subTypeGenerate:
		cfg, err := decodeGenConfig(s.GenConfig)
		if err != nil {
			return "", nil, err
		}
		proxies, err := h.resolveGeneratorNodes(ctx, cfg)
		if err != nil {
			return "", nil, err
		}
		if len(proxies) == 0 {
			return "", nil, fmt.Errorf("订阅中没有可用的节点")
		}
		res, err := proxykit.Render(cfg, proxies)
		if err != nil {
			return "", nil, err
		}
		if res.Content != s.Content {
			_ = h.repo.UpdateSubFileCache(s.ID, res.Content)
		}
		return res.Content, proxykit.UniqueNames(proxies), nil
	case subTypeImport:
		stale := s.AutoUpdate > 0 && time.Since(time.UnixMilli(s.LastSyncTime)) > time.Duration(s.AutoUpdate)*time.Hour
		if stale || strings.TrimSpace(s.Content) == "" {
			fetchCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()
			if err := h.fetchImportedSubscription(fetchCtx, s); err != nil {
				log.Printf("subscription %d auto update failed: %v", s.ID, err)
				if strings.TrimSpace(s.Content) == "" {
					return "", nil, err
				}
			} else {
				_ = h.repo.UpdateSubFileSync(s.ID, s.Content, s.SubInfo, "", s.LastSyncTime)
			}
		}
		return s.Content, nil, nil
	default:
		if strings.TrimSpace(s.Content) == "" {
			return "", nil, fmt.Errorf("订阅内容为空")
		}
		return s.Content, nil, nil
	}
}
