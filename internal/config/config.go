package config

import (
	"os"
	"strings"
	"time"
)

// Config 汇总全部环境变量配置，main 启动时加载一次。
type Config struct {
	Port           string
	DBPath         string
	LogPath        string // 运行日志路径：由 fpk 启动脚本注入 LOG_FILE；空则本地默认写入 DB 同目录
	Version        string // 应用版本，随 fpk 打包注入 APP_VERSION；前端检测更新与之对应
	SessionTTL     time.Duration
	BaseURL        string   // 分享链接前缀，留空则前端用 location.origin
	SecretKey      string   // AI API Key 加密密钥，任意非空字符串，内部派生 AES key
	AdminUsernames []string // 指定管理员用户名
	AllowedOrigins []string // 额外受信任的来源（反向代理 / 自定义域名 / 外部嵌入），逗号分隔
	TrustProxy     bool     // 是否信任 X-Forwarded-Host 还原外部地址（老浏览器回退路径）
}

func Load() Config {
	c := Config{
		Port:      envOr("PORT", "43210"),
		DBPath:    envOr("DB_PATH", "./panda.db"),
		LogPath:   os.Getenv("LOG_FILE"),
		Version:   envOr("APP_VERSION", "0.2.1"),
		BaseURL:   strings.TrimRight(os.Getenv("BASE_URL"), "/"),
		SecretKey: os.Getenv("SECRET_KEY"),
	}
	ttl, err := time.ParseDuration(envOr("SESSION_TTL", "168h"))
	if err != nil || ttl <= 0 {
		ttl = 168 * time.Hour
	}
	c.SessionTTL = ttl
	for _, n := range strings.Split(os.Getenv("ADMIN_USERNAMES"), ",") {
		if n = strings.TrimSpace(n); n != "" {
			c.AdminUsernames = append(c.AdminUsernames, n)
		}
	}
	for _, o := range strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.AllowedOrigins = append(c.AllowedOrigins, o)
		}
	}
	c.TrustProxy = os.Getenv("TRUST_PROXY") == "1" || strings.EqualFold(os.Getenv("TRUST_PROXY"), "true")
	return c
}

func (c Config) Addr() string { return ":" + c.Port }

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
