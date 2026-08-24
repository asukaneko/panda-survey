package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"panda-survey/internal/ai"
	"panda-survey/internal/middleware"
	"panda-survey/internal/store"
)

// ---- 管理员：AI 配置 ----

type aiConfigReq struct {
	BaseURL    string `json:"base_url"`
	APIKey     string `json:"api_key"`
	Model      string `json:"model"`
	TimeoutSec int    `json:"timeout_sec"`
	DailyQuota int    `json:"daily_quota"`
}

func (d *Deps) handleGetAIConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := ai.Load(d.Settings, d.Cfg.SecretKey)
	configured := true
	if err != nil {
		if !errors.Is(err, ai.ErrNotConfigured) {
			fail(w, http.StatusInternalServerError, 1, err.Error())
			return
		}
		configured = false
		cfg = ai.DefaultConfig()
	}
	ok(w, map[string]any{"configured": configured, "config": ai.Masked(cfg)})
}

func (d *Deps) handleSaveAIConfig(w http.ResponseWriter, r *http.Request) {
	var req aiConfigReq
	if !readJSON(w, r, &req) {
		return
	}
	req.BaseURL = strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	if !strings.HasPrefix(req.BaseURL, "http://") && !strings.HasPrefix(req.BaseURL, "https://") {
		fail(w, http.StatusBadRequest, 1001, "Base URL 须以 http(s) 开头")
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		fail(w, http.StatusBadRequest, 1001, "模型名不能为空")
		return
	}
	cfg := ai.Config{
		BaseURL: req.BaseURL, APIKey: req.APIKey, Model: strings.TrimSpace(req.Model),
		TimeoutSec: req.TimeoutSec, DailyQuota: req.DailyQuota,
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 60
	}
	if cfg.TimeoutSec > 300 {
		cfg.TimeoutSec = 300
	}
	if cfg.DailyQuota <= 0 {
		cfg.DailyQuota = 50
	}
	if cfg.DailyQuota > 10000 {
		cfg.DailyQuota = 10000
	}
	// 掩码 Key 表示未修改：沿用已存 Key
	if ai.KeyMasked(cfg.APIKey) {
		old, err := ai.Load(d.Settings, d.Cfg.SecretKey)
		if err != nil && !errors.Is(err, ai.ErrNotConfigured) {
			fail(w, http.StatusInternalServerError, 1, err.Error())
			return
		}
		if old.APIKey != "" {
			cfg.APIKey = old.APIKey
		}
	}
	if cfg.APIKey == "" {
		fail(w, http.StatusBadRequest, 1001, "API Key 不能为空")
		return
	}
	if err := ai.Save(d.Settings, d.Cfg.SecretKey, cfg); err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	ok(w, ai.Masked(cfg))
}

// handleTestAIConfig 连接测试：优先用请求体里的值（未保存也可测），掩码 Key 用已存的。
func (d *Deps) handleTestAIConfig(w http.ResponseWriter, r *http.Request) {
	var req aiConfigReq
	if !readJSON(w, r, &req) {
		return
	}
	cfg := ai.Config{
		BaseURL: strings.TrimRight(strings.TrimSpace(req.BaseURL), "/"),
		APIKey:  req.APIKey, Model: strings.TrimSpace(req.Model),
		TimeoutSec: req.TimeoutSec,
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 15 // 测试用短超时
	}
	if ai.KeyMasked(cfg.APIKey) || (cfg.APIKey == "" && cfg.BaseURL == "") {
		saved, err := ai.Load(d.Settings, d.Cfg.SecretKey)
		if err != nil {
			fail(w, http.StatusBadRequest, 1006, "AI 未配置或配置不完整，无法测试")
			return
		}
		if ai.KeyMasked(cfg.APIKey) || cfg.APIKey == "" {
			cfg.APIKey = saved.APIKey
		}
		if cfg.BaseURL == "" {
			cfg.BaseURL = saved.BaseURL
		}
		if cfg.Model == "" {
			cfg.Model = saved.Model
		}
	}
	if cfg.BaseURL == "" || cfg.APIKey == "" || cfg.Model == "" {
		fail(w, http.StatusBadRequest, 1001, "Base URL、API Key、模型名均不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := ai.NewClient(cfg).Ping(ctx); err != nil {
		fail(w, http.StatusBadRequest, 1006, "连接失败："+err.Error())
		return
	}
	ok(w, map[string]bool{"reachable": true})
}

/* ---- 管理员后台：用户管理 / 全站问卷管理 / 概览 ---- */

func (d *Deps) requireAdminID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("uid"), 10, 64)
	if err != nil || id <= 0 {
		fail(w, http.StatusBadRequest, 1001, "用户 id 无效")
		return 0, false
	}
	return id, true
}

func (d *Deps) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	ov, err := d.Admin.Overview()
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	ok(w, ov)
}

func (d *Deps) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	users, err := d.Admin.Users()
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	if users == nil {
		users = []store.AdminUser{}
	}
	ok(w, users)
}

func (d *Deps) handleAdminUserBan(w http.ResponseWriter, r *http.Request) {
	uid, okID := d.requireAdminID(w, r)
	if !okID {
		return
	}
	me := middleware.User(r)
	if me.UserID == uid {
		fail(w, http.StatusBadRequest, 1001, "不能封禁自己")
		return
	}
	if err := d.Admin.SetUserStatus(uid, 1); err != nil {
		mapErr(w, err)
		return
	}
	ok(w, nil)
}

func (d *Deps) handleAdminUserUnban(w http.ResponseWriter, r *http.Request) {
	uid, okID := d.requireAdminID(w, r)
	if !okID {
		return
	}
	if err := d.Admin.SetUserStatus(uid, 0); err != nil {
		mapErr(w, err)
		return
	}
	ok(w, nil)
}

func (d *Deps) handleAdminUserDelete(w http.ResponseWriter, r *http.Request) {
	uid, okID := d.requireAdminID(w, r)
	if !okID {
		return
	}
	me := middleware.User(r)
	if me.UserID == uid {
		fail(w, http.StatusBadRequest, 1001, "不能删除自己")
		return
	}
	if err := d.Admin.DeleteUser(uid); err != nil {
		mapErr(w, err)
		return
	}
	ok(w, nil)
}

func (d *Deps) handleAdminSurveys(w http.ResponseWriter, r *http.Request) {
	surveys, err := d.Admin.Surveys()
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	if surveys == nil {
		surveys = []store.AdminSurvey{}
	}
	ok(w, surveys)
}

func (d *Deps) adminSurveyID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		fail(w, http.StatusBadRequest, 1001, "问卷 id 无效")
		return 0, false
	}
	return id, true
}

func (d *Deps) handleAdminSurveyStop(w http.ResponseWriter, r *http.Request) {
	id, okID := d.adminSurveyID(w, r)
	if !okID {
		return
	}
	if err := d.Admin.ForceStop(id); err != nil {
		mapErr(w, err)
		return
	}
	ok(w, nil)
}

func (d *Deps) handleAdminSurveyDelete(w http.ResponseWriter, r *http.Request) {
	id, okID := d.adminSurveyID(w, r)
	if !okID {
		return
	}
	if err := d.Admin.ForceDelete(id); err != nil {
		mapErr(w, err)
		return
	}
	ok(w, nil)
}
