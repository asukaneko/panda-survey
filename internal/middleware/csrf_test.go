package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// csrfStatus 构造一个 POST /api/auth/register 并返回 CSRF 中间件处理后的状态码。
// 默认 Host 为应用真实监听地址 192.168.100.254:43210。
func csrfStatus(allowed []string, trustProxy bool, mod func(*http.Request)) int {
	r := httptest.NewRequest(http.MethodPost, "/api/auth/register", nil)
	r.Header.Set("Content-Type", "application/json")
	r.Host = "192.168.100.254:43210"
	if mod != nil {
		mod(r)
	}
	w := httptest.NewRecorder()
	CSRF(allowed, trustProxy)(okHandler()).ServeHTTP(w, r)
	return w.Code
}

// 核心回归：fnOS 网关 / Nginx 等反向代理场景。
// 浏览器视为同源（相对路径、无预检），但服务端 r.Host 是被代理后的内部端口。
// 旧实现按 Origin==Host 精确比较会误杀，Sec-Fetch-Site 可正确识别为同源。
func TestCSRFProxiedRequestAllowed(t *testing.T) {
	code := csrfStatus(nil, false, func(r *http.Request) {
		r.Header.Set("Origin", "http://192.168.100.254:5666")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	})
	if code != http.StatusOK {
		t.Fatalf("代理同源请求应放行，实际 %d", code)
	}
}

// 自定义域名 + HTTPS 反代：Origin 与内部 Host 完全不同，仍应放行。
func TestCSRFCustomDomainProxiedAllowed(t *testing.T) {
	code := csrfStatus(nil, false, func(r *http.Request) {
		r.Header.Set("Origin", "https://survey.aykeji.cn")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	})
	if code != http.StatusOK {
		t.Fatalf("自定义域名反代应放行，实际 %d", code)
	}
}

// 真正的跨站发起必须拒绝 —— CSRF 防护的核心语义不能丢。
func TestCSRFCrossSiteRejected(t *testing.T) {
	if code := csrfStatus(nil, false, func(r *http.Request) {
		r.Header.Set("Origin", "http://evil.example.com")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
	}); code != http.StatusForbidden {
		t.Fatalf("跨站请求应拒绝，实际 %d", code)
	}
	// 旧浏览器无 Sec-Fetch-Site 时，靠 Origin 与 Host 比较兜底
	if code := csrfStatus(nil, false, func(r *http.Request) {
		r.Header.Set("Origin", "http://evil.example.com")
	}); code != http.StatusForbidden {
		t.Fatalf("旧浏览器跨站请求应拒绝，实际 %d", code)
	}
}

// 直连（未走代理）的同源请求，两种判定路径都应放行。
func TestCSRFDirectSameOriginAllowed(t *testing.T) {
	for name, mod := range map[string]func(*http.Request){
		"sec-fetch-site": func(r *http.Request) {
			r.Header.Set("Origin", "http://192.168.100.254:43210")
			r.Header.Set("Sec-Fetch-Site", "same-origin")
		},
		"legacy-origin": func(r *http.Request) {
			r.Header.Set("Origin", "http://192.168.100.254:43210")
		},
		"legacy-referer": func(r *http.Request) {
			r.Header.Set("Referer", "http://192.168.100.254:43210/register")
		},
	} {
		if code := csrfStatus(nil, false, mod); code != http.StatusOK {
			t.Fatalf("%s 直连同源应放行，实际 %d", name, code)
		}
	}
}

// 非浏览器客户端（curl / 脚本）不带来源信息，且已被 Content-Type 约束，应放行。
func TestCSRFNoOriginClientAllowed(t *testing.T) {
	if code := csrfStatus(nil, false, nil); code != http.StatusOK {
		t.Fatalf("无来源的非浏览器客户端应放行，实际 %d", code)
	}
}

// ALLOWED_ORIGINS 白名单优先级最高，可覆盖跨站拒绝（用于外部站点嵌入投稿）。
func TestCSRFAllowedOriginsOverrides(t *testing.T) {
	allowed := []string{"https://portal.example.com"}
	if code := csrfStatus(allowed, false, func(r *http.Request) {
		r.Header.Set("Origin", "https://portal.example.com")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
	}); code != http.StatusOK {
		t.Fatalf("白名单来源应放行，实际 %d", code)
	}
}

// 老浏览器 + 反代：无 Sec-Fetch-Site 时需 TRUST_PROXY 还原外部地址。
func TestCSRFLegacyBrowserWithTrustedProxy(t *testing.T) {
	mod := func(r *http.Request) {
		r.Header.Set("Origin", "http://192.168.100.254:5666")
		r.Header.Set("X-Forwarded-Host", "192.168.100.254:5666")
	}
	if code := csrfStatus(nil, false, mod); code != http.StatusForbidden {
		t.Fatalf("未开启 TRUST_PROXY 时不应信任 X-Forwarded-Host，实际 %d", code)
	}
	if code := csrfStatus(nil, true, mod); code != http.StatusOK {
		t.Fatalf("开启 TRUST_PROXY 后应放行，实际 %d", code)
	}
}

// 写接口仍强制 application/json。
func TestCSRFRejectsNonJSON(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/auth/register", nil)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	CSRF(nil, false)(okHandler()).ServeHTTP(w, r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("非 JSON 写请求应拒绝，实际 %d", w.Code)
	}
}

// 读请求不受 CSRF 约束。
func TestCSRFIgnoresSafeMethods(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/surveys", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	CSRF(nil, false)(okHandler()).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET 不应受 CSRF 约束，实际 %d", w.Code)
	}
}

func TestHostsEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"192.168.100.254:43210", "192.168.100.254:43210", true},
		{"192.168.100.254:43210", "192.168.100.254:5666", false},
		{"Example.COM", "example.com", true},         // 主机大小写不敏感
		{"example.com", "example.com:80", true},      // 缺省端口 == 默认端口
		{"example.com:443", "example.com", true},     // 默认端口 == 缺省端口
		{"example.com:80", "example.com:443", false}, // 端口不同
		{"[::1]:43210", "[::1]:43210", true},         // IPv6
		{"[::1]:43210", "[::1]:5666", false},
	}
	for _, c := range cases {
		if got := hostsEqual(c.a, c.b); got != c.want {
			t.Errorf("hostsEqual(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestHostOf(t *testing.T) {
	cases := map[string]string{
		"http://192.168.100.254:5666":       "192.168.100.254:5666",
		"https://survey.aykeji.cn/register": "survey.aykeji.cn",
		"https://a.com/x?y=1#z":             "a.com",
		"192.168.100.254:43210":             "192.168.100.254:43210",
	}
	for in, want := range cases {
		if got := hostOf(in); got != want {
			t.Errorf("hostOf(%q) = %q, want %q", in, got, want)
		}
	}
}
