package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"panda-survey/internal/model"
)

var (
	ErrNotFound  = errors.New("资源不存在")
	ErrForbidden = errors.New("无权操作")
	ErrConflict  = errors.New("数据已被修改，请刷新后重试")
	ErrState     = errors.New("问卷当前状态不允许该操作")
)

const surveyCols = `id, user_id, title, description, status, deadline, max_responses, created_at, updated_at`

type SurveyStore struct{ DB *sql.DB }

func scanSurvey(row interface{ Scan(...any) error }) (*model.Survey, error) {
	var s model.Survey
	var deadline, maxResp sql.NullString
	err := row.Scan(&s.ID, &s.UserID, &s.Title, &s.Description, &s.Status, &deadline, &maxResp, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if deadline.Valid {
		s.Deadline = deadline.String
	}
	if maxResp.Valid {
		var m int64
		fmt.Sscan(maxResp.String, &m)
		s.MaxResponses = &m
	}
	return &s, nil
}

// Create 新建空白草稿问卷。
func (st *SurveyStore) Create(userID int64, title, description string) (*model.Survey, error) {
	now := model.NowUTC()
	res, err := st.DB.Exec(
		`INSERT INTO surveys(user_id, title, description, status, created_at, updated_at) VALUES (?,?,?,0,?,?)`,
		userID, title, description, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return st.Get(id)
}

func (st *SurveyStore) Get(id int64) (*model.Survey, error) {
	return scanSurvey(st.DB.QueryRow(
		`SELECT `+surveyCols+` FROM surveys WHERE id = ? AND deleted_at IS NULL`, id))
}

// GetOwned 取问卷并校验归属，他人问卷返回 ErrForbidden。
func (st *SurveyStore) GetOwned(id, userID int64) (*model.Survey, error) {
	s, err := st.Get(id)
	if err != nil {
		return nil, err
	}
	if s.UserID != userID {
		return nil, ErrForbidden
	}
	return s, nil
}

// List 我的问卷列表（含回收量），按更新时间倒序。
func (st *SurveyStore) List(userID int64) ([]model.Survey, error) {
	rows, err := st.DB.Query(`
		SELECT s.id, s.user_id, s.title, s.description, s.status, s.deadline, s.max_responses,
		       s.created_at, s.updated_at,
		       (SELECT COUNT(*) FROM responses r WHERE r.survey_id = s.id) AS rc
		FROM surveys s
		WHERE s.user_id = ? AND s.deleted_at IS NULL
		ORDER BY s.updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Survey
	for rows.Next() {
		var s model.Survey
		var deadline, maxResp sql.NullString
		if err := rows.Scan(&s.ID, &s.UserID, &s.Title, &s.Description, &s.Status, &deadline, &maxResp,
			&s.CreatedAt, &s.UpdatedAt, &s.ResponseCount); err != nil {
			return nil, err
		}
		if deadline.Valid {
			s.Deadline = deadline.String
		}
		if maxResp.Valid {
			var m int64
			fmt.Sscan(maxResp.String, &m)
			s.MaxResponses = &m
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (st *SurveyStore) Questions(surveyID int64) ([]model.Question, error) {
	rows, err := st.DB.Query(`
		SELECT id, survey_id, type, title, required, sort_order, config, created_at
		FROM questions WHERE survey_id = ? ORDER BY sort_order, id`, surveyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Question
	for rows.Next() {
		var q model.Question
		var cfg string
		var req int
		if err := rows.Scan(&q.ID, &q.SurveyID, &q.Type, &q.Title, &req, &q.SortOrder, &cfg, &q.CreatedAt); err != nil {
			return nil, err
		}
		q.Required = req == 1
		if err := jsonUnmarshalConfig(cfg, &q.Config); err != nil {
			return nil, fmt.Errorf("题目 %d config 损坏: %w", q.ID, err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// Save 整体替换保存（仅草稿可编辑）。expectedUpdatedAt 为乐观锁版本。
func (st *SurveyStore) Save(surveyID, userID int64, title, description string,
	questions []model.QuestionPayload, expectedUpdatedAt string) error {
	tx, err := st.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var owner int64
	var status int
	var updatedAt string
	err = tx.QueryRow(
		`SELECT user_id, status, updated_at FROM surveys WHERE id = ? AND deleted_at IS NULL`, surveyID,
	).Scan(&owner, &status, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if owner != userID {
		return ErrForbidden
	}
	if status != 0 {
		return fmt.Errorf("%w：仅草稿状态可编辑，已发布的问卷请停止后复制为新问卷", ErrState)
	}
	if expectedUpdatedAt != "" && expectedUpdatedAt != updatedAt {
		return ErrConflict
	}
	now := model.NowUTC()
	if _, err := tx.Exec(
		`UPDATE surveys SET title = ?, description = ?, updated_at = ? WHERE id = ?`,
		title, description, now, surveyID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM questions WHERE survey_id = ?`, surveyID); err != nil {
		return err
	}
	for i, p := range questions {
		cfgJSON, err := jsonMarshalConfig(p.Config)
		if err != nil {
			return err
		}
		req := 0
		if p.Required {
			req = 1
		}
		if _, err := tx.Exec(
			`INSERT INTO questions(survey_id, type, title, required, sort_order, config, created_at)
			 VALUES (?,?,?,?,?,?,?)`,
			surveyID, p.Type, p.Title, req, i+1, cfgJSON, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (st *SurveyStore) Publish(surveyID, userID int64, deadline string, maxResponses *int64) error {
	return st.transition(surveyID, userID, func(status int) error {
		if status == 1 {
			return nil // 已发布视为幂等成功，仅更新参数
		}
		if status != 0 && status != 2 {
			return ErrState
		}
		return nil
	}, `UPDATE surveys SET status = 1, deadline = ?, max_responses = ?, updated_at = ? WHERE id = ?`,
		deadline, maxResponses)
}

func (st *SurveyStore) Stop(surveyID, userID int64) error {
	return st.transition(surveyID, userID, func(status int) error {
		if status != 1 {
			return fmt.Errorf("%w：仅发布中的问卷可停止", ErrState)
		}
		return nil
	}, `UPDATE surveys SET status = 2, updated_at = ? WHERE id = ?`)
}

// transition 校验归属与状态后执行更新。
func (st *SurveyStore) transition(surveyID, userID int64, check func(status int) error,
	update string, args ...any) error {
	tx, err := st.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner int64
	var status int
	err = tx.QueryRow(
		`SELECT user_id, status FROM surveys WHERE id = ? AND deleted_at IS NULL`, surveyID,
	).Scan(&owner, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if owner != userID {
		return ErrForbidden
	}
	if err := check(status); err != nil {
		return err
	}
	// update 语句约定：自定义参数在前，updated_at 与 id 在后
	fullArgs := append(append([]any{}, args...), model.NowUTC(), surveyID)
	if _, err := tx.Exec(update, fullArgs...); err != nil {
		return err
	}
	return tx.Commit()
}

func (st *SurveyStore) SoftDelete(surveyID, userID int64) error {
	res, err := st.DB.Exec(
		`UPDATE surveys SET deleted_at = ?, updated_at = ? WHERE id = ? AND user_id = ? AND deleted_at IS NULL`,
		model.NowUTC(), model.NowUTC(), surveyID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Copy 复制为新草稿（含题目）。
func (st *SurveyStore) Copy(surveyID, userID int64) (*model.Survey, error) {
	src, err := st.GetOwned(surveyID, userID)
	if err != nil {
		return nil, err
	}
	qs, err := st.Questions(surveyID)
	if err != nil {
		return nil, err
	}
	now := model.NowUTC()
	title := src.Title + "（副本）"
	res, err := st.DB.Exec(
		`INSERT INTO surveys(user_id, title, description, status, created_at, updated_at) VALUES (?,?,?,0,?,?)`,
		userID, title, src.Description, now, now)
	if err != nil {
		return nil, err
	}
	newID, _ := res.LastInsertId()
	for i, q := range qs {
		cfgJSON, err := jsonMarshalConfig(q.Config)
		if err != nil {
			return nil, err
		}
		req := 0
		if q.Required {
			req = 1
		}
		if _, err := st.DB.Exec(
			`INSERT INTO questions(survey_id, type, title, required, sort_order, config, created_at)
			 VALUES (?,?,?,?,?,?,?)`, newID, q.Type, q.Title, req, i+1, cfgJSON, now); err != nil {
			return nil, err
		}
	}
	return st.Get(newID)
}

// SweepAutoStop 到达截止时间的发布中问卷自动置为已停止，返回处理行数。
func (st *SurveyStore) SweepAutoStop() (int64, error) {
	rows, err := st.DB.Query(
		`SELECT id, deadline FROM surveys WHERE status = 1 AND deadline IS NOT NULL AND deleted_at IS NULL`)
	if err != nil {
		return 0, err
	}
	type pair struct {
		id   int64
		dl   string
	}
	var expired []int64
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.id, &p.dl); err != nil {
			rows.Close()
			return 0, err
		}
		if dl, err := time.Parse(time.RFC3339Nano, p.dl); err == nil && time.Now().UTC().After(dl) {
			expired = append(expired, p.id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range expired {
		st.DB.Exec(`UPDATE surveys SET status = 2, updated_at = ? WHERE id = ? AND status = 1`, model.NowUTC(), id)
	}
	return int64(len(expired)), nil
}

func (st *SurveyStore) ResponseCount(surveyID int64) (int64, error) {
	var n int64
	err := st.DB.QueryRow(`SELECT COUNT(*) FROM responses WHERE survey_id = ?`, surveyID).Scan(&n)
	return n, err
}

// ---- 统计只读分享令牌 ----

// SetShareToken 生成/更新分享令牌；空串表示关闭分享。
func (st *SurveyStore) SetShareToken(surveyID, userID int64, token string) error {
	res, err := st.DB.Exec(
		`UPDATE surveys SET share_token = ?, updated_at = ? WHERE id = ? AND user_id = ? AND deleted_at IS NULL`,
		token, model.NowUTC(), surveyID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetByShareToken 按令牌取问卷（令牌非空才算开通）。
func (st *SurveyStore) GetByShareToken(token string) (*model.Survey, error) {
	return scanSurvey(st.DB.QueryRow(
		`SELECT `+surveyCols+` FROM surveys WHERE share_token = ? AND deleted_at IS NULL`, token))
}
