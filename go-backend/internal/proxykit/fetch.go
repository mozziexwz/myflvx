package proxykit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// MaxSubscriptionBytes caps downloaded subscription content.
const MaxSubscriptionBytes = 10 << 20

// DefaultSubscriptionUA makes most providers return a mihomo/Clash YAML.
const DefaultSubscriptionUA = "clash.meta/1.19 mihomo/1.19"

// ErrBlockedAddress is returned when a subscription URL resolves to a
// loopback, private or otherwise internal address.
var ErrBlockedAddress = errors.New("订阅地址指向内网或本机地址，已拒绝")

// AllowPrivateFetchForTesting lets tests fetch from httptest servers.
var AllowPrivateFetchForTesting = false

func blockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() ||
		isCGNAT(ip) || ip.Equal(net.ParseIP("168.63.129.16"))
}

func isCGNAT(ip net.IP) bool {
	ip4 := ip.To4()
	return ip4 != nil && ip4[0] == 100 && ip4[1]&0xc0 == 64
}

// newSafeHTTPClient returns a client that refuses to connect to internal
// addresses. The check runs on the address actually dialled, so DNS
// rebinding cannot bypass it.
func newSafeHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout: 15 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			if AllowPrivateFetchForTesting {
				return nil
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if blockedIP(net.ParseIP(host)) {
				return ErrBlockedAddress
			}
			return nil
		},
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("重定向次数过多")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("不支持的重定向协议: %s", req.URL.Scheme)
			}
			return nil
		},
	}
}

// SubscriptionInfo is parsed from the `subscription-userinfo` header.
type SubscriptionInfo struct {
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
	Total    int64 `json:"total"`
	Expire   int64 `json:"expire"`
}

// ParseSubscriptionUserInfo parses "upload=1; download=2; total=3; expire=4".
func ParseSubscriptionUserInfo(v string) *SubscriptionInfo {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	info := &SubscriptionInfo{}
	found := false
	for _, part := range strings.Split(v, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64)
		if err != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(kv[0])) {
		case "upload":
			info.Upload, found = int64(n), true
		case "download":
			info.Download, found = int64(n), true
		case "total":
			info.Total, found = int64(n), true
		case "expire":
			info.Expire, found = int64(n), true
		}
	}
	if !found {
		return nil
	}
	return info
}

// FetchResult is the downloaded subscription.
type FetchResult struct {
	Content  []byte
	UserInfo string
	FileName string
}

// ValidateSubscriptionURL checks the scheme and host of a subscription URL.
func ValidateSubscriptionURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("订阅地址格式错误")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("订阅地址只支持 http / https")
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("订阅地址缺少主机名")
	}
	return u, nil
}

// FetchSubscription downloads a subscription with SSRF protection.
func FetchSubscription(ctx context.Context, rawURL, userAgent string) (*FetchResult, error) {
	u, err := ValidateSubscriptionURL(rawURL)
	if err != nil {
		return nil, err
	}
	if userAgent = strings.TrimSpace(userAgent); userAgent == "" {
		userAgent = DefaultSubscriptionUA
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	resp, err := newSafeHTTPClient(60 * time.Second).Do(req)
	if err != nil {
		if errors.Is(err, ErrBlockedAddress) {
			return nil, ErrBlockedAddress
		}
		return nil, fmt.Errorf("下载订阅失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("下载订阅失败: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxSubscriptionBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取订阅失败: %w", err)
	}
	if len(body) > MaxSubscriptionBytes {
		return nil, fmt.Errorf("订阅内容超过 %d MB", MaxSubscriptionBytes>>20)
	}
	result := &FetchResult{Content: body, UserInfo: resp.Header.Get("Subscription-Userinfo")}
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if idx := strings.Index(strings.ToLower(cd), "filename="); idx >= 0 {
			result.FileName = strings.Trim(strings.TrimSpace(cd[idx+len("filename="):]), `"'`)
		}
	}
	return result, nil
}
