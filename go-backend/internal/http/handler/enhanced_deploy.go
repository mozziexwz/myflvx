package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/http/response"
	"go-backend/internal/proxykit"
	"go-backend/internal/store/model"
)

// 部署节点（仅管理员）：面板通过 FLVX 节点端执行 mihomo_script，一键部署
// Shadowsocks 2022 / mieru / Sudoku。一条部署记录对应一个 FLVX 节点；面板下发
// 命令后在后台跟踪节点上的任务，完成后读取分享链接生成代理节点。

const (
	deployStatusPending     = "pending"
	deployStatusRunning     = "running"
	deployStatusSuccess     = "success"
	deployStatusFailed      = "failed"
	deployStatusUninstalled = "uninstalled"

	deployScriptURLConfigKey   = "deploy_mihomo_script_url"
	deployScriptProxyConfigKey = "deploy_mihomo_script_use_proxy"

	deployCommandType     = "MihomoScript"
	deployLogLimit        = 256 << 10
	deployTaskTimeout     = 25 * time.Minute
	deployPollInterval    = 2 * time.Second
	deployPollTimeout     = 15 * time.Second
	deployStartTimeout    = 30 * time.Second
	deployMaxPollFailures = 90 // about three minutes without an answer from the node
)

var reANSIEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

type deployView struct {
	ID          int64                 `json:"id"`
	Name        string                `json:"name"`
	NodeID      int64                 `json:"nodeId"`
	NodeName    string                `json:"nodeName,omitempty"`
	NodeOnline  bool                  `json:"nodeOnline"`
	NodeVersion string                `json:"nodeVersion,omitempty"`
	Host        string                `json:"host"`
	Protocols   []string              `json:"protocols"`
	Params      proxykit.DeployParams `json:"params"`
	Status      string                `json:"status"`
	TaskOp      string                `json:"taskOp,omitempty"`
	LastLog     string                `json:"lastLog,omitempty"`
	LastRunTime int64                 `json:"lastRunTime"`
	Remark      string                `json:"remark"`
	CreatedTime int64                 `json:"createdTime"`
	UpdatedTime int64                 `json:"updatedTime"`
	ProxyNodes  int                   `json:"proxyNodes"`
}

// deployTaskResult mirrors the agent's MihomoScript response data.
type deployTaskResult struct {
	TaskID     string `json:"taskId"`
	Op         string `json:"op"`
	State      string `json:"state"`
	ExitCode   int    `json:"exitCode"`
	Log        string `json:"log"`
	NextOffset int    `json:"nextOffset"`
	Truncated  bool   `json:"truncated"`
	Share      string `json:"share"`
	Error      string `json:"error"`
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

func deployOpLabel(op string) string {
	switch op {
	case proxykit.ScriptOpInstall:
		return "部署"
	case proxykit.ScriptOpInfo:
		return "刷新状态"
	case proxykit.ScriptOpUninstall:
		return "卸载"
	default:
		return op
	}
}

// deployScriptURL returns the mihomo_script URL the node agent downloads.
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

// deployNodeHost returns the address clients use for a FLVX node.
func deployNodeHost(node *model.Node) string {
	host := strings.TrimSpace(node.ServerIP)
	if host == "" && node.ServerIPV4.Valid {
		host = strings.TrimSpace(node.ServerIPV4.String)
	}
	if host == "" && node.ServerIPV6.Valid {
		host = strings.TrimSpace(node.ServerIPV6.String)
	}
	return strings.Trim(host, "[]")
}

func (h *Handler) deployToView(d model.ProxyDeploy, withLog bool, nodes map[int64]*model.Node) deployView {
	params, _ := decodeDeployParams(d.Params)
	status := d.Status
	_, following := h.deployTasks.Load(d.ID)
	if status == deployStatusRunning && !following {
		status = deployStatusFailed
	}
	v := deployView{
		ID: d.ID, Name: d.Name, NodeID: d.NodeID, Host: d.Host, Protocols: params.Protocols, Params: params,
		Status: status, LastRunTime: d.LastRunTime, Remark: d.Remark, CreatedTime: d.CreatedTime, UpdatedTime: d.UpdatedTime,
	}
	if status == deployStatusRunning {
		v.TaskOp = d.TaskOp
	}
	if withLog {
		v.LastLog = d.LastLog
		if status != d.Status {
			v.LastLog += "\n[面板] 任务跟踪已中断，可以点「刷新状态」查看服务器上的实际情况。"
		}
	}
	if d.NodeID > 0 {
		node, ok := nodes[d.NodeID]
		if !ok {
			node, _ = h.repo.GetNodeByID(d.NodeID)
			if nodes != nil {
				nodes[d.NodeID] = node
			}
		}
		if node != nil {
			v.NodeName = node.Name
			v.NodeOnline = node.Status == 1
			if node.Version.Valid {
				v.NodeVersion = node.Version.String
			}
		}
	}
	if items, err := h.repo.ListProxyNodesBySource(proxySourceDeploy, deploySourceRef(d.ID)); err == nil {
		v.ProxyNodes = len(items)
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
	nodes := map[int64]*model.Node{}
	views := make([]deployView, 0, len(items))
	for _, d := range items {
		views = append(views, h.deployToView(d, false, nodes))
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
	response.WriteJSON(w, response.OK(h.deployToView(*d, true, nil)))
}

func (h *Handler) deployDefaults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	params := proxykit.DeployParams{
		Protocols:    []string{proxykit.DeployProtoSS, proxykit.DeployProtoMieru, proxykit.DeployProtoSudoku},
		IPVersion:    "ipv4-prefer",
		BlockPrivate: true,
	}
	if err := proxykit.FillDeployDefaults(&params); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"params": params, "scriptUrl": h.deployScriptURL()}))
}

// deployRecordName names the deployment (and the prefix of its proxy nodes).
func deployRecordName(prefix string, node *model.Node, multiple bool) string {
	prefix = strings.TrimSpace(prefix)
	switch {
	case prefix == "":
		return node.Name
	case multiple:
		return prefix + "-" + node.Name
	default:
		return prefix
	}
}

// buildDeployRecords prepares one deployment per FLVX node, all sharing the
// given parameters except the client address (each node's own address unless
// host overrides it for a single node).
func (h *Handler) buildDeployRecords(userID int64, nodeIDs []int64, prefix, host string, base proxykit.DeployParams, remark string) ([]*model.ProxyDeploy, error) {
	seen := map[int64]bool{}
	nodes := []*model.Node{}
	for _, id := range nodeIDs {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		node, err := h.repo.GetNodeByID(id)
		if err != nil || node == nil {
			return nil, fmt.Errorf("FLVX 节点 %d 不存在", id)
		}
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		return nil, errors.New("请选择要部署的 FLVX 节点")
	}
	if err := proxykit.FillDeployDefaults(&base); err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	records := make([]*model.ProxyDeploy, 0, len(nodes))
	for _, node := range nodes {
		params := base
		params.Protocols = append([]string(nil), base.Protocols...)
		params.Host = deployNodeHost(node)
		if len(nodes) == 1 && strings.TrimSpace(host) != "" {
			params.Host = strings.Trim(strings.TrimSpace(host), "[]")
		}
		if params.HasProtocol(proxykit.DeployProtoSudoku) && params.SudokuKeyMode == "pair" {
			// Every server generates its own key pair; the client key is read back after install.
			params.SudokuKey = ""
			params.SudokuServerKey = ""
		}
		name := deployRecordName(prefix, node, len(nodes) > 1)
		params.ScriptName = proxykit.ScriptSafeName(name)
		if err := proxykit.ValidateDeployParams(params); err != nil {
			return nil, fmt.Errorf("节点「%s」: %v", node.Name, err)
		}
		raw, _ := json.Marshal(params)
		records = append(records, &model.ProxyDeploy{
			UserID: userID, Name: name, NodeID: node.ID, Host: params.Host, Protocols: strings.Join(params.Protocols, ","),
			Params: string(raw), Status: deployStatusPending, Remark: strings.TrimSpace(remark),
			CreatedTime: now, UpdatedTime: now,
		})
	}
	return records, nil
}

// createDeployRecords stores the records and optionally starts the install on each.
func (h *Handler) createDeployRecords(records []*model.ProxyDeploy, run bool, claims *auth.Claims) ([]deployView, []string, error) {
	for _, d := range records {
		if err := h.repo.CreateProxyDeploy(d); err != nil {
			return nil, nil, err
		}
	}
	warnings := []string{}
	if run {
		for _, d := range records {
			if err := h.startDeployTask(*d, proxykit.ScriptOpInstall, claims); err != nil {
				warnings = append(warnings, fmt.Sprintf("「%s」未能开始部署: %v", d.Name, err))
			}
		}
	}
	nodes := map[int64]*model.Node{}
	views := make([]deployView, 0, len(records))
	for _, d := range records {
		if current, err := h.repo.GetProxyDeploy(d.ID); err == nil && current != nil {
			views = append(views, h.deployToView(*current, false, nodes))
		}
	}
	return views, warnings, nil
}

func claimsPointer(r *http.Request) *auth.Claims {
	if claims, ok := requestClaims(r); ok {
		return &claims
	}
	return nil
}

func (h *Handler) deployCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeIDs []int64               `json:"nodeIds"`
		Name    string                `json:"name"`
		Host    string                `json:"host"`
		Params  proxykit.DeployParams `json:"params"`
		Remark  string                `json:"remark"`
		Run     bool                  `json:"run"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	userID, ok := callerID(w, r)
	if !ok {
		return
	}
	records, err := h.buildDeployRecords(userID, req.NodeIDs, req.Name, req.Host, req.Params, req.Remark)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	views, warnings, err := h.createDeployRecords(records, req.Run, claimsPointer(r))
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"items": views, "warnings": warnings}))
}

func (h *Handler) deployCopy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      int64   `json:"id"`
		NodeIDs []int64 `json:"nodeIds"`
		Run     bool    `json:"run"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	userID, ok := callerID(w, r)
	if !ok {
		return
	}
	source, err := h.repo.GetProxyDeploy(req.ID)
	if err != nil || source == nil {
		response.WriteJSON(w, response.ErrDefault("部署记录不存在"))
		return
	}
	params, err := decodeDeployParams(source.Params)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	targets := []int64{}
	for _, id := range req.NodeIDs {
		if id != source.NodeID {
			targets = append(targets, id)
		}
	}
	// Each copy is named after its FLVX node and keeps the ports and secrets.
	records, err := h.buildDeployRecords(userID, targets, "", "", params, source.Remark)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	views, warnings, err := h.createDeployRecords(records, req.Run, claimsPointer(r))
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"items": views, "warnings": warnings}))
}

func (h *Handler) deployUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     int64                 `json:"id"`
		Name   string                `json:"name"`
		Host   string                `json:"host"`
		Params proxykit.DeployParams `json:"params"`
		Remark string                `json:"remark"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	d, err := h.repo.GetProxyDeploy(req.ID)
	if err != nil || d == nil {
		response.WriteJSON(w, response.ErrDefault("部署记录不存在"))
		return
	}
	if _, running := h.deployTasks.Load(d.ID); running {
		response.WriteJSON(w, response.ErrDefault("正在执行任务，请稍后再修改"))
		return
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		d.Name = name
	}
	params := req.Params
	params.Host = strings.Trim(strings.TrimSpace(req.Host), "[]")
	if params.Host == "" {
		params.Host = d.Host
	}
	if err := proxykit.FillDeployDefaults(&params); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	params.ScriptName = proxykit.ScriptSafeName(d.Name)
	if err := proxykit.ValidateDeployParams(params); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	raw, _ := json.Marshal(params)
	d.Host = params.Host
	d.Protocols = strings.Join(params.Protocols, ",")
	d.Params = string(raw)
	d.Remark = strings.TrimSpace(req.Remark)
	d.UpdatedTime = time.Now().UnixMilli()
	if err := h.repo.SaveProxyDeploy(d); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(h.deployToView(*d, false, nil)))
}

func (h *Handler) deployGeneratedNodeIDs(id int64) []int64 {
	ids := []int64{}
	if items, err := h.repo.ListProxyNodesBySource(proxySourceDeploy, deploySourceRef(id)); err == nil {
		for _, n := range items {
			ids = append(ids, n.ID)
			if children, err := h.repo.ListProxyNodeChildren(n.ID); err == nil {
				for _, c := range children {
					ids = append(ids, c.ID)
				}
			}
		}
	}
	return ids
}

func (h *Handler) deployDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID          int64 `json:"id"`
		DeleteNodes bool  `json:"deleteNodes"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	if _, running := h.deployTasks.Load(req.ID); running {
		response.WriteJSON(w, response.ErrDefault("正在执行任务，无法删除"))
		return
	}
	if req.DeleteNodes {
		if err := h.repo.DeleteProxyNodes(h.deployGeneratedNodeIDs(req.ID)); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}
	if err := h.repo.DeleteProxyDeploy(req.ID); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) deployStartOp(w http.ResponseWriter, r *http.Request, ids []int64, op string) {
	claims := claimsPointer(r)
	started := 0
	warnings := []string{}
	for _, id := range ids {
		d, err := h.repo.GetProxyDeploy(id)
		if err != nil || d == nil {
			warnings = append(warnings, fmt.Sprintf("部署记录 %d 不存在", id))
			continue
		}
		if err := h.startDeployTask(*d, op, claims); err != nil {
			warnings = append(warnings, fmt.Sprintf("「%s」: %v", d.Name, err))
			continue
		}
		started++
	}
	if started == 0 && len(warnings) > 0 {
		response.WriteJSON(w, response.ErrDefault(strings.Join(warnings, "；")))
		return
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"started": started, "warnings": warnings}))
}

func (h *Handler) deployRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID  int64   `json:"id"`
		IDs []int64 `json:"ids"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	ids := req.IDs
	if req.ID > 0 {
		ids = append(ids, req.ID)
	}
	if len(ids) == 0 {
		response.WriteJSON(w, response.ErrDefault("请选择部署记录"))
		return
	}
	h.deployStartOp(w, r, ids, proxykit.ScriptOpInstall)
}

func (h *Handler) deployRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	h.deployStartOp(w, r, []int64{req.ID}, proxykit.ScriptOpInfo)
}

func (h *Handler) deployUninstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if !decodeEnhancedBody(w, r, &req) {
		return
	}
	h.deployStartOp(w, r, []int64{req.ID}, proxykit.ScriptOpUninstall)
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
	}))
}

// ─── 节点端任务 ───────────────────────────────────────────────────────

// friendlyDeployError explains common agent errors.
func friendlyDeployError(err error) error {
	msg := strings.TrimSpace(err.Error())
	switch {
	case strings.Contains(msg, "未知命令类型"):
		return errors.New("节点端版本过旧，不支持面板部署：请先在「节点」页面把节点端升级到 myflvx 2.2.2 或更高版本")
	case strings.Contains(msg, "节点不在线"):
		return errors.New("节点不在线")
	default:
		return errors.New(msg)
	}
}

func decodeDeployTaskResult(data map[string]interface{}) deployTaskResult {
	var out deployTaskResult
	if raw, err := json.Marshal(data); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// deployLog buffers task output, keeping the tail when it grows too large.
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
		kept := append([]byte("...（日志过长，已省略前面的内容）\n"), tail...)
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

// startDeployTask asks the node agent to run a mihomo_script operation and
// follows it in the background.
func (h *Handler) startDeployTask(d model.ProxyDeploy, op string, claims *auth.Claims) error {
	if d.NodeID <= 0 {
		return errors.New("该部署没有关联 FLVX 节点")
	}
	params, err := decodeDeployParams(d.Params)
	if err != nil {
		return err
	}
	if op == proxykit.ScriptOpInstall {
		if err := proxykit.ValidateDeployParams(params); err != nil {
			return err
		}
	}
	if _, loaded := h.deployTasks.LoadOrStore(d.ID, ""); loaded {
		return errors.New("正在执行任务，请等待完成")
	}
	payload := map[string]interface{}{
		"action":    "start",
		"op":        op,
		"scriptUrl": h.deployScriptURL(),
		"args":      proxykit.ScriptOpArgs(op, params),
	}
	res, err := h.sendNodeCommandWithTimeout(d.NodeID, deployCommandType, payload, deployStartTimeout, false, false)
	if err != nil {
		h.deployTasks.Delete(d.ID)
		return friendlyDeployError(err)
	}
	task := decodeDeployTaskResult(res.Data)
	if task.TaskID == "" {
		h.deployTasks.Delete(d.ID)
		return errors.New("节点端没有返回任务编号")
	}
	h.deployTasks.Store(d.ID, task.TaskID)

	nodeName := fmt.Sprintf("#%d", d.NodeID)
	if node, err := h.repo.GetNodeByID(d.NodeID); err == nil && node != nil {
		nodeName = node.Name
	}
	now := time.Now().UnixMilli()
	header := fmt.Sprintf("[面板] %s：已下发到 FLVX 节点「%s」（任务 %s）\n", deployOpLabel(op), nodeName, task.TaskID)
	_ = h.repo.UpdateProxyDeployRun(d.ID, deployStatusRunning, header, now, now)
	_ = h.repo.SetProxyDeployTask(d.ID, task.TaskID, op, now)
	go h.followDeployTask(d.ID, d.NodeID, task.TaskID, op, header, claims)
	return nil
}

// resumeDeployTasks keeps following tasks that were running when the panel stopped.
func (h *Handler) resumeDeployTasks() {
	items, err := h.repo.ListRunningProxyDeploys()
	if err != nil {
		return
	}
	now := time.Now().UnixMilli()
	for _, d := range items {
		if d.TaskID == "" || d.NodeID <= 0 {
			_ = h.repo.UpdateProxyDeployRun(d.ID, deployStatusFailed, d.LastLog+"\n[面板] 面板重启时任务尚未开始，请重新执行。\n", 0, now)
			continue
		}
		if _, loaded := h.deployTasks.LoadOrStore(d.ID, d.TaskID); loaded {
			continue
		}
		header := fmt.Sprintf("[面板] 面板已重启，继续跟踪节点上的%s任务（%s）\n", deployOpLabel(d.TaskOp), d.TaskID)
		go h.followDeployTask(d.ID, d.NodeID, d.TaskID, d.TaskOp, header, nil)
	}
}

func (h *Handler) followDeployTask(id, nodeID int64, taskID, op, header string, claims *auth.Claims) {
	defer h.deployTasks.Delete(id)
	out := &deployLog{}
	out.Write([]byte(header))
	deadline := time.Now().Add(deployTaskTimeout)
	offset := 0
	failures := 0
	saved := ""
	fail := func(msg string) {
		fmt.Fprintf(out, "\n[面板] %s\n", msg)
		now := time.Now().UnixMilli()
		_ = h.repo.UpdateProxyDeployRun(id, deployStatusFailed, out.String(), 0, now)
		_ = h.repo.SetProxyDeployTask(id, "", "", now)
	}
	for {
		if time.Now().After(deadline) {
			fail(fmt.Sprintf("等待节点执行超过 %d 分钟，停止跟踪。", int(deployTaskTimeout/time.Minute)))
			return
		}
		time.Sleep(deployPollInterval)
		res, err := h.sendNodeCommandWithTimeout(nodeID, deployCommandType, map[string]interface{}{
			"action": "poll", "taskId": taskID, "offset": offset,
		}, deployPollTimeout, false, false)
		if err != nil {
			if strings.Contains(err.Error(), "任务不存在") {
				fail("节点端已重启，任务丢失。可以点「刷新状态」查看服务器上的实际情况。")
				return
			}
			failures++
			if failures == 1 {
				fmt.Fprintf(out, "[面板] 暂时无法连接节点（%v），继续等待…\n", friendlyDeployError(err))
			}
			if failures >= deployMaxPollFailures {
				fail("节点长时间没有响应，停止跟踪。可以稍后点「刷新状态」查看结果。")
				return
			}
			continue
		}
		failures = 0
		task := decodeDeployTaskResult(res.Data)
		if task.Truncated {
			out.Write([]byte("...（节点端日志过长，已省略前面的内容）\n"))
		}
		out.Write([]byte(reANSIEscape.ReplaceAllString(task.Log, "")))
		offset = task.NextOffset
		if task.State == "running" || task.State == "" {
			if current := out.String(); current != saved {
				saved = current
				_ = h.repo.UpdateProxyDeployRun(id, deployStatusRunning, current, 0, time.Now().UnixMilli())
			}
			continue
		}
		h.finishDeployTask(id, op, task, out, claims)
		return
	}
}

func (h *Handler) finishDeployTask(id int64, op string, task deployTaskResult, out *deployLog, claims *auth.Claims) {
	now := time.Now().UnixMilli()
	defer func() { _ = h.repo.SetProxyDeployTask(id, "", "", time.Now().UnixMilli()) }()
	if task.State != "success" {
		reason := strings.TrimSpace(task.Error)
		if reason == "" {
			reason = fmt.Sprintf("退出码 %d", task.ExitCode)
		}
		fmt.Fprintf(out, "\n[面板] %s失败：%s\n", deployOpLabel(op), reason)
		_ = h.repo.UpdateProxyDeployRun(id, deployStatusFailed, out.String(), 0, now)
		return
	}
	d, err := h.repo.GetProxyDeploy(id)
	if err != nil || d == nil {
		return
	}
	status := deployStatusSuccess
	switch op {
	case proxykit.ScriptOpUninstall:
		status = deployStatusUninstalled
		if ids := h.deployGeneratedNodeIDs(id); len(ids) > 0 {
			_ = h.repo.SetProxyNodesEnabled(ids, 0, now)
			fmt.Fprintf(out, "[面板] 已卸载，%d 个对应的代理节点已停用\n", len(ids))
		}
	default:
		params, err := decodeDeployParams(d.Params)
		if err != nil {
			fmt.Fprintf(out, "[面板] 部署参数损坏: %v\n", err)
			status = deployStatusFailed
			break
		}
		links := proxykit.ExtractShareLinks(task.Share)
		found := proxykit.ApplyShareLinks(&params, links)
		fmt.Fprintf(out, "[面板] 读取到 %d 条分享链接（%s）\n", len(links), strings.Join(found, ", "))
		raw, _ := json.Marshal(params)
		d.Params = string(raw)
		d.UpdatedTime = now
		if err := h.repo.SaveProxyDeploy(d); err != nil {
			log.Printf("deploy %d save params failed: %v", id, err)
		}
		if claims == nil {
			claims = h.deployOwnerClaims(d.UserID)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		count, warnings, err := h.syncDeployNodes(ctx, claims, *d, params)
		cancel()
		if err != nil {
			fmt.Fprintf(out, "[面板] 生成代理节点失败: %v\n", err)
		} else {
			fmt.Fprintf(out, "[面板] 已生成 / 更新 %d 个代理节点\n", count)
		}
		for _, warning := range warnings {
			fmt.Fprintf(out, "[面板] 提示: %s\n", warning)
		}
	}
	fmt.Fprintf(out, "[面板] %s完成\n", deployOpLabel(op))
	_ = h.repo.UpdateProxyDeployRun(id, status, out.String(), 0, time.Now().UnixMilli())
}

// deployOwnerClaims builds the identity used to update relay forwards when a
// task finishes after a panel restart.
func (h *Handler) deployOwnerClaims(userID int64) *auth.Claims {
	if userID <= 0 {
		return nil
	}
	user, err := h.repo.GetUserByID(userID)
	if err != nil || user == nil {
		return nil
	}
	return &auth.Claims{Sub: strconv.FormatInt(user.ID, 10), User: user.User, Name: user.User, RoleID: user.RoleID}
}

// syncDeployNodes creates or updates the proxy nodes of a deployment (one per
// protocol, owned by the deployment's creator) and keeps relay nodes /
// forwards built on them in sync.
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
		warnings = append(warnings, "还没有读取到 Sudoku 的客户端密钥（服务器生成的密钥对），请点「刷新状态」")
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
			updated.Enabled = 1
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
			UserID: d.UserID, Name: proxykit.Name(proxy), Tags: joinTags([]string{"部署"}), Remark: d.Remark, Source: proxySourceDeploy,
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
	count, warnings, err := h.syncDeployNodes(r.Context(), claimsPointer(r), *d, params)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"nodes": count, "warnings": warnings}))
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
		if url != "" && !strings.HasPrefix(url, "https://") {
			response.WriteJSON(w, response.ErrDefault("脚本地址必须以 https:// 开头（节点端只下载 https 地址）"))
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
		"scriptUrl":        rawURL,
		"defaultScriptUrl": proxykit.DefaultMihomoScriptURL,
		"useProxy":         useProxy,
		"effectiveUrl":     h.deployScriptURL(),
	}))
}
