package store

import (
		"database/sql"
		"errors"
		"fmt"
	
		"panda-survey/internal/model"
	)

// AdminStore 管理员后台：用户与全站问卷管理。
type AdminStore struct{ DB *sql.DB }

type AdminUser struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	Role        int    `json:"role"`
	Status      int    `json:"status"` // 0 正常 1 封禁
	CreatedAt   string `json:"created_at"`
	SurveyCount int64  `json:"survey_count"`
}

type AdminSurvey struct {
	ID            int64  `json:"id"`
	Username      string `json:"username"`
	Title         string `json:"title"`
	Status        int    `json:"status"`
	ResponseCount int64  `json:"response_count"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// Users 全部用户及各自问卷数。
func (st *AdminStore) Users() ([]AdminUser, error) {
	rows, err := st.DB.Query(`
		SELECT u.id, u.username, u.role, u.status, u.created_at,
		       (SELECT COUNT(*) FROM surveys s
		        WHERE s.user_id = u.id AND s.deleted_at IS NULL) AS sc
		FROM users u ORDER BY u.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminUser
	for rows.Next() {
		var u AdminUser
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.Status, &u.CreatedAt, &u.SurveyCount); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetUserStatus 封禁/解封；封禁时吊销该用户全部会话。
func (st *AdminStore) SetUserStatus(userID int64, status int) error {
	res, err := st.DB.Exec(`UPDATE users SET status = ? WHERE id = ?`, status, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if status != 0 {
		st.DB.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID)
	}
	return nil
}

// Surveys 全站问卷（含所有者与回收量）。
func (st *AdminStore) Surveys() ([]AdminSurvey, error) {
	rows, err := st.DB.Query(`
		SELECT s.id, u.username, s.title, s.status, s.created_at, s.updated_at,
		       (SELECT COUNT(*) FROM responses r WHERE r.survey_id = s.id) AS rc
		FROM surveys s JOIN users u ON u.id = s.user_id
		WHERE s.deleted_at IS NULL
		ORDER BY s.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminSurvey
	for rows.Next() {
		var s AdminSurvey
		if err := rows.Scan(&s.ID, &s.Username, &s.Title, &s.Status, &s.CreatedAt, &s.UpdatedAt, &s.ResponseCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ForceStop 管理员强制下架任意问卷。
func (st *AdminStore) ForceStop(surveyID int64) error {
	res, err := st.DB.Exec(
		`UPDATE surveys SET status = 2, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		model.NowUTC(), surveyID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ForceDelete 管理员强制软删除任意问卷。
func (st *AdminStore) ForceDelete(surveyID int64) error {
	res, err := st.DB.Exec(
		`UPDATE surveys SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		model.NowUTC(), model.NowUTC(), surveyID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUser 永久删除用户及其全部问卷（级联清空题目/答卷/答案）、会话与 AI 用量。
// surveys.user_id 无级联约束，须先删问卷再删用户。
func (st *AdminStore) DeleteUser(userID int64) error {
	tx, err := st.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 校验用户存在
	var one int
	if err := tx.QueryRow(`SELECT 1 FROM users WHERE id = ?`, userID).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	// 删除用户问卷：questions/responses 级联，answers 级联自两者
	if _, err := tx.Exec(`DELETE FROM surveys WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM ai_usage WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM users WHERE id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// Overview 全站用量概览。
func (st *AdminStore) Overview() (map[string]int64, error) {
	out := map[string]int64{}
	counts := []struct {
		key, query string
	}{
		{"users", `SELECT COUNT(*) FROM users`},
		{"surveys", `SELECT COUNT(*) FROM surveys WHERE deleted_at IS NULL`},
		{"published", `SELECT COUNT(*) FROM surveys WHERE status = 1 AND deleted_at IS NULL`},
		{"responses", `SELECT COUNT(*) FROM responses`},
		{"ai_calls", `SELECT COUNT(*) FROM ai_usage`},
	}
	for _, c := range counts {
		var n int64
		if err := st.DB.QueryRow(c.query).Scan(&n); err != nil {
			return nil, fmt.Errorf("统计 %s: %w", c.key, err)
		}
		out[c.key] = n
	}
	return out, nil
}
