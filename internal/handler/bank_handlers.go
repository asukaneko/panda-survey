package handler

import (
	"net/http"
	"strconv"
	"strings"

	"panda-survey/internal/middleware"
	"panda-survey/internal/model"
	"panda-survey/internal/service"
)

/* ---- 题库管理（需登录，仅限本人）---- */

type bankReq struct {
	Name string `json:"name"`
}

func (d *Deps) handleListBanks(w http.ResponseWriter, r *http.Request) {
	list, err := d.Banks.List(middleware.User(r).UserID)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	if list == nil {
		list = []model.QuestionBank{}
	}
	ok(w, list)
}

func (d *Deps) handleCreateBank(w http.ResponseWriter, r *http.Request) {
	var req bankReq
	if !readJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = "未命名题库"
	}
	if len([]rune(req.Name)) > 100 {
		fail(w, http.StatusBadRequest, 1001, "题库名称不超过 100 字")
		return
	}
	b, err := d.Banks.Create(middleware.User(r).UserID, req.Name)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	ok(w, b)
}

func (d *Deps) bankID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		fail(w, http.StatusBadRequest, 1001, "题库 id 无效")
		return 0, false
	}
	return id, true
}

func (d *Deps) handleRenameBank(w http.ResponseWriter, r *http.Request) {
	id, okID := d.bankID(w, r)
	if !okID {
		return
	}
	var req bankReq
	if !readJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		fail(w, http.StatusBadRequest, 1001, "题库名称不能为空")
		return
	}
	if len([]rune(req.Name)) > 100 {
		fail(w, http.StatusBadRequest, 1001, "题库名称不超过 100 字")
		return
	}
	if err := d.Banks.Rename(id, middleware.User(r).UserID, req.Name); err != nil {
		mapErr(w, err)
		return
	}
	b, _ := d.Banks.Get(id)
	ok(w, b)
}

func (d *Deps) handleDeleteBank(w http.ResponseWriter, r *http.Request) {
	id, okID := d.bankID(w, r)
	if !okID {
		return
	}
	if err := d.Banks.SoftDelete(id, middleware.User(r).UserID); err != nil {
		mapErr(w, err)
		return
	}
	ok(w, nil)
}

func (d *Deps) handleListBankQuestions(w http.ResponseWriter, r *http.Request) {
	id, okID := d.bankID(w, r)
	if !okID {
		return
	}
	b, err := d.Banks.GetOwned(id, middleware.User(r).UserID)
	if err != nil {
		mapErr(w, err)
		return
	}
	qs, err := d.Banks.Questions(b.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	if qs == nil {
		qs = []model.BankQuestion{}
	}
	ok(w, map[string]any{"bank": b, "questions": qs})
}

type bankQuestionReq struct {
	Type   string                 `json:"type"`
	Title  string                 `json:"title"`
	Config model.QuestionConfig   `json:"config"`
}

func (d *Deps) handleAddBankQuestion(w http.ResponseWriter, r *http.Request) {
	id, okID := d.bankID(w, r)
	if !okID {
		return
	}
	b, err := d.Banks.GetOwned(id, middleware.User(r).UserID)
	if err != nil {
		mapErr(w, err)
		return
	}
	var req bankQuestionReq
	if !readJSON(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if err := service.ValidateQuizQuestion(req.Type, req.Title, &req.Config); err != nil {
		fail(w, http.StatusBadRequest, 1001, err.Error())
		return
	}
	qid, err := d.Banks.AddQuestion(b.ID, model.BankQuestionPayload{Type: req.Type, Title: req.Title, Config: req.Config})
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, err.Error())
		return
	}
	ok(w, map[string]int64{"id": qid})
}

func (d *Deps) bankQuestionID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("qid"), 10, 64)
	if err != nil || id <= 0 {
		fail(w, http.StatusBadRequest, 1001, "题目 id 无效")
		return 0, false
	}
	return id, true
}

func (d *Deps) handleUpdateBankQuestion(w http.ResponseWriter, r *http.Request) {
	id, okID := d.bankID(w, r)
	if !okID {
		return
	}
	qid, okQ := d.bankQuestionID(w, r)
	if !okQ {
		return
	}
	b, err := d.Banks.GetOwned(id, middleware.User(r).UserID)
	if err != nil {
		mapErr(w, err)
		return
	}
	var req bankQuestionReq
	if !readJSON(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if err := service.ValidateQuizQuestion(req.Type, req.Title, &req.Config); err != nil {
		fail(w, http.StatusBadRequest, 1001, err.Error())
		return
	}
	if err := d.Banks.UpdateQuestion(b.ID, qid,
		model.BankQuestionPayload{Type: req.Type, Title: req.Title, Config: req.Config}); err != nil {
		mapErr(w, err)
		return
	}
	ok(w, nil)
}

func (d *Deps) handleDeleteBankQuestion(w http.ResponseWriter, r *http.Request) {
	id, okID := d.bankID(w, r)
	if !okID {
		return
	}
	qid, okQ := d.bankQuestionID(w, r)
	if !okQ {
		return
	}
	if _, err := d.Banks.GetOwned(id, middleware.User(r).UserID); err != nil {
		mapErr(w, err)
		return
	}
	if err := d.Banks.DeleteQuestion(id, qid); err != nil {
		mapErr(w, err)
		return
	}
	ok(w, nil)
}
