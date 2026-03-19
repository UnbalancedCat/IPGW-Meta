package utils

import (
	"context"
	"net"
	"net/http"
	"net/http/cookiejar"
	"time"
)

// userAgentTransport 是一个原生的 RoundTripper 拦截器，
// 用于模拟 Python 中 session.headers.update(...) 全局携带请求头的行为。
type userAgentTransport struct {
	rt http.RoundTripper
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// 每次请求前附加强制伪装的头部
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36")
	}
	return t.rt.RoundTrip(req)
}

// NewNetworkClient 用于初始化一个全局通用的认证客户端
func NewNetworkClient() *http.Client {
	// 构建内存 CookieJar（对标 Python requests.Session 会话自动持有 Cookie 功能）
	jar, _ := cookiejar.New(nil)

	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	transport := &http.Transport{
		// 1. 设置代理为 nil 以阻断继承环境变量中的代理
		// （对标 Python 脚本中的 session.trust_env = False）
		Proxy: nil,

		// 2. 底层网络拨号劫持：强制网络连接走 tcp4（纯 IPv4 寻址）
		// （对标 Python 中的 socket.getaddrinfo 拦截机制）
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", addr)
		},

		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		// 将原生 Transport 包装进劫持了 UA 的 Transport 里
		Transport: &userAgentTransport{rt: transport},
		Jar:       jar,
		Timeout:   15 * time.Second, // 限制统一网络超时防进程挂死
	}
}
