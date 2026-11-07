package proxykit

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHTarget holds one-off SSH credentials for a deployment. They are used for
// a single run and are never stored by the panel.
type SSHTarget struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	AuthType   string `json:"authType"` // password | private_key
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"privateKey,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
	UseSudo    bool   `json:"useSudo"`
}

// Validate checks the credentials.
func (t SSHTarget) Validate() error {
	if strings.TrimSpace(t.Host) == "" {
		return fmt.Errorf("SSH 主机不能为空")
	}
	if strings.TrimSpace(t.Username) == "" {
		return fmt.Errorf("SSH 用户名不能为空")
	}
	switch t.AuthType {
	case "password":
		if t.Password == "" {
			return fmt.Errorf("SSH 密码不能为空")
		}
	case "private_key":
		if strings.TrimSpace(t.PrivateKey) == "" {
			return fmt.Errorf("SSH 私钥不能为空")
		}
	default:
		return fmt.Errorf("不支持的 SSH 认证方式")
	}
	if t.Port < 0 || t.Port > 65535 {
		return fmt.Errorf("SSH 端口无效")
	}
	return nil
}

func (t SSHTarget) clientConfig() (*ssh.ClientConfig, error) {
	var auth ssh.AuthMethod
	switch t.AuthType {
	case "password":
		auth = ssh.Password(t.Password)
	case "private_key":
		var (
			signer ssh.Signer
			err    error
		)
		if t.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(t.PrivateKey), []byte(t.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(t.PrivateKey))
		}
		if err != nil {
			return nil, fmt.Errorf("SSH 私钥解析失败: %w", err)
		}
		auth = ssh.PublicKeys(signer)
	default:
		return nil, fmt.Errorf("不支持的 SSH 认证方式")
	}
	return &ssh.ClientConfig{
		User:            strings.TrimSpace(t.Username),
		Auth:            []ssh.AuthMethod{auth},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         20 * time.Second,
	}, nil
}

// lockedWriter serialises stdout/stderr writes from the SSH session.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// RunSSHScript runs a shell script on the target, streaming combined output
// to out. The script is sent on stdin to `sh -s` so it never shows up in the
// remote process list.
func RunSSHScript(ctx context.Context, t SSHTarget, script string, out io.Writer) error {
	if err := t.Validate(); err != nil {
		return err
	}
	cfg, err := t.clientConfig()
	if err != nil {
		return err
	}
	port := t.Port
	if port <= 0 {
		port = 22
	}
	addr := net.JoinHostPort(strings.Trim(strings.TrimSpace(t.Host), "[]"), strconv.Itoa(port))
	dialer := net.Dialer{Timeout: 20 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("SSH 连接失败: %w", err)
	}
	defer conn.Close()

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		return fmt.Errorf("SSH 认证失败: %w", err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("SSH 会话创建失败: %w", err)
	}
	defer session.Close()

	w := &lockedWriter{w: out}
	session.Stdout = w
	session.Stderr = w
	session.Stdin = strings.NewReader(script)

	done := make(chan error, 1)
	go func() { done <- session.Run("sh -s") }()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGTERM)
		_ = session.Close()
		return fmt.Errorf("SSH 执行超时或被取消: %w", ctx.Err())
	case err := <-done:
		if err != nil {
			return fmt.Errorf("远程执行失败: %w", err)
		}
		return nil
	}
}
