package socket

// MihomoScript runs mihomo_script (https://github.com/mozziexwz/mihomo_script)
// on this node for the panel's 部署节点 feature, so Shadowsocks 2022 / mieru /
// Sudoku can be deployed and managed from the panel without SSH.
//
// The command is asynchronous: "start" downloads the script and runs one
// operation in the background and returns a task id; "poll" returns the new
// log output and, once finished, the exit code and /etc/mihomo/share.txt.
// Only one task runs at a time. Arguments are passed to bash directly (no
// shell), and both the operation and every option are checked against an
// allow-list first.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	mihomoTaskTimeout     = 20 * time.Minute
	mihomoTaskRetention   = time.Hour
	mihomoLogLimit        = 256 << 10
	mihomoPollChunk       = 64 << 10
	mihomoScriptMaxSize   = 2 << 20
	mihomoShareMaxSize    = 64 << 10
	mihomoDownloadTimeout = 2 * time.Minute
)

const (
	mihomoStateRunning = "running"
	mihomoStateSuccess = "success"
	mihomoStateFailed  = "failed"
)

// Overridable in tests.
var (
	mihomoShareFile   = "/etc/mihomo/share.txt"
	mihomoFetchScript = fetchMihomoScript
	mihomoRunScript   = runMihomoScriptProcess
)

// mihomoOps lists the operations the panel may run, with the options each accepts.
var mihomoOps = map[string]map[string]bool{
	"install": {
		"-y": true, "--yes": true, "--reset": true, "--force": true,
		"--protocols": true, "--version": true, "--host": true, "--name": true,
		"--ip-version": true, "--block-private": true,
		"--ss-port": true, "--ss-cipher": true, "--ss-password": true,
		"--mieru-port": true, "--mieru-user": true, "--mieru-password": true, "--mieru-low-entropy": true,
		"--sudoku-port": true, "--sudoku-key": true, "--sudoku-table": true, "--sudoku-padding": true,
		"--sudoku-pure-downlink": true, "--sudoku-custom-tables": true,
	},
	"info":      {},
	"status":    {},
	"restart":   {},
	"uninstall": {"-y": true, "--yes": true},
}

// Options that take no value.
var mihomoFlagOnly = map[string]bool{"-y": true, "--yes": true, "--reset": true, "--force": true}

var reMihomoArgValue = regexp.MustCompile(`^[A-Za-z0-9_.,:/=+@%-]{1,512}$`)

type mihomoScriptRequest struct {
	Action    string   `json:"action"`
	Op        string   `json:"op"`
	ScriptURL string   `json:"scriptUrl"`
	Args      []string `json:"args"`
	TaskID    string   `json:"taskId"`
	Offset    int      `json:"offset"`
}

type mihomoScriptResult struct {
	TaskID     string `json:"taskId"`
	Op         string `json:"op"`
	State      string `json:"state"`
	ExitCode   int    `json:"exitCode"`
	Log        string `json:"log"`
	NextOffset int    `json:"nextOffset"`
	Truncated  bool   `json:"truncated,omitempty"`
	Share      string `json:"share,omitempty"`
	Error      string `json:"error,omitempty"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt,omitempty"`
}

// mihomoLog keeps the tail of the task output and tracks absolute offsets so
// the panel can poll incrementally even after the head was dropped.
type mihomoLog struct {
	mu   sync.Mutex
	base int
	buf  []byte
}

func (l *mihomoLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	if over := len(l.buf) - mihomoLogLimit; over > 0 {
		l.buf = append([]byte(nil), l.buf[over:]...)
		l.base += over
	}
	return len(p), nil
}

// read returns up to mihomoPollChunk bytes from offset, the next offset and
// whether output before offset was dropped.
func (l *mihomoLog) read(offset int) (string, int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	truncated := false
	if offset < l.base {
		offset = l.base
		truncated = true
	}
	start := offset - l.base
	if start > len(l.buf) {
		start = len(l.buf)
	}
	end := len(l.buf)
	if end-start > mihomoPollChunk {
		end = start + mihomoPollChunk
	}
	return string(l.buf[start:end]), l.base + end, truncated
}

type mihomoTask struct {
	id        string
	op        string
	log       mihomoLog
	startedAt time.Time

	mu         sync.Mutex
	state      string
	exitCode   int
	share      string
	errMsg     string
	finishedAt time.Time
}

func (t *mihomoTask) finish(state string, exitCode int, share, errMsg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = state
	t.exitCode = exitCode
	t.share = share
	t.errMsg = errMsg
	t.finishedAt = time.Now()
}

func (t *mihomoTask) result(offset int) mihomoScriptResult {
	text, next, truncated := t.log.read(offset)
	t.mu.Lock()
	defer t.mu.Unlock()
	res := mihomoScriptResult{
		TaskID: t.id, Op: t.op, State: t.state, ExitCode: t.exitCode, Log: text, NextOffset: next,
		Truncated: truncated, StartedAt: t.startedAt.UnixMilli(),
	}
	if t.state != mihomoStateRunning {
		// Hand out the result only once the whole log has been read, so the
		// panel never finalises with output still pending.
		if logged, _, _ := t.log.read(next); logged == "" {
			res.Share = t.share
			res.Error = t.errMsg
			res.FinishedAt = t.finishedAt.UnixMilli()
		} else {
			res.State = mihomoStateRunning
		}
	}
	return res
}

type mihomoTaskManager struct {
	mu    sync.Mutex
	tasks map[string]*mihomoTask
}

var mihomoTasks = &mihomoTaskManager{tasks: map[string]*mihomoTask{}}

func (m *mihomoTaskManager) prune(now time.Time) {
	for id, t := range m.tasks {
		t.mu.Lock()
		expired := t.state != mihomoStateRunning && now.Sub(t.finishedAt) > mihomoTaskRetention
		t.mu.Unlock()
		if expired {
			delete(m.tasks, id)
		}
	}
}

func (m *mihomoTaskManager) start(op, scriptURL string, args []string) (*mihomoTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.prune(now)
	for _, t := range m.tasks {
		t.mu.Lock()
		running := t.state == mihomoStateRunning
		t.mu.Unlock()
		if running {
			return nil, fmt.Errorf("已有部署任务正在运行（%s），请稍后再试", t.op)
		}
	}
	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}
	task := &mihomoTask{id: hex.EncodeToString(idBytes), op: op, state: mihomoStateRunning, startedAt: now}
	m.tasks[task.id] = task
	go runMihomoTask(task, scriptURL, args)
	return task, nil
}

func (m *mihomoTaskManager) get(id string) *mihomoTask {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(time.Now())
	return m.tasks[id]
}

func validateMihomoRequest(op, scriptURL string, args []string) error {
	allowed, ok := mihomoOps[op]
	if !ok {
		return fmt.Errorf("不支持的操作: %s", op)
	}
	if !strings.HasPrefix(scriptURL, "https://") || len(scriptURL) > 2048 || strings.ContainsAny(scriptURL, " \t\r\n") {
		return errors.New("脚本地址必须是 https:// 开头的有效地址")
	}
	for i := 0; i < len(args); i++ {
		name := args[i]
		if !allowed[name] {
			return fmt.Errorf("不支持的参数: %s", name)
		}
		if mihomoFlagOnly[name] {
			continue
		}
		if i+1 >= len(args) {
			return fmt.Errorf("参数 %s 缺少值", name)
		}
		i++
		if !reMihomoArgValue.MatchString(args[i]) {
			return fmt.Errorf("参数 %s 的值包含不支持的字符", name)
		}
	}
	return nil
}

func (w *WebSocketReporter) handleMihomoScript(data interface{}) (interface{}, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("序列化数据失败: %v", err)
	}
	var req mihomoScriptRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("解析部署参数失败: %v", err)
	}
	switch req.Action {
	case "start":
		op := strings.TrimSpace(req.Op)
		scriptURL := strings.TrimSpace(req.ScriptURL)
		if err := validateMihomoRequest(op, scriptURL, req.Args); err != nil {
			return nil, err
		}
		task, err := mihomoTasks.start(op, scriptURL, append([]string(nil), req.Args...))
		if err != nil {
			return nil, err
		}
		return task.result(0), nil
	case "poll":
		task := mihomoTasks.get(strings.TrimSpace(req.TaskID))
		if task == nil {
			return nil, errors.New("部署任务不存在（节点端可能已重启）")
		}
		return task.result(req.Offset), nil
	default:
		return nil, fmt.Errorf("未知操作: %s", req.Action)
	}
}

func runMihomoTask(task *mihomoTask, scriptURL string, args []string) {
	defer func() {
		if r := recover(); r != nil {
			task.finish(mihomoStateFailed, -1, "", fmt.Sprintf("部署任务异常: %v", r))
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), mihomoTaskTimeout)
	defer cancel()

	fmt.Fprintf(&task.log, "[节点] 下载脚本 %s\n", scriptURL)
	script, err := mihomoFetchScript(ctx, scriptURL)
	if err != nil {
		fmt.Fprintf(&task.log, "[节点] 下载脚本失败: %v\n", err)
		task.finish(mihomoStateFailed, -1, "", "下载脚本失败: "+err.Error())
		return
	}
	tmp, err := os.CreateTemp("", "flvx-mihomo-*.sh")
	if err != nil {
		task.finish(mihomoStateFailed, -1, "", "创建临时文件失败: "+err.Error())
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(script); err != nil {
		_ = tmp.Close()
		task.finish(mihomoStateFailed, -1, "", "写入脚本失败: "+err.Error())
		return
	}
	_ = tmp.Close()

	fmt.Fprintf(&task.log, "[节点] 执行 mihomo_script %s\n", task.op)
	exitCode, runErr := mihomoRunScript(ctx, tmp.Name(), task.op, args, &task.log)
	if ctx.Err() == context.DeadlineExceeded {
		fmt.Fprintf(&task.log, "\n[节点] 超过 %d 分钟未完成，已终止\n", int(mihomoTaskTimeout/time.Minute))
		task.finish(mihomoStateFailed, exitCode, "", "执行超时")
		return
	}
	if runErr != nil || exitCode != 0 {
		msg := fmt.Sprintf("脚本退出码 %d", exitCode)
		if runErr != nil && exitCode < 0 {
			msg = runErr.Error()
		}
		fmt.Fprintf(&task.log, "\n[节点] 执行失败: %s\n", msg)
		task.finish(mihomoStateFailed, exitCode, "", msg)
		return
	}
	share := ""
	if task.op == "install" || task.op == "info" {
		if content, err := readMihomoShareFile(); err == nil {
			share = content
		} else {
			fmt.Fprintf(&task.log, "[节点] 读取分享链接失败: %v\n", err)
		}
	}
	fmt.Fprintf(&task.log, "\n[节点] 执行完成\n")
	task.finish(mihomoStateSuccess, 0, share, "")
}

func readMihomoShareFile() (string, error) {
	f, err := os.Open(mihomoShareFile)
	if err != nil {
		return "", err
	}
	defer f.Close()
	content, err := io.ReadAll(io.LimitReader(f, mihomoShareMaxSize))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(content)), nil
}

func fetchMihomoScript(ctx context.Context, scriptURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, mihomoDownloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scriptURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, mihomoScriptMaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(content) > mihomoScriptMaxSize {
		return nil, errors.New("脚本文件过大")
	}
	if !bytes.HasPrefix(content, []byte("#!")) || !bytes.Contains(content, []byte("mihomo_script")) {
		return nil, errors.New("下载的内容不是 mihomo_script 脚本（请检查脚本地址或代理设置）")
	}
	return content, nil
}

func runMihomoScriptProcess(ctx context.Context, scriptPath, op string, args []string, out io.Writer) (int, error) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		return -1, errors.New("节点上没有 bash，mihomo_script 需要 bash 和 systemd")
	}
	cmd := exec.CommandContext(ctx, bash, append([]string{scriptPath, op}, args...)...)
	cmd.Dir = os.TempDir()
	cmd.Env = append(os.Environ(), "TERM=dumb", "NO_COLOR=1", "DEBIAN_FRONTEND=noninteractive")
	cmd.Stdin = nil
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.WaitDelay = 10 * time.Second
	err = cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), err
	}
	return -1, err
}
