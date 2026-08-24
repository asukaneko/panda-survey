package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"panda-survey/internal/middleware"
	"panda-survey/internal/model"
)

type createSurveyReq struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

func (d *Deps) handleCreateSurvey(w http.ResponseWriter, r *http.Request) {
	var req createSurveyReq
	if !readJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		req.Title = "未命名问卷"
	}
	s, err := d.Surveys.Create(middleware.User(r).UserID, strings.TrimSpace(req.Title), req.Description)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	ok(w, s)
}

func (d *Deps) handleListSurveys(w http.ResponseWriter, r *http.Request) {
	list, err := d.Surveys.List(middleware.User(r).UserID)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	if list == nil {
		list = []model.Survey{}
	}
	ok(w, list)
}

func (d *Deps) surveyID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		fail(w, http.StatusBadRequest, 1001, "问卷 id 无效")
		return 0, false
	}
	return id, true
}

func (d *Deps) handleGetSurvey(w http.ResponseWriter, r *http.Request) {
	id, okID := d.surveyID(w, r)
	if !okID {
		return
	}
	s, err := d.Surveys.GetOwned(id, middleware.User(r).UserID)
	if err != nil {
		mapErr(w, err)
		return
	}
	qs, err := d.Surveys.Questions(id)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	if qs == nil {
		qs = []model.Question{}
	}
	ok(w, map[string]any{"survey": s, "questions": qs})
}

type saveSurveyReq struct {
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	UpdatedAt   string                 `json:"updated_at"` // 乐观锁版本
	Questions   []model.QuestionPayload `json:"questions"`
}

func (d *Deps) handleSaveSurvey(w http.ResponseWriter, r *http.Request) {
	id, okID := d.surveyID(w, r)
	if !okID {
		return
	}
	var req saveSurveyReq
	if !readJSON(w, r, &req) {
		return
	}
	if req.Questions == nil {
		req.Questions = []model.QuestionPayload{}
	}
	err := d.SurveySvc.ValidateAndSave(id, middleware.User(r).UserID,
		strings.TrimSpace(req.Title), req.Description, req.Questions, req.UpdatedAt)
	if err != nil {
		mapErr(w, err)
		return
	}
	s, err := d.Surveys.Get(id)
	if err != nil {
		mapErr(w, err)
		return
	}
	ok(w, s)
}

func (d *Deps) handleDeleteSurvey(w http.ResponseWriter, r *http.Request) {
	id, okID := d.surveyID(w, r)
	if !okID {
		return
	}
	if err := d.Surveys.SoftDelete(id, middleware.User(r).UserID); err != nil {
		mapErr(w, err)
		return
	}
	ok(w, nil)
}

type publishReq struct {
	Deadline     string `json:"deadline"` // RFC3339 或 datetime-local 格式，可空
	MaxResponses *int64 `json:"max_responses"`
}

// parseDeadline 兼容 datetime-local（YYYY-MM-DDTHH:MM）与 RFC3339，统一转 UTC 存储。
func parseDeadline(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	// 前端提交的是用户本地时间，按服务器时区解释
	layouts := []string{"2006-01-02T15:04", "2006-01-02 15:04", time.RFC3339, "2006-01-02"}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return t.UTC().Format(time.RFC3339Nano), nil
		}
	}
	return "", fmt.Errorf("截止时间格式无效")
}

func (d *Deps) handlePublish(w http.ResponseWriter, r *http.Request) {
	id, okID := d.surveyID(w, r)
	if !okID {
		return
	}
	var req publishReq
	if !readJSON(w, r, &req) {
		return
	}
	deadline, err := parseDeadline(req.Deadline)
	if err != nil {
		fail(w, http.StatusBadRequest, 1001, err.Error())
		return
	}
	if err := d.Surveys.Publish(id, middleware.User(r).UserID, deadline, req.MaxResponses); err != nil {
		mapErr(w, err)
		return
	}
	s, _ := d.Surveys.Get(id)
	ok(w, s)
}

func (d *Deps) handleStop(w http.ResponseWriter, r *http.Request) {
	id, okID := d.surveyID(w, r)
	if !okID {
		return
	}
	if err := d.Surveys.Stop(id, middleware.User(r).UserID); err != nil {
		mapErr(w, err)
		return
	}
	s, _ := d.Surveys.Get(id)
	ok(w, s)
}

func (d *Deps) handleCopy(w http.ResponseWriter, r *http.Request) {
	id, okID := d.surveyID(w, r)
	if !okID {
		return
	}
	s, err := d.Surveys.Copy(id, middleware.User(r).UserID)
	if err != nil {
		mapErr(w, err)
		return
	}
	ok(w, s)
}

// handleSweep 手动触发自动停止巡检（也由后台定时任务调用）。
func (d *Deps) handleSweep(w http.ResponseWriter, r *http.Request) {
	n, err := d.Surveys.SweepAutoStop()
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	ok(w, map[string]int64{"stopped": n})
}
