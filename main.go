package main

import (
	"embed"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"panda-survey/internal/auth"
	"panda-survey/internal/config"
	"panda-survey/internal/db"
	"panda-survey/internal/handler"
	"panda-survey/internal/middleware"
	"panda-survey/internal/questiontype"
	"panda-survey/internal/service"
	"panda-survey/internal/store"
)

//go:embed all:web
var webFS embed.FS

func main() {
	cfg := config.Load()

	// 运行日志：fpk 启动脚本已将 stdout 重定向到 LOG_FILE（app.log），
	// 此时不再重复写文件，仅把该路径作为反馈诊断的日志来源。
	// 本地直接运行（未注入 LOG_FILE）时，写入 DB 同目录的 server.log，便于调试与反馈。
	if cfg.LogPath == "" {
		cfg.LogPath = filepath.Join(filepath.Dir(cfg.DBPath), "server.log")
	}
	if f, err := os.OpenFile(cfg.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		log.SetOutput(io.MultiWriter(os.Stdout, f))
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}

	questiontype.RegisterAll()

	authSvc := auth.New(database, cfg.SessionTTL)
	authSvc.PromoteAdmins(cfg.AdminUsernames)

	deps := &handler.Deps{
		Cfg:        cfg,
		SessionTTL: cfg.SessionTTL,
		Auth:       authSvc,
		Surveys:    &store.SurveyStore{DB: database},
		Responses:  &store.ResponseStore{DB: database},
		Settings:   &store.SettingsStore{DB: database},
		AIUsage:    &store.AIUsageStore{DB: database},
		Admin:      &store.AdminStore{DB: database},
		Banks:      &store.BankStore{DB: database},
		SurveySvc:  &service.SurveyService{Surveys: &store.SurveyStore{DB: database}, Responses: &store.ResponseStore{DB: database}},
		StatsSvc:   &service.StatsService{Surveys: &store.SurveyStore{DB: database}, Responses: &store.ResponseStore{DB: database}},
		Limiter:        middleware.NewRateLimiter(10),
		LoginLimit:     middleware.NewRateLimiter(5),
		FeedbackLimit:  middleware.NewRateLimiter(5),
		StartTime:      time.Now(),
	}

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, deps, webFS)

	// 后台巡检：到截止时间的问卷自动停止
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if n, err := deps.Surveys.SweepAutoStop(); err == nil && n > 0 {
				log.Printf("自动停止 %d 份到截止时间的问卷", n)
			}
		}
	}()

	log.Printf("熊猫问卷已启动: http://localhost:%s", cfg.Port)
	log.Fatal(http.ListenAndServe(cfg.Addr(), mux))
}
