package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/http/response"
	"go-backend/internal/proxykit"
	"go-backend/internal/store/model"
)

// 部署节点：用 mihomo_script 一键部署 Shadowsocks 2022 / mieru / Sudoku。

const (
	deployStatusPending = "pending"
	deployStatusRunning = "running"
	deployStatusSuccess = "success"
	deployStatusFailed  = "failed"

	deployScriptURLConfigKey   = "deploy_mihomo_script_url"
	deployScriptProxyConfigKey = "deploy_mihomo_script_use_proxy"

	deployLogLimit = 256 << 10
	deployTimeout  = 20 * time.Minute
)

// deployRunning tracks deployments with an SSH run in progress.
var deployRunning sync.Map

type deployView struct {
	ID          int64                 `json:"id"`
	Name        string                `json:"name"`
	NodeID      int64                 `json:"nodeId"`
	NodeName    string                `json:"nodeName,omitempty"`
	Host        string                `json:"host"`
	Protocols   []string              `json:"protocols"`
	Params      proxykit.DeployParams `json:"params"`
	Status      string                `json:"status"`
	LastLog     string                `json:"lastLog,omitempty"`
	LastRunTime int64                 `json:"lastRunTime"`
	Remark      string                `json:"remark"`
	CreatedTime int64                 `json:"createdTime"`
	UpdatedTime int64                 `json:"updatedTime"`
	Command     string                `json:"command"`
	ProxyNodes  int                   `json:"proxyNodes"`
}

func deploySourceRef(id int64) string { return fmt.Sprintf("deploy:%d", id) }

func decodeDeployParams(raw string) (proxykit.DeployParams, error) {
	var p proxykit.DeployParams
	if strings.TrimSpace(raw) == "" {
		return p, fmt.Errorf("缺少部署参数")
	}
	err := json.Unmarshal([]byte(raw), &p)
	return p, err
}

func (h *Handler) deployScriptURL() string {
	url := ""
	if cfg, err := h.repo.GetConfigByName(deployScriptURLConfigKey); err == nil && cfg != nil {
		url = strings.TrimSpace(cfg.Value)
	}
	if url == "" {
		url = proxykit.DefaultMihomoScriptURL
	}
	useProxy := false
	if cfg, err := h.repo.GetConfigByName(deployScriptProxyConfigKey); err == nil && cfg != nil {
		useProxy = cfg.Value == "true"
	}
	if useProxy {
		if _, proxyURL := h.getGithubProxyConfig(); proxyURL != "" &&
			(strings.HasPrefix(url, "https://raw.githubusercontent.com/") || strings.HasPrefix(url, "https://github.com/")) {
			return strings.TrimSuffix(proxyURL, "/") + "/" + url
		}
	}
	return url
}

func (h *Handler) deployToView(d model.ProxyDeploy, withLog bool) deployView {
	params, _ := decodeDeployParams(d.Params)
	status := d.Status
	if status == deployStatusRunning {
		if _, running := deployRunning.Load(d.ID); !running {
			status = deployStatusFailed
		}
	}
	v := deployView{
		ID: d.ID, Name: d.Name, NodeID: d.NodeID, Host: d.Host, Protocols: params.Protocols, Params: params,
		Status: status, LastRunTime: d.LastRunTime, Remark: d.Remark, CreatedTime: d.CreatedTime, UpdatedTime: d.UpdatedTime,
		Command: proxykit.BuildInstallCommand(params, h.deployScriptURL()),
	}
	if withLog {
		v.LastLog = d.LastLog
		if status != d.Status {
			v.LastLog += "\n[面板] 部署进程已中断（面板重启或超时），请重新部署。"
		}
	}
	if d.NodeID > 0 {
		if node, err := h.repo.GetNodeByID(d.NodeID); err == nil && node != nil {
			v.NodeName = node.Name
		}
	}
	if nodes, err := h.repo.ListProxyNodesBySource(proxySourceDeploy, deploySourceRef(d.ID)); err == nil {
		v.ProxyNodes = len(nodes)
	}
	return v
}

func (h *Handler) deployList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	items, err := h.repo.ListProxyDeploys()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	views := make([]deployView, 0, len(items))
	for _, d := range items {
		views = append(views, h.deployToView(d, false))
	}
	response.WriteJSON(w, response.OK(views))
}

func (h *Handler) deployGet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	d, err := h.repo.GetProxyDeploy(req.ID)
	if err != nil || d == nil {
		response.WriteJSON(w, response.ErrDefault("部署记录不存在"))
		return
	}
	response.WriteJSON(w, response.OK(h.deployToView(*d, true)))
}

func (h *Handler) deployDefaults(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host   string `json:"host"`
		NodeID int64  `json:"nodeId"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	host := strings.TrimSpace(req.Host)
	if host == "" && req.NodeID > 0 {
		if node, err := h.repo.GetNodeByID(req.NodeID); err == nil && node != nil {
			host = strings.TrimSpace(node.ServerIP)
		}
	}
	params := proxykit.DeployParams{
		Protocols:    []string{proxykit.DeployProtoSS, proxykit.DeployProtoMieru, proxykit.DeployProtoSudoku},
		Host:         host,
		IPVersion:    "ipv4-prefer",
		BlockPrivate: true,
	}
	if err := proxykit.FillDeployDefaults(&params); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"params": params, "scriptUrl": h.deployScriptURL()}))
}

type deployMutation struct {
	ID          int64                 `json:"id"`
	Name        string                `json:"name"`
	NodeID      int64                 `json:"nodeId"`
	Params      proxykit.DeployParams `json:"params"`
	Remark      string                `json:"remark"`
	DeleteNodes bool                  `json:"deleteNodes"`
}

func prepareDeployParams(name string, params proxykit.DeployParams) (proxykit.DeployParams, error) {
	if err := proxykit.FillDeployDefaults(&params); err != nil {
		return params, err
	}
	if params.ScriptName == "" {
		params.ScriptName = proxykit.ScriptSafeName(name)
	}
	if err := proxykit.ValidateDeployParams(params); err != nil {
		return params, err
	}
	return params, nil
}

func (h *Handler) deployCreate(w http.ResponseWriter, r *http.Request) {
	var req deployMutation
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		response.WriteJSON(w, response.ErrDefault("请填写名称（同时作为节点名前缀）"))
		return
	}
	params, err := prepareDeployParams(name, req.Params)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	raw, _ := json.Marshal(params)
	now := time.Now().UnixMilli()
	d := &model.ProxyDeploy{
		Name: name, NodeID: req.NodeID, Host: params.Host, Protocols: strings.Join(params.Protocols, ","),
		Params: string(raw), Status: deployStatusPending, Remark: strings.TrimSpace(req.Remark),
		CreatedTime: now, UpdatedTime: now,
	}
	if err := h.repo.CreateProxyDeploy(d); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(h.deployToView(*d, false)))
}

func (h *Handler) deployUpdate(w http.ResponseWriter, r *http.Request) {
	var req deployMutation
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	d, err := h.repo.GetProxyDeploy(req.ID)
	if err != nil || d == nil {
		response.WriteJSON(w, response.ErrDefault("部署记录不存在"))
		return
	}
	if _, running := deployRunning.Load(d.ID); running {
		response.WriteJSON(w, response.ErrDefault("部署正在进行中，请稍后再修改"))
		return
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		d.Name = name
	}
	params, err := prepareDeployParams(d.Name, req.Params)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	raw, _ := json.Marshal(params)
	d.NodeID = req.NodeID
	d.Host = params.Host
	d.Protocols = strings.Join(params.Protocols, ",")
	d.Params = string(raw)
	d.Remark = strings.TrimSpace(req.Remark)
	d.UpdatedTime = time.Now().UnixMilli()
	if err := h.repo.SaveProxyDeploy(d); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(h.deployToView(*d, false)))
}

func (h *Handler) deployDelete(w http.ResponseWriter, r *http.Request) {
	var req deployMutation
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	if _, running := deployRunning.Load(req.ID); running {
		response.WriteJSON(w, response.ErrDefault("部署正在进行中，无法删除"))
		return
	}
	if req.DeleteNodes {
		if nodes, err := h.repo.ListProxyNodesBySource(proxySourceDeploy, deploySourceRef(req.ID)); err == nil && len(nodes) > 0 {
			ids := make([]int64, 0, len(nodes))
			for _, n := range nodes {
				ids = append(ids, n.ID)
				if children, err := h.repo.ListProxyNodeChildren(n.ID); err == nil {
					for _, c := range children {
						ids = append(ids, c.ID)
					}
				}
			}
			if err := h.repo.DeleteProxyNodes(ids); err != nil {
				response.WriteJSON(w, response.Err(-2, err.Error()))
				return
			}
		}
	}
	if err := h.repo.DeleteProxyDeploy(req.ID); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) deployCommand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	d, err := h.repo.GetProxyDeploy(req.ID)
	if err != nil || d == nil {
		response.WriteJSON(w, response.ErrDefault("部署记录不存在"))
		return
	}
	params, err := decodeDeployParams(d.Params)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	scriptURL := h.deployScriptURL()
	response.WriteJSON(w, response.OK(map[string]interface{}{
		"command":   proxykit.BuildInstallCommand(params, scriptURL),
		"scriptUrl": scriptURL,
		"needsKeyReadback": params.HasProtocol(proxykit.DeployProtoSudoku) && params.SudokuKeyMode == "pair",
	}))
}

// syncDeployNodes creates or updates the proxy nodes of a deployment (one
// per protocol) and keeps relay nodes / forwards built on them in sync.
func (h *Handler) syncDeployNodes(ctx context.Context, claims *auth.Claims, d model.ProxyDeploy, params proxykit.DeployParams) (int, []string, error) {
	warnings := []string{}
	ref := deploySourceRef(d.ID)
	existing, err := h.repo.ListProxyNodesBySource(proxySourceDeploy, ref)
	if err != nil {
		return 0, nil, err
	}
	byProto := map[string]model.ProxyNode{}
	for _, n := range existing {
		byProto[n.Protocol] = n
	}
	clients := proxykit.ClientProxies(params, d.Name)
	if params.HasProtocol(proxykit.DeployProtoSudoku) && clients[proxykit.DeployProtoSudoku] == nil {
		warnings = append(warnings, "Sudoku 使用服务器生成的密钥对，还没有读取到客户端密钥：请使用 SSH 部署，或把部署输出中的 sudoku:// 链接粘贴回来")
	}
	now := time.Now().UnixMilli()
	toCreate := []*model.ProxyNode{}
	count := 0
	for _, proto := range params.Protocols {
		proxy := clients[proto]
		if proxy == nil {
			continue
		}
		mihomoType := proxykit.Type(proxy)
		if old, ok := byProto[mihomoType]; ok {
			before := old
			updated := old
			if err := applyProxyToNode(&updated, proxy); err != nil {
				return count, warnings, err
			}
			if link, err := proxykit.ShareLink(proxy); err == nil {
				updated.RawURL = link
			}
			updated.UpdatedTime = now
			if err := h.repo.SaveProxyNode(&updated); err != nil {
				return count, warnings, err
			}
			if claims != nil {
				warnings = append(warnings, h.syncRelayChildren(ctx, *claims, before, updated)...)
			}
			delete(byProto, mihomoType)
			count++
			continue
		}
		node := &model.ProxyNode{
			Name: proxykit.Name(proxy), Tags: joinTags([]string{"部署"}), Remark: d.Remark, Source: proxySourceDeploy,
			SourceRef: ref, Enabled: 1, CreatedTime: now, UpdatedTime: now,
		}
		if link, err := proxykit.ShareLink(proxy); err == nil {
			node.RawURL = link
		}
		if err := applyProxyToNode(node, proxy); err != nil {
			return count, warnings, err
		}
		toCreate = append(toCreate, node)
		count++
	}
	if err := h.repo.CreateProxyNodes(toCreate); err != nil {
		return count, warnings, err
	}
	// Nodes of protocols that were removed from the deployment are disabled
	// rather than deleted, so subscriptions and relays keep their references.
	stale := []int64{}
	for _, n := range byProto {
		stale = append(stale, n.ID)
	}
	if len(stale) > 0 {
		_ = h.repo.SetProxyNodesEnabled(stale, 0, now)
		warnings = append(warnings, fmt.Sprintf("%d 个已移除协议的节点已停用", len(stale)))
	}
	return count, warnings, nil
}

func (h *Handler) deploySyncNodes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	d, err := h.repo.GetProxyDeploy(req.ID)
	if err != nil || d == nil {
		response.WriteJSON(w, response.ErrDefault("部署记录不存在"))
		return
	}
	params, err := decodeDeployParams(d.Params)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	var claimsPtr *auth.Claims
	if claims, ok := requestClaims(r); ok {
		claimsPtr = &claims
	}
	count, warnings, err := h.syncDeployNodes(r.Context(), claimsPtr, *d, params)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if d.Status == deployStatusPending {
		_ = h.repo.UpdateProxyDeployRun(d.ID, deployStatusSuccess, d.LastLog, 0, time.Now().UnixMilli())
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"nodes": count, "warnings": warnings}))
}

func (h *Handler) deployApplyOutput(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     int64  `json:"id"`
		Output string `json:"output"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	d, err := h.repo.GetProxyDeploy(req.ID)
	if err != nil || d == nil {
		response.WriteJSON(w, response.ErrDefault("部署记录不存在"))
		return
	}
	params, err := decodeDeployParams(d.Params)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	links := proxykit.ExtractShareLinks(req.Output)
	found := proxykit.ApplyShareLinks(&params, links)
	if len(found) == 0 {
		response.WriteJSON(w, response.ErrDefault("没有在输出中找到 ss:// / mierus:// / sudoku:// 分享链接"))
		return
	}
	raw, _ := json.Marshal(params)
	d.Params = string(raw)
	d.Status = deployStatusSuccess
	d.UpdatedTime = time.Now().UnixMilli()
	if err := h.repo.SaveProxyDeploy(d); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	var claimsPtr *auth.Claims
	if claims, ok := requestClaims(r); ok {
		claimsPtr = &claims
	}
	count, warnings, err := h.syncDeployNodes(r.Context(), claimsPtr, *d, params)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"found": found, "nodes": count, "warnings": warnings}))
}

// deployLog buffers the SSH output and periodically persists it so the page
// can show progress while the deployment runs.
type deployLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *deployLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n, err := l.buf.Write(p)
	if l.buf.Len() > deployLogLimit {
		tail := l.buf.Bytes()[l.buf.Len()-deployLogLimit/2:]
		kept := append([]byte("...（日志过长，已截断前面的内容）\n"), tail...)
		l.buf.Reset()
		l.buf.Write(kept)
	}
	return n, err
}

func (l *deployLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func (h *Handler) deployRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID  int64              `json:"id"`
		SSH proxykit.SSHTarget `json:"ssh"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	d, err := h.repo.GetProxyDeploy(req.ID)
	if err != nil || d == nil {
		response.WriteJSON(w, response.ErrDefault("部署记录不存在"))
		return
	}
	params, err := decodeDeployParams(d.Params)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if err := proxykit.ValidateDeployParams(params); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if strings.TrimSpace(req.SSH.Host) == "" {
		req.SSH.Host = params.Host
	}
	if err := req.SSH.Validate(); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	claims, _ := requestClaims(r)
	if _, loaded := deployRunning.LoadOrStore(d.ID, true); loaded {
		response.WriteJSON(w, response.ErrDefault("该部署正在进行中"))
		return
	}
	start := time.Now().UnixMilli()
	_ = h.repo.UpdateProxyDeployRun(d.ID, deployStatusRunning, "[面板] 正在连接 "+req.SSH.Host+" ...\n", start, start)

	script := proxykit.BuildSSHScript(params, h.deployScriptURL(), req.SSH.UseSudo)
	go h.runDeployment(*d, params, req.SSH, script, claims)
	response.WriteJSON(w, response.OK(map[string]interface{}{"status": deployStatusRunning}))
}

func (h *Handler) runDeployment(d model.ProxyDeploy, params proxykit.DeployParams, target proxykit.SSHTarget, script string, claims auth.Claims) {
	defer deployRunning.Delete(d.ID)
	ctx, cancel := context.WithTimeout(context.Background(), deployTimeout)
	defer cancel()

	out := &deployLog{}
	fmt.Fprintf(out, "[面板] 连接 %s@%s:%d，执行 mihomo_script install（协议: %s）\n", target.Username, target.Host, target.Port, strings.Join(params.Protocols, ", "))

	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_ = h.repo.UpdateProxyDeployRun(d.ID, deployStatusRunning, out.String(), 0, time.Now().UnixMilli())
			}
		}
	}()

	runErr := proxykit.RunSSHScript(ctx, target, script, out)
	close(stop)

	now := time.Now().UnixMilli()
	if runErr != nil {
		fmt.Fprintf(out, "\n[面板] 部署失败: %v\n", runErr)
		_ = h.repo.UpdateProxyDeployRun(d.ID, deployStatusFailed, out.String(), 0, now)
		return
	}
	links := proxykit.ExtractShareLinks(out.String())
	found := proxykit.ApplyShareLinks(&params, links)
	fmt.Fprintf(out, "\n[面板] 部署完成，读取到 %d 条分享链接（%s）\n", len(links), strings.Join(found, ", "))

	raw, _ := json.Marshal(params)
	if current, err := h.repo.GetProxyDeploy(d.ID); err == nil && current != nil {
		current.Params = string(raw)
		current.Status = deployStatusSuccess
		current.UpdatedTime = now
		_ = h.repo.SaveProxyDeploy(current)
		d = *current
	}
	var claimsPtr *auth.Claims
	if claims.Sub != "" {
		claimsPtr = &claims
	}
	count, warnings, err := h.syncDeployNodes(ctx, claimsPtr, d, params)
	if err != nil {
		fmt.Fprintf(out, "[面板] 生成节点失败: %v\n", err)
		log.Printf("deploy %d node sync failed: %v", d.ID, err)
	} else {
		fmt.Fprintf(out, "[面板] 已生成/更新 %d 个节点\n", count)
	}
	for _, warning := range warnings {
		fmt.Fprintf(out, "[面板] 提示: %s\n", warning)
	}
	_ = h.repo.UpdateProxyDeployRun(d.ID, deployStatusSuccess, out.String(), 0, time.Now().UnixMilli())
}

func (h *Handler) deploySettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ScriptURL *string `json:"scriptUrl"`
		UseProxy  *bool   `json:"useProxy"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	now := time.Now().UnixMilli()
	if req.ScriptURL != nil {
		url := strings.TrimSpace(*req.ScriptURL)
		if url != "" && !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
			response.WriteJSON(w, response.ErrDefault("脚本地址必须以 http:// 或 https:// 开头"))
			return
		}
		if err := h.repo.UpsertConfig(deployScriptURLConfigKey, url, now); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}
	if req.UseProxy != nil {
		value := "false"
		if *req.UseProxy {
			value = "true"
		}
		if err := h.repo.UpsertConfig(deployScriptProxyConfigKey, value, now); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}
	rawURL := ""
	if cfg, err := h.repo.GetConfigByName(deployScriptURLConfigKey); err == nil && cfg != nil {
		rawURL = cfg.Value
	}
	useProxy := false
	if cfg, err := h.repo.GetConfigByName(deployScriptProxyConfigKey); err == nil && cfg != nil {
		useProxy = cfg.Value == "true"
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{
		"scriptUrl":       rawURL,
		"defaultScriptUrl": proxykit.DefaultMihomoScriptURL,
		"useProxy":        useProxy,
		"effectiveUrl":    h.deployScriptURL(),
	}))
}
