package middleware

import (
	"context"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"panda-survey/internal/auth"
)

type ctxKey int

const userKey ctxKey = 1

// User 从请求上下文取出 auth 中间件注入的当前用户。
func User(r *http.Request) *authCtx {
	if v, ok := r.Context().Value(userKey).(*authCtx); ok {
		return v
	}
	return nil
}

type authCtx struct {
	Token    string
	Username string
	UserID   int64
	Role     int
}

// ---- 日志与恢复 ----

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, status: 200}
		start := time.Now()
		next.ServeHTTP(sw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
	})
}

func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				log.Printf("panic %s %s: %v", r.Method, r.URL.Path, p)
				http.Error(w, `{"code":1,"msg":"服务器内部错误"}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ---- 认证守卫 ----

// RequireAuth 校验 session cookie；通过后注入用户信息，否则 401。
func RequireAuth(svc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie("panda_session")
			if err != nil || c.Value == "" {
				writeErr(w, http.StatusUnauthorized, 1002, "未登录")
				return
			}
			u, err := svc.UserBySession(c.Value)
			if err != nil {
				writeErr(w, http.StatusUnauthorized, 1002, "未登录或登录已过期")
				return
			}
			ctx := context.WithValue(r.Context(), userKey, &authCtx{
				Token: c.Value, Username: u.Username, UserID: u.ID, Role: u.Role,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin 在 RequireAuth 之后使用，校验管理员角色。
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := User(r); u == nil || u.Role != 1 {
			writeErr(w, http.StatusForbidden, 1003, "需要管理员权限")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- CSRF：写方法（POST/PUT/DELETE）防护 ----
//
// 判定顺序：
//  1. 显式白名单 ALLOWED_ORIGINS 优先。
//  2. 浏览器发送 Sec-Fetch-Site 时以它为准。该头由浏览器自动附加、JS 无法伪造，
//     且基于「发起方页面」与「请求目标」的真实关系计算，不受反向代理改写 Host 的影响，
//     因此能正确处理 fnOS 网关 / Nginx / 自定义域名等代理场景。
//  3. 老浏览器无此头时，回退到 Origin（或 Referer）与服务端外部地址比较；
//     代理场景可用 TRUST_PROXY=1 或 ALLOWED_ORIGINS 放行。
//
// 写接口统一要求 Content-Type: application/json，HTML 表单构造不出该类型，
// 因此来源信息缺失的非浏览器客户端（curl 等）按可信处理。
func CSRF(allowedOrigins []string, trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
				if r.Method != http.MethodDelete {
					ct := r.Header.Get("Content-Type")
					if !strings.HasPrefix(ct, "application/json") {
						writeErr(w, http.StatusUnsupportedMediaType, 1001, "写接口仅接受 application/json")
						return
					}
				}
				if !siteAllowed(r, allowedOrigins, trustProxy) {
					writeErr(w, http.StatusForbidden, 1003, "跨站请求被拒绝")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// siteAllowed 判断写请求的发起方是否可信。
func siteAllowed(r *http.Request, allowed []string, trustProxy bool) bool {
	// 1) 显式登记的外部来源优先放行（反向代理 / 自定义域名 / 嵌入第三方站点）
	if src := r.Header.Get("Origin"); src != "" {
		for _, a := range allowed {
			if hostsEqual(hostOf(src), hostOf(a)) {
				return true
			}
		}
	}
	// 2) 现代浏览器：以 Sec-Fetch-Site 为准
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
	case "same-origin", "same-site", "none":
		return true
	case "cross-site":
		return false
	}
	// 3) 无该头（旧浏览器 / 非浏览器客户端）：回退来源比较
	return originAllowed(r, allowed, trustProxy)
}

// originAllowed 回退路径：比较来源与服务端外部地址。
func originAllowed(r *http.Request, allowed []string, trustProxy bool) bool {
	src := r.Header.Get("Origin")
	if src == "" {
		src = r.Header.Get("Referer")
	}
	if src == "" {
		return true // 来源不可知，且已被 Content-Type 约束
	}
	oh := hostOf(src)
	if oh == "" {
		return false
	}
	if hostsEqual(oh, hostOf(effectiveHost(r, trustProxy))) {
		return true
	}
	for _, a := range allowed {
		if hostsEqual(oh, hostOf(a)) {
			return true
		}
	}
	return false
}

// effectiveHost 返回用于同源比较的服务端外部地址。
// 仅 TRUST_PROXY=1 时信任 X-Forwarded-Host / Forwarded：直接暴露端口时
// 伪造该头并无意义（受害者浏览器不会发送攻击者指定的头）。
func effectiveHost(r *http.Request, trustProxy bool) string {
	if !trustProxy {
		return r.Host
	}
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
		return strings.TrimSpace(strings.Split(fh, ",")[0])
	}
	if f := r.Header.Get("Forwarded"); f != "" {
		for _, part := range strings.Split(f, ",") {
			for _, kv := range strings.Split(part, ";") {
				kv = strings.TrimSpace(kv)
				if len(kv) > 5 && strings.EqualFold(kv[:5], "host=") {
					return strings.Trim(kv[5:], `"`)
				}
			}
		}
	}
	return r.Host
}

// hostOf 从 URL 或 host[:port] 中提取主机部分。
func hostOf(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return s
}

func splitHostPort(h string) (host, port string) {
	if strings.HasPrefix(h, "[") { // IPv6: [::1]:43210
		if i := strings.LastIndex(h, "]"); i >= 0 {
			host = h[:i+1]
			if rest := h[i+1:]; strings.HasPrefix(rest, ":") {
				port = rest[1:]
			}
			return
		}
	}
	if i := strings.LastIndex(h, ":"); i >= 0 {
		return h[:i], h[i+1:]
	}
	return h, ""
}

// hostsEqual 忽略大小写比较主机，并把缺省端口与默认端口(80/443)视为等价
// —— 浏览器对 http/https 默认端口不发送端口号。
func hostsEqual(a, b string) bool {
	ah, ap := splitHostPort(a)
	bh, bp := splitHostPort(b)
	if !strings.EqualFold(strings.Trim(ah, "[]"), strings.Trim(bh, "[]")) {
		return false
	}
	if ap == bp {
		return true
	}
	isDefault := func(p string) bool { return p == "80" || p == "443" }
	return (ap == "" && isDefault(bp)) || (bp == "" && isDefault(ap))
}

// ---- 提交限频：每 IP 每分钟最多 10 次答卷提交 ----

type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // 每秒补充令牌
	burst   float64
}

type bucket struct {
	tokens float64
	last   time.Time
}

func NewRateLimiter(perMinute float64) *RateLimiter {
	return &RateLimiter{
		buckets: map[string]*bucket{},
		rate:    perMinute / 60.0,
		burst:   perMinute,
	}
}

func (l *RateLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.buckets[ip]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[ip] = b
		if len(l.buckets) > 10000 { // 防内存膨胀：粗暴清理
			l.buckets = map[string]*bucket{ip: b}
		}
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// ClientIP 取 RemoteAddr 的主机部分。
// 注意：不信任 X-Forwarded-For —— 未部署可信反向代理时，
// 攻击者可伪造该头绕过 IP 限流。仅当部署可信反代（如 Nginx）且
// 其清除客户端传入的 XFF 时，才应启用 XFF 解析。
func ClientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func writeErr(w http.ResponseWriter, status, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(`{"code":` + itoa(code) + `,"msg":"` + msg + `","data":null}`))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
