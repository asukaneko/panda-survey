package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"panda-survey/internal/ai"
	"panda-survey/internal/middleware"
	"panda-survey/internal/templates"
)

/* ---- 模板库 ---- */

func (d *Deps) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	out := make([]map[string]any, 0, len(templates.List))
	for _, t := range templates.List {
		out = append(out, map[string]any{
			"id": t.ID, "name": t.Name, "description": t.Description,
			"question_count": len(t.Questions),
		})
	}
	ok(w, out)
}

type fromTemplateReq struct {
	TemplateID string `json:"template_id"`
}

func (d *Deps) handleCreateFromTemplate(w http.ResponseWriter, r *http.Request) {
	var req fromTemplateReq
	if !readJSON(w, r, &req) {
		return
	}
	tpl := templates.Get(req.TemplateID)
	if tpl == nil {
		fail(w, http.StatusBadRequest, 1001, "模板不存在")
		return
	}
	uid := middleware.User(r).UserID
	s, err := d.Surveys.Create(uid, tpl.Name, tpl.Description)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	err = d.SurveySvc.ValidateAndSave(s.ID, uid, tpl.Name, tpl.Description, tpl.Questions, s.UpdatedAt)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	created, _ := d.Surveys.Get(s.ID)
	ok(w, created)
}

/* ---- 统计只读分享 ---- */

func newShareToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// handleShareStats POST 生成/重置令牌；DELETE 关闭分享。
func (d *Deps) handleShareStats(w http.ResponseWriter, r *http.Request) {
	id, okID := d.surveyID(w, r)
	if !okID {
		return
	}
	uid := middleware.User(r).UserID
	switch r.Method {
	case http.MethodPost:
		token := newShareToken()
		if err := d.Surveys.SetShareToken(id, uid, token); err != nil {
			mapErr(w, err)
			return
		}
		ok(w, map[string]string{"token": token, "path": "/share/" + token})
	case http.MethodDelete:
		if err := d.Surveys.SetShareToken(id, uid, ""); err != nil {
			mapErr(w, err)
			return
		}
		ok(w, nil)
	}
}

// handleSharedStats 匿名只读统计（按令牌；不含明细与 IP）。
func (d *Deps) handleSharedStats(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		fail(w, http.StatusNotFound, 1008, "分享链接无效")
		return
	}
	s, err := d.Surveys.GetByShareToken(token)
	if err != nil {
		fail(w, http.StatusNotFound, 1008, "分享链接无效或已关闭")
		return
	}
	stats, total, err := d.StatsSvc.Stats(s)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	ok(w, map[string]any{
		"survey":    map[string]any{"title": s.Title, "description": s.Description},
		"total":     total,
		"questions": stats,
	})
}

/* ---- AI 摘要 ---- */

type summarizeReq struct {
	SurveyID   int64 `json:"survey_id"`
	QuestionID int64 `json:"question_id"`
}

func (d *Deps) handleAISummarize(w http.ResponseWriter, r *http.Request) {
	client, cfg, okClient := d.loadAIClient(w)
	if !okClient {
		return
	}
	u := middleware.User(r)
	if !d.checkQuota(w, u.UserID, cfg) {
		return
	}
	var req summarizeReq
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
	var target *struct {
		id    int64
		title string
		typ   string
	}
	for _, q := range questions {
		if q.ID == req.QuestionID {
			target = &struct {
				id    int64
				title string
				typ   string
			}{q.ID, q.Title, q.Type}
			break
		}
	}
	if target == nil {
		fail(w, http.StatusBadRequest, 1001, "题目不属于该问卷")
		return
	}
	if target.typ != "text" && target.typ != "textarea" {
		fail(w, http.StatusBadRequest, 1001, "AI 摘要仅支持文本题")
		return
	}
	answers, err := d.Responses.RecentTexts(target.id, 500)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	ctx, cancel := longTaskCtx(r, cfg)
	defer cancel()
	summary, err := ai.SummarizeAnswers(ctx, client, target.title, answers)
	if err != nil {
		fail(w, http.StatusBadRequest, 1006, err.Error())
		return
	}
	d.AIUsage.Incr(u.UserID, "summarize", survey.ID)
	ok(w, map[string]string{"summary": summary})
}
