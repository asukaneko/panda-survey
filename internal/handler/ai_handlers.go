package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"panda-survey/internal/ai"
	"panda-survey/internal/middleware"
	"panda-survey/internal/model"
)

// longTaskCtx 长任务（生成整卷 / Agent 编辑 / 单题优化）超时预算：
// 基础超时的 3 倍，下限 2 分钟、上限 10 分钟。
func longTaskCtx(r *http.Request, cfg ai.Config) (context.Context, context.CancelFunc) {
	sec := cfg.TimeoutSec * 3
	if sec > 600 {
		sec = 600
	}
	if sec < 120 {
		sec = 120
	}
	return context.WithTimeout(r.Context(), time.Duration(sec)*time.Second)
}

// loadAIClient 加载 AI 配置；未配置返回 false 并已写出响应。
func (d *Deps) loadAIClient(w http.ResponseWriter) (*ai.Client, ai.Config, bool) {
	cfg, err := ai.Load(d.Settings, d.Cfg.SecretKey)
	if err != nil {
		if errors.Is(err, ai.ErrNotConfigured) {
			fail(w, http.StatusBadRequest, 1006, "AI 服务未配置，请联系管理员在设置页配置")
		} else {
			fail(w, http.StatusInternalServerError, 1006, "AI 配置加载失败："+err.Error())
		}
		return nil, cfg, false
	}
	return ai.NewClient(cfg), cfg, true
}

// checkQuota 每用户每日配额；超额返回 false 并已写出响应。
func (d *Deps) checkQuota(w http.ResponseWriter, userID int64, cfg ai.Config) bool {
	used, err := d.AIUsage.CountToday(userID)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, "查询用量失败")
		return false
	}
	if used >= int64(cfg.DailyQuota) {
		fail(w, http.StatusTooManyRequests, 1005,
			"今日 AI 调用配额已用完（每日 "+intToStr(cfg.DailyQuota)+" 次）")
		return false
	}
	return true
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// handleAIStatus 前端入口门控：是否开通、配额、今日已用。
func (d *Deps) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	cfg, err := ai.Load(d.Settings, d.Cfg.SecretKey)
	if err != nil && !errors.Is(err, ai.ErrNotConfigured) {
		fail(w, http.StatusInternalServerError, 1006, err.Error())
		return
	}
	configured := err == nil
	quota, used := cfg.DailyQuota, 0
	if configured {
		if n, err := d.AIUsage.CountToday(middleware.User(r).UserID); err == nil {
			used = int(n)
		}
	}
	ok(w, map[string]any{"enabled": configured, "quota": quota, "used_today": used})
}

type generateReq struct {
	Prompt string `json:"prompt"`
}

func (d *Deps) handleAIGenerate(w http.ResponseWriter, r *http.Request) {
	client, cfg, okClient := d.loadAIClient(w)
	if !okClient {
		return
	}
	u := middleware.User(r)
	if !d.checkQuota(w, u.UserID, cfg) {
		return
	}
	var req generateReq
	if !readJSON(w, r, &req) {
		return
	}
	ctx, cancel := longTaskCtx(r, cfg)
	defer cancel()
	gen, err := ai.GenerateSurvey(ctx, client, req.Prompt)
	if err != nil {
		fail(w, http.StatusBadRequest, 1006, err.Error())
		return
	}
	d.AIUsage.Incr(u.UserID, "generate_survey", 0)
	ok(w, gen)
}

// handleAIGenerateQuiz AI 生成答题卷草稿（标题/描述/答题配置/带答案与分值的题目）。
func (d *Deps) handleAIGenerateQuiz(w http.ResponseWriter, r *http.Request) {
	client, cfg, okClient := d.loadAIClient(w)
	if !okClient {
		return
	}
	u := middleware.User(r)
	if !d.checkQuota(w, u.UserID, cfg) {
		return
	}
	var req generateReq
	if !readJSON(w, r, &req) {
		return
	}
	ctx, cancel := longTaskCtx(r, cfg)
	defer cancel()
	gen, err := ai.GenerateQuiz(ctx, client, req.Prompt)
	if err != nil {
		fail(w, http.StatusBadRequest, 1006, err.Error())
		return
	}
	d.AIUsage.Incr(u.UserID, "generate_quiz", 0)
	ok(w, gen)
}

// handleAIGenerateBankQuestions AI 生成一组题库题目（预览后由前端逐个导入题库）。
func (d *Deps) handleAIGenerateBankQuestions(w http.ResponseWriter, r *http.Request) {
	client, cfg, okClient := d.loadAIClient(w)
	if !okClient {
		return
	}
	u := middleware.User(r)
	if !d.checkQuota(w, u.UserID, cfg) {
		return
	}
	var req generateReq
	if !readJSON(w, r, &req) {
		return
	}
	ctx, cancel := longTaskCtx(r, cfg)
	defer cancel()
	questions, err := ai.GenerateBankQuestions(ctx, client, req.Prompt)
	if err != nil {
		fail(w, http.StatusBadRequest, 1006, err.Error())
		return
	}
	d.AIUsage.Incr(u.UserID, "generate_bank", 0)
	ok(w, map[string]any{"questions": questions})
}

type agentEditReq struct {
	SurveyID    int64  `json:"survey_id"`
	Instruction string `json:"instruction"`
}

func (d *Deps) handleAIAgentEdit(w http.ResponseWriter, r *http.Request) {
	client, cfg, okClient := d.loadAIClient(w)
	if !okClient {
		return
	}
	u := middleware.User(r)
	if !d.checkQuota(w, u.UserID, cfg) {
		return
	}
	var req agentEditReq
	if !readJSON(w, r, &req) {
		return
	}
	survey, err := d.Surveys.GetOwned(req.SurveyID, u.UserID)
	if err != nil {
		mapErr(w, err)
		return
	}
	questions, err := d.Surveys.Questions(survey.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	if len(questions) == 0 {
		fail(w, http.StatusBadRequest, 1001, "问卷还没有题目，请先添加题目或使用 AI 创建问卷")
		return
	}
	ctx, cancel := longTaskCtx(r, cfg)
	defer cancel()
	result, err := ai.RunAgentEdit(ctx, client, *survey, questions, req.Instruction)
	if err != nil {
		fail(w, http.StatusBadRequest, 1006, err.Error())
		return
	}
	d.AIUsage.Incr(u.UserID, "agent_edit", survey.ID)
	ok(w, result)
}

type optimizeReq struct {
	Question model.QuestionPayload `json:"question"`
}

func (d *Deps) handleAIOptimize(w http.ResponseWriter, r *http.Request) {
	client, cfg, okClient := d.loadAIClient(w)
	if !okClient {
		return
	}
	u := middleware.User(r)
	if !d.checkQuota(w, u.UserID, cfg) {
		return
	}
	var req optimizeReq
	if !readJSON(w, r, &req) {
		return
	}
	ctx, cancel := longTaskCtx(r, cfg)
	defer cancel()
	result, err := ai.OptimizeQuestion(ctx, client, req.Question)
	if err != nil {
		fail(w, http.StatusBadRequest, 1006, err.Error())
		return
	}
	d.AIUsage.Incr(u.UserID, "optimize_question", 0)
	ok(w, result)
}
