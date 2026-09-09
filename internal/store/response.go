package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"panda-survey/internal/model"
)

type ResponseStore struct{ DB *sql.DB }

var ErrSurveyClosed = errors.New("问卷已停止回收")

// Create 校验问卷可提交并在事务内写入答卷与答案；
// 事务内复查回收量上限，避免并发超出。profileJSON 为个人信息 JSON（答题卷），
// score 为判分得分（答题卷，普通问卷恒为 0）。
func (st *ResponseStore) Create(survey *model.Survey, answers []model.AnswerRow,
	profileJSON string, score int64, ip, ua string, duration int64) (int64, error) {
	tx, err := st.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var status int
	var deadline sql.NullString
	err = tx.QueryRow(
		`SELECT status, deadline FROM surveys WHERE id = ? AND deleted_at IS NULL`, survey.ID,
	).Scan(&status, &deadline)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if status != 1 {
		return 0, ErrSurveyClosed
	}
	if deadline.Valid {
		if dl, err := time.Parse(time.RFC3339Nano, deadline.String); err == nil && time.Now().UTC().After(dl) {
			tx.Exec(`UPDATE surveys SET status = 2, updated_at = ? WHERE id = ? AND status = 1`,
				model.NowUTC(), survey.ID)
			tx.Commit() // 自动停止需要落库，不能被 Rollback 吞掉
			return 0, ErrSurveyClosed
		}
	}
	var count int64
	if err := tx.QueryRow(`SELECT COUNT(*) FROM responses WHERE survey_id = ?`, survey.ID).Scan(&count); err != nil {
		return 0, err
	}
	if survey.MaxResponses != nil && count >= *survey.MaxResponses {
		tx.Exec(`UPDATE surveys SET status = 2, updated_at = ? WHERE id = ? AND status = 1`,
			model.NowUTC(), survey.ID)
		tx.Commit()
		return 0, ErrSurveyClosed
	}

	now := model.NowUTC()
	res, err := tx.Exec(
		`INSERT INTO responses(survey_id, ip, user_agent, duration, profile, score, created_at) VALUES (?,?,?,?,?,?,?)`,
		survey.ID, ip, ua, duration, profileJSON, score, now)
	if err != nil {
		return 0, err
	}
	rid, _ := res.LastInsertId()
	for _, a := range answers {
		if _, err := tx.Exec(
			`INSERT INTO answers(response_id, question_id, question_type, value, created_at) VALUES (?,?,?,?,?)`,
			rid, a.QuestionID, a.QuestionType, a.Value, now); err != nil {
			return 0, err
		}
	}
	return rid, tx.Commit()
}

// Delete 删除问卷下的一份答卷（答案级联删除）。
func (st *ResponseStore) Delete(surveyID, responseID int64) error {
	var one int
	err := st.DB.QueryRow(
		`SELECT 1 FROM responses WHERE id = ? AND survey_id = ?`, responseID, surveyID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := st.DB.Exec(`DELETE FROM answers WHERE response_id = ?`, responseID); err != nil {
		return err
	}
	_, err = st.DB.Exec(`DELETE FROM responses WHERE id = ?`, responseID)
	return err
}

func (st *ResponseStore) List(surveyID int64) ([]model.ResponseRow, error) {
	rows, err := st.DB.Query(
		`SELECT id, survey_id, ip, duration, profile, score, created_at FROM responses WHERE survey_id = ? ORDER BY id DESC`,
		surveyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ResponseRow
	for rows.Next() {
		var r model.ResponseRow
		if err := rows.Scan(&r.ID, &r.SurveyID, &r.IP, &r.Duration, &r.Profile, &r.Score, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Leaderboard 答题卷排行榜：得分降序、耗时升序、提交时间升序，取前 limit 名。
func (st *ResponseStore) Leaderboard(surveyID int64, limit int) ([]model.ResponseRow, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := st.DB.Query(`
		SELECT id, survey_id, ip, duration, profile, score, created_at
		FROM responses WHERE survey_id = ?
		ORDER BY score DESC, duration ASC, id ASC LIMIT ?`, surveyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ResponseRow
	for rows.Next() {
		var r model.ResponseRow
		if err := rows.Scan(&r.ID, &r.SurveyID, &r.IP, &r.Duration, &r.Profile, &r.Score, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AnswersOfSurvey 问卷下全部答案（导出与统计用），按提交顺序。
func (st *ResponseStore) AnswersOfSurvey(surveyID int64) ([]model.AnswerRow, error) {
	rows, err := st.DB.Query(`
		SELECT a.id, a.response_id, a.question_id, a.question_type, a.value, a.created_at
		FROM answers a JOIN responses r ON r.id = a.response_id
		WHERE r.survey_id = ?
		ORDER BY r.id, a.id`, surveyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AnswerRow
	for rows.Next() {
		var a model.AnswerRow
		if err := rows.Scan(&a.ID, &a.ResponseID, &a.QuestionID, &a.QuestionType, &a.Value, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ChoiceDistribution 某题各取值的计数（value 为 JSON 文本）。
func (st *ResponseStore) ChoiceDistribution(questionID int64) (map[string]int, int, error) {
	rows, err := st.DB.Query(
		`SELECT value, COUNT(*) FROM answers WHERE question_id = ? GROUP BY value ORDER BY COUNT(*) DESC`,
		questionID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	dist := map[string]int{}
	total := 0
	for rows.Next() {
		var v string
		var n int
		if err := rows.Scan(&v, &n); err != nil {
			return nil, 0, err
		}
		dist[v] = n
		total += n
	}
	return dist, total, rows.Err()
}

// RecentTexts 文本题最近的答案文本。
func (st *ResponseStore) RecentTexts(questionID int64, limit int) ([]string, error) {
	rows, err := st.DB.Query(
		`SELECT value FROM answers WHERE question_id = ? ORDER BY id DESC LIMIT ?`, questionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		var s string
		if err := json.Unmarshal([]byte(v), &s); err == nil {
			out = append(out, s)
		}
	}
	return out, rows.Err()
}

// QuestionValues 某题全部答案的 value JSON 文本（矩阵/排序等需整体解析的题型统计用）。
func (st *ResponseStore) QuestionValues(questionID int64) ([]string, error) {
	rows, err := st.DB.Query(
		`SELECT value FROM answers WHERE question_id = ? ORDER BY id`, questionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---- settings ----

type SettingsStore struct{ DB *sql.DB }

func (st *SettingsStore) Get(key string) (string, bool, error) {
	var v string
	err := st.DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (st *SettingsStore) Set(key, value string) error {
	_, err := st.DB.Exec(`
		INSERT INTO settings(key, value, updated_at) VALUES (?,?,?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, model.NowUTC())
	return err
}

// ---- AI 用量 ----

type AIUsageStore struct{ DB *sql.DB }

func (st *AIUsageStore) Incr(userID int64, action string, surveyID int64) error {
	_, err := st.DB.Exec(
		`INSERT INTO ai_usage(user_id, action, survey_id, created_at) VALUES (?,?,?,?)`,
		userID, action, surveyID, model.NowUTC())
	return err
}

func (st *AIUsageStore) CountToday(userID int64) (int64, error) {
	dayStart := time.Now().UTC().Truncate(24 * time.Hour).Format(time.RFC3339Nano)
	var n int64
	err := st.DB.QueryRow(
		`SELECT COUNT(*) FROM ai_usage WHERE user_id = ? AND created_at >= ?`, userID, dayStart).Scan(&n)
	return n, err
}
