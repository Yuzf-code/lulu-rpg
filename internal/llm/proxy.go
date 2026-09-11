package llm

import (
	"net/http"
	"net/url"
)

// transportWithProxy 返回出站传输层：proxy 为空时直连，
// 否则显式走指定代理。默认不读环境的 http_proxy——自托管/局域网
// 服务（Ollama、vLLM 等）是最常见目标，被本机代理劫持会直接 502。
func TransportWithProxy(proxy string) http.RoundTripper {
	t := &http.Transport{}
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			t.Proxy = http.ProxyURL(u)
		}
	}
	return t
}
