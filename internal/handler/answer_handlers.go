package handler

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"

	"panda-survey/internal/middleware"
	"panda-survey/internal/model"
	"panda-survey/internal/service"
)

// handlePublicView 填答端问卷视图：仅发布中可见，只回题目不回统计。
// 答题卷额外回 quiz_config，并剥离题目正确答案（不泄露给填写端）。
func (d *Deps) handlePublicView(w http.ResponseWriter, r *http.Request) {
	id, okID := d.surveyID(w, r)
	if !okID {
		return
	}
	s, err := d.Surveys.Get(id)
	if err != nil {
		mapErr(w, err)
		return
	}
	switch s.Status {
	case 0:
		fail(w, http.StatusNotFound, 1008, "问卷尚未发布")
		return
	case 2:
		fail(w, http.StatusNotFound, 1008, "问卷已停止回收")
		return
	}
	qs, err := d.Surveys.Questions(id)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	if len(qs) == 0 {
		fail(w, http.StatusNotFound, 1008, "问卷暂无题目")
	}
	isQuiz := s.Kind == model.KindQuiz
	for i := range qs {
		qs[i].Config.Correct = nil // 正确答案不出网
	}
	if isQuiz && s.QuizConfig.QuestionOrder == "random" {
		rand.Shuffle(len(qs), func(i, j int) { qs[i], qs[j] = qs[j], qs[i] })
	}
	ok(w, map[string]any{
		"survey":    map[string]any{"id": s.ID, "title": s.Title, "description": s.Description, "kind": s.Kind, "quiz_config": s.QuizConfig},
		"questions": qs,
	})
}

type submitReq struct {
	Answers  []model.AnswerInput  `json:"answers"`
	Profile  []model.ProfileValue `json:"profile"` // 答题卷个人信息
	Duration int64                `json:"duration"`
}

// handleSubmit 匿名提交答卷：限频 + 逐题校验 + 事务落库。
// 答题卷（kind=1）服务端判分并返回成绩；show_answer 时附答案与逐题对错。
func (d *Deps) handleSubmit(w http.ResponseWriter, r *http.Request) {
	id, okID := d.surveyID(w, r)
	if !okID {
		return
	}
	ip := middleware.ClientIP(r)
	if !d.Limiter.Allow(ip) {
		fail(w, http.StatusTooManyRequests, 1005, "提交过于频繁，请稍后再试")
		return
	}
	var req submitReq
	if !readJSON(w, r, &req) {
		return
	}
	s, err := d.Surveys.Get(id)
	if err != nil {
		mapErr(w, err)
		return
	}
	if req.Answers == nil {
		req.Answers = []model.AnswerInput{}
	}
	if req.Duration < 0 {
		req.Duration = 0
	}
	if s.Kind == model.KindQuiz {
		rid, result, err := d.SurveySvc.SubmitQuiz(s, req.Answers, req.Profile, ip,
			r.UserAgent(), req.Duration)
		if err != nil {
			mapErr(w, err)
			return
		}
		ok(w, map[string]any{"response_id": rid, "score": result.Score, "total": result.Total, "results": result.Results})
		return
	}
	rid, err := d.SurveySvc.SubmitResponse(s, req.Answers, ip,
		r.UserAgent(), req.Duration)
	if err != nil {
		mapErr(w, err)
		return
	}
	ok(w, map[string]int64{"response_id": rid})
}

// handleLeaderboard 答题卷排行榜（公开）：仅发布中的答题卷且开启 show_ranking 时可见。
func (d *Deps) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	id, okID := d.surveyID(w, r)
	if !okID {
		return
	}
	s, err := d.Surveys.Get(id)
	if err != nil {
		mapErr(w, err)
		return
	}
	if s.Kind != model.KindQuiz || !s.QuizConfig.ShowRanking {
		fail(w, http.StatusNotFound, 1008, "该答题未开放排行榜")
		return
	}
	if s.Status != 1 {
		fail(w, http.StatusNotFound, 1008, "答题已结束")
		return
	}
	rows, err := d.Responses.Leaderboard(id, 50)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	out := make([]model.LeaderboardEntry, 0, len(rows))
	for i, row := range rows {
		out = append(out, model.LeaderboardEntry{
			Rank:       i + 1,
			ResponseID: row.ID,
			Name:       leaderboardName(row.Profile),
			Score:      row.Score,
			Duration:   row.Duration,
			CreatedAt:  row.CreatedAt,
		})
	}
	ok(w, map[string]any{"entries": out})
}

// leaderboardName 从个人信息 JSON 提取展示名（第一个有值字段），无则匿名。
func leaderboardName(profileJSON string) string {
	if profileJSON == "" {
		return "匿名"
	}
	var vals []model.ProfileValue
	if json.Unmarshal([]byte(profileJSON), &vals) != nil {
		return "匿名"
	}
	for _, v := range vals {
		v.Value = strings.TrimSpace(v.Value)
		if v.Value != "" {
			r := []rune(v.Value)
			if len(r) > 20 {
				return string(r[:20])
			}
			return v.Value
		}
	}
	return "匿名"
}

// handleStats 逐题统计（仅本人）。
func (d *Deps) handleStats(w http.ResponseWriter, r *http.Request) {
	id, okID := d.surveyID(w, r)
	if !okID {
		return
	}
	s, err := d.Surveys.GetOwned(id, middleware.User(r).UserID)
	if err != nil {
		mapErr(w, err)
		return
	}
	stats, total, err := d.StatsSvc.Stats(s)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	if stats == nil {
		stats = []model.QuestionStats{}
	}
	data := map[string]any{"survey": s, "total": total, "questions": stats}
	if s.Kind == model.KindQuiz {
		if avg, err := d.StatsSvc.AvgScore(s.ID); err == nil {
			data["avg_score"] = avg
		}
	}
	ok(w, data)
}

// handleExportCSV 明细/汇总导出，UTF-8 带 BOM。
func (d *Deps) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	id, okID := d.surveyID(w, r)
	if !okID {
		return
	}
	s, err := d.Surveys.GetOwned(id, middleware.User(r).UserID)
	if err != nil {
		mapErr(w, err)
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode != "detail" && mode != "summary" {
		fail(w, http.StatusBadRequest, 1001, "mode 须为 detail 或 summary")
		return
	}
	var csv string
	if mode == "detail" {
		csv, err = d.StatsSvc.ExportDetail(s)
	} else {
		csv, err = d.StatsSvc.ExportSummary(s)
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="survey-%d-%s.csv"`, id, mode))
	w.Write([]byte(csv))
}

// handleListResponses 明细答卷列表（仅本人），供统计页逐份查看与删除。
func (d *Deps) handleListResponses(w http.ResponseWriter, r *http.Request) {
	id, okID := d.surveyID(w, r)
	if !okID {
		return
	}
	if _, err := d.Surveys.GetOwned(id, middleware.User(r).UserID); err != nil {
		mapErr(w, err)
		return
	}
	questions, err := d.Surveys.Questions(id)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	responses, err := d.Responses.List(id)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	answers, err := d.Responses.AnswersOfSurvey(id)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	byRID := map[int64][]model.AnswerRow{}
	for _, a := range answers {
		byRID[a.ResponseID] = append(byRID[a.ResponseID], a)
	}
	out := make([]map[string]any, 0, len(responses))
	for _, resp := range responses {
		item := map[string]any{
			"id": resp.ID, "created_at": resp.CreatedAt,
			"duration": resp.Duration, "ip": resp.IP, "answers": map[string]string{},
			"score": resp.Score, "profile": resp.Profile,
		}
		ansMap := item["answers"].(map[string]string)
		for _, a := range byRID[resp.ID] {
			for _, q := range questions {
				if q.ID == a.QuestionID {
					ansMap[q.Title] = service.AnswerDisplay(q.Type, a.Value, q.Config)
					break
				}
			}
		}
		out = append(out, item)
	}
	ok(w, out)
}

// handleDeleteResponse 删除单份答卷（仅本人问卷，统计即时回减）。
func (d *Deps) handleDeleteResponse(w http.ResponseWriter, r *http.Request) {
	id, okID := d.surveyID(w, r)
	if !okID {
		return
	}
	rid, err := strconv.ParseInt(r.PathValue("rid"), 10, 64)
	if err != nil || rid <= 0 {
		fail(w, http.StatusBadRequest, 1001, "答卷 id 无效")
		return
	}
	if _, err := d.Surveys.GetOwned(id, middleware.User(r).UserID); err != nil {
		mapErr(w, err)
		return
	}
	if err := d.Responses.Delete(id, rid); err != nil {
		mapErr(w, err)
		return
	}
	ok(w, nil)
}
