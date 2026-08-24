package handler

import (
	"io/fs"
	"net/http"
	"strings"
	"time"

	"panda-survey/internal/auth"
	"panda-survey/internal/config"
	"panda-survey/internal/middleware"
	"panda-survey/internal/service"
	"panda-survey/internal/store"
)

// Deps 汇总全部依赖，由 main 装配后传入。
type Deps struct {
	Cfg        config.Config
	SessionTTL time.Duration
	Auth       *auth.Service
	Surveys    *store.SurveyStore
	Responses  *store.ResponseStore
	Settings   *store.SettingsStore
	AIUsage    *store.AIUsageStore
	Admin      *store.AdminStore
	SurveySvc  *service.SurveyService
	StatsSvc   *service.StatsService
	Limiter       *middleware.RateLimiter
	LoginLimit    *middleware.RateLimiter
	FeedbackLimit *middleware.RateLimiter
	StartTime     time.Time
}

type middlewareFn func(http.Handler) http.Handler

func chain(h http.HandlerFunc, mws ...middlewareFn) http.Handler {
	var out http.Handler = h
	for i := len(mws) - 1; i >= 0; i-- {
		out = mws[i](out)
	}
	return out
}

// RegisterRoutes 注册全部 API 路由与页面路由。
func RegisterRoutes(mux *http.ServeMux, d *Deps, webFS fs.FS) {
	base := []middlewareFn{middleware.Recover, middleware.Logging, middleware.CSRF}
	authed := append(append([]middlewareFn{}, base...), middleware.RequireAuth(d.Auth))
	admin := append(append([]middlewareFn{}, authed...), middleware.RequireAdmin)

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]string{"status": "up", "time": time.Now().UTC().Format(time.RFC3339)})
	})

	// 认证
	mux.Handle("POST /api/auth/register", chain(d.handleRegister, base...))
	mux.Handle("POST /api/auth/login", chain(d.handleLogin, base...))
	mux.Handle("POST /api/auth/logout", chain(d.handleLogout, base...))
	mux.Handle("GET /api/auth/me", chain(d.handleMe, authed...))
	mux.Handle("PUT /api/auth/password", chain(d.handleChangePassword, authed...))

	// 问卷管理（需登录，仅限本人）
	mux.Handle("GET /api/surveys", chain(d.handleListSurveys, authed...))
	mux.Handle("POST /api/surveys", chain(d.handleCreateSurvey, authed...))
	mux.Handle("GET /api/surveys/{id}", chain(d.handleGetSurvey, authed...))
	mux.Handle("PUT /api/surveys/{id}", chain(d.handleSaveSurvey, authed...))
	mux.Handle("DELETE /api/surveys/{id}", chain(d.handleDeleteSurvey, authed...))
	mux.Handle("POST /api/surveys/{id}/publish", chain(d.handlePublish, authed...))
	mux.Handle("POST /api/surveys/{id}/stop", chain(d.handleStop, authed...))
	mux.Handle("POST /api/surveys/{id}/copy", chain(d.handleCopy, authed...))

	// 填答与统计
	mux.Handle("GET /api/surveys/{id}/public", chain(d.handlePublicView, base...))
	mux.Handle("POST /api/surveys/{id}/responses", chain(d.handleSubmit, base...))
	mux.Handle("GET /api/surveys/{id}/stats", chain(d.handleStats, authed...))
	mux.Handle("GET /api/surveys/{id}/responses", chain(d.handleListResponses, authed...))
	mux.Handle("GET /api/surveys/{id}/export", chain(d.handleExportCSV, authed...))
	mux.Handle("DELETE /api/surveys/{id}/responses/{rid}", chain(d.handleDeleteResponse, authed...))
	mux.Handle("POST /api/surveys/sweep", chain(d.handleSweep, admin...))

	// 管理员：AI 配置 + 后台管理
	mux.Handle("GET /api/admin/ai-config", chain(d.handleGetAIConfig, admin...))
	mux.Handle("POST /api/admin/ai-config", chain(d.handleSaveAIConfig, admin...))
	mux.Handle("POST /api/admin/ai-config/test", chain(d.handleTestAIConfig, admin...))
	mux.Handle("GET /api/admin/overview", chain(d.handleAdminOverview, admin...))
	mux.Handle("GET /api/admin/users", chain(d.handleAdminUsers, admin...))
	mux.Handle("PUT /api/admin/users/{uid}/ban", chain(d.handleAdminUserBan, admin...))
	mux.Handle("PUT /api/admin/users/{uid}/unban", chain(d.handleAdminUserUnban, admin...))
	mux.Handle("DELETE /api/admin/users/{uid}", chain(d.handleAdminUserDelete, admin...))
	mux.Handle("GET /api/admin/surveys", chain(d.handleAdminSurveys, admin...))
	mux.Handle("POST /api/admin/surveys/{id}/stop", chain(d.handleAdminSurveyStop, admin...))
	mux.Handle("DELETE /api/admin/surveys/{id}", chain(d.handleAdminSurveyDelete, admin...))

	// 模板库（需登录）
	mux.Handle("GET /api/templates", chain(d.handleListTemplates, authed...))
	mux.Handle("POST /api/surveys/from-template", chain(d.handleCreateFromTemplate, authed...))

	// 统计只读分享
	mux.Handle("POST /api/surveys/{id}/share-stats", chain(d.handleShareStats, authed...))
	mux.Handle("DELETE /api/surveys/{id}/share-stats", chain(d.handleShareStats, authed...))
	mux.Handle("GET /api/share/{token}/stats", chain(d.handleSharedStats, base...))

	// AI 能力（需登录 + 配额）
	mux.Handle("GET /api/ai/status", chain(d.handleAIStatus, authed...))
	mux.Handle("POST /api/ai/generate-survey", chain(d.handleAIGenerate, authed...))
	mux.Handle("POST /api/ai/agent-edit", chain(d.handleAIAgentEdit, authed...))
	mux.Handle("POST /api/ai/optimize-question", chain(d.handleAIOptimize, authed...))
	mux.Handle("POST /api/ai/summarize-answers", chain(d.handleAISummarize, authed...))

	// 页面与静态资源
	mux.Handle("GET /", Pages(webFS))

	// Bug 反馈：浏览器调用本接口，后端补上服务端诊断与运行日志尾部后再转发 panda 主页
	mux.Handle("POST /api/feedback", chain(d.handleFeedback, base...))
}

// Pages 前端路由：路径 -> 内嵌 HTML；/css /js /vendor 走静态文件。
// 所有响应带 no-cache：以协商缓存确保前端更新后刷新即生效，避免新旧资源混搭。
func Pages(webFS fs.FS) http.Handler {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		p := r.URL.Path
		var file string
		switch {
		case p == "/":
			file = "index.html"
		case p == "/login":
			file = "login.html"
		case p == "/register":
			file = "register.html"
		case p == "/about":
			file = "about.html"
		case p == "/console":
			file = "console.html"
		case p == "/admin", p == "/admin/settings":
			if p == "/admin/settings" {
				http.Redirect(w, r, "/admin", http.StatusFound)
				return
			}
			file = "admin.html"
		case strings.HasPrefix(p, "/stats/"):
			file = "stats.html"
		case strings.HasPrefix(p, "/share/"), strings.HasPrefix(p, "/s/"), strings.HasPrefix(p, "/preview/"):
			if strings.HasPrefix(p, "/share/") {
				file = "stats.html"
			} else {
				file = "fill.html"
			}
		default:
			if strings.HasPrefix(p, "/css/") || strings.HasPrefix(p, "/js/") || strings.HasPrefix(p, "/vendor/") {
				fileServer.ServeHTTP(w, r)
				return
			}
			http.NotFound(w, r)
			return
		}
		data, err := fs.ReadFile(sub, file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})
}
