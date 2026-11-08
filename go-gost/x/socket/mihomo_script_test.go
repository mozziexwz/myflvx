package socket

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateMihomoRequest(t *testing.T) {
	const url = "https://raw.githubusercontent.com/mozziexwz/mihomo_script/main/mihomo.sh"
	valid := []string{
		"-y", "--protocols", "ss,mieru,sudoku", "--host", "203.0.113.5", "--ip-version", "ipv4-prefer",
		"--block-private", "1", "--ss-port", "23456", "--ss-cipher", "2022-blake3-aes-128-gcm",
		"--ss-password", "AbC+/dEf==", "--mieru-user", "u1a2b3c", "--mieru-password", "0123456789abcdef",
		"--sudoku-key", "8d3c1f9e-1111-4222-8333-444455556666", "--sudoku-padding", "1-15",
		"--sudoku-custom-tables", "xpxvvpvv,vxpvxvvp",
	}
	if err := validateMihomoRequest("install", url, valid); err != nil {
		t.Fatalf("valid install rejected: %v", err)
	}
	if err := validateMihomoRequest("uninstall", url, []string{"-y"}); err != nil {
		t.Fatalf("valid uninstall rejected: %v", err)
	}
	if err := validateMihomoRequest("info", url, nil); err != nil {
		t.Fatalf("valid info rejected: %v", err)
	}

	cases := []struct {
		name string
		op   string
		url  string
		args []string
	}{
		{"unknown op", "log", url, nil},
		{"plain http", "info", "http://example.com/mihomo.sh", nil},
		{"unknown option", "install", url, []string{"--evil", "1"}},
		{"option not allowed for op", "uninstall", url, []string{"--host", "1.2.3.4"}},
		{"missing value", "install", url, []string{"--host"}},
		{"shell metacharacters", "install", url, []string{"--host", "1.2.3.4;reboot"}},
		{"command substitution", "install", url, []string{"--name", "$(id)"}},
		{"space in value", "install", url, []string{"--name", "a b"}},
		{"positional argument", "install", url, []string{"ss"}},
	}
	for _, tc := range cases {
		if err := validateMihomoRequest(tc.op, tc.url, tc.args); err == nil {
			t.Errorf("%s: expected an error", tc.name)
		}
	}
}

func TestMihomoLogKeepsTailWithOffsets(t *testing.T) {
	var l mihomoLog
	chunk := strings.Repeat("x", 1024)
	for i := 0; i < (mihomoLogLimit/1024)+10; i++ {
		_, _ = l.Write([]byte(chunk))
	}
	text, next, truncated := l.read(0)
	if !truncated {
		t.Fatalf("expected truncated read from offset 0")
	}
	if len(text) != mihomoPollChunk {
		t.Fatalf("expected a %d byte chunk, got %d", mihomoPollChunk, len(text))
	}
	total := (mihomoLogLimit/1024 + 10) * 1024
	offset := next
	for {
		text, offset, _ = l.read(offset)
		if text == "" {
			break
		}
	}
	if offset != total {
		t.Fatalf("expected final offset %d, got %d", total, offset)
	}
}

func resetMihomoTasks(t *testing.T) {
	t.Helper()
	origFetch, origRun, origShare := mihomoFetchScript, mihomoRunScript, mihomoShareFile
	mihomoTasks = &mihomoTaskManager{tasks: map[string]*mihomoTask{}}
	t.Cleanup(func() {
		mihomoFetchScript, mihomoRunScript, mihomoShareFile = origFetch, origRun, origShare
		mihomoTasks = &mihomoTaskManager{tasks: map[string]*mihomoTask{}}
	})
}

func pollUntilDone(t *testing.T, w *WebSocketReporter, taskID string) (mihomoScriptResult, string) {
	t.Helper()
	var log strings.Builder
	offset := 0
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		out, err := w.handleMihomoScript(map[string]interface{}{"action": "poll", "taskId": taskID, "offset": offset})
		if err != nil {
			t.Fatalf("poll: %v", err)
		}
		res := out.(mihomoScriptResult)
		log.WriteString(res.Log)
		offset = res.NextOffset
		if res.State != mihomoStateRunning {
			return res, log.String()
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not finish", taskID)
	return mihomoScriptResult{}, ""
}

func TestMihomoTaskInstallReturnsShareLinks(t *testing.T) {
	resetMihomoTasks(t)
	shareFile := filepath.Join(t.TempDir(), "share.txt")
	if err := os.WriteFile(shareFile, []byte("ss://example\nsudoku://example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mihomoShareFile = shareFile
	mihomoFetchScript = func(context.Context, string) ([]byte, error) {
		return []byte("#!/usr/bin/env bash\n# mihomo_script\n"), nil
	}
	var gotOp string
	var gotArgs []string
	mihomoRunScript = func(_ context.Context, scriptPath, op string, args []string, out io.Writer) (int, error) {
		if _, err := os.Stat(scriptPath); err != nil {
			return -1, err
		}
		gotOp, gotArgs = op, args
		fmt.Fprintln(out, "installing mihomo ...")
		return 0, nil
	}

	w := &WebSocketReporter{}
	out, err := w.handleMihomoScript(map[string]interface{}{
		"action": "start", "op": "install", "scriptUrl": "https://example.com/mihomo.sh",
		"args": []string{"-y", "--protocols", "ss,sudoku"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	started := out.(mihomoScriptResult)
	if started.TaskID == "" || started.State != mihomoStateRunning {
		t.Fatalf("unexpected start result: %+v", started)
	}
	res, log := pollUntilDone(t, w, started.TaskID)
	if res.State != mihomoStateSuccess || res.ExitCode != 0 {
		t.Fatalf("expected success, got %+v", res)
	}
	if res.Share != "ss://example\nsudoku://example" {
		t.Fatalf("unexpected share: %q", res.Share)
	}
	if !strings.Contains(log, "installing mihomo") || !strings.Contains(log, "执行完成") {
		t.Fatalf("log missing output: %q", log)
	}
	if gotOp != "install" || strings.Join(gotArgs, " ") != "-y --protocols ss,sudoku" {
		t.Fatalf("unexpected invocation: %s %v", gotOp, gotArgs)
	}
}

func TestMihomoTaskFailureAndSingleRun(t *testing.T) {
	resetMihomoTasks(t)
	mihomoFetchScript = func(context.Context, string) ([]byte, error) {
		return []byte("#!/usr/bin/env bash\n# mihomo_script\n"), nil
	}
	release := make(chan struct{})
	mihomoRunScript = func(_ context.Context, _ string, _ string, _ []string, out io.Writer) (int, error) {
		<-release
		fmt.Fprintln(out, "需要 root 权限")
		return 1, fmt.Errorf("exit status 1")
	}

	w := &WebSocketReporter{}
	start := map[string]interface{}{"action": "start", "op": "restart", "scriptUrl": "https://example.com/mihomo.sh"}
	out, err := w.handleMihomoScript(start)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := w.handleMihomoScript(start); err == nil {
		t.Fatalf("expected a second concurrent task to be rejected")
	}
	close(release)
	res, log := pollUntilDone(t, w, out.(mihomoScriptResult).TaskID)
	if res.State != mihomoStateFailed || res.ExitCode != 1 {
		t.Fatalf("expected failure with exit code 1, got %+v", res)
	}
	if !strings.Contains(log, "需要 root 权限") {
		t.Fatalf("log missing script output: %q", log)
	}
	if _, err := w.handleMihomoScript(map[string]interface{}{"action": "poll", "taskId": "missing"}); err == nil {
		t.Fatalf("expected unknown task to fail")
	}
}
