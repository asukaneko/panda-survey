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

// ---- CSRF：POST/PUT 仅接受 JSON，写方法一律校验 Origin 同源 ----

func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			if r.Method != http.MethodDelete {
				ct := r.Header.Get("Content-Type")
				if !strings.HasPrefix(ct, "application/json") {
					writeErr(w, http.StatusUnsupportedMediaType, 1001, "写接口仅接受 application/json")
					return
				}
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				if !sameHost(origin, r.Host) {
					writeErr(w, http.StatusForbidden, 1003, "跨站请求被拒绝")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func sameHost(origin, host string) bool {
	// 去掉 scheme，比较 hostname[:port]
	o := origin
	if i := strings.Index(o, "://"); i >= 0 {
		o = o[i+3:]
	}
	o = strings.TrimSuffix(o, "/")
	return strings.EqualFold(o, host)
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
