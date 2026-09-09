package store

import (
	"database/sql"
	"errors"
	"fmt"

	"panda-survey/internal/model"
)

// BankStore 题库仓储：题库与库内题目的增删改查。
type BankStore struct{ DB *sql.DB }

// List 用户题库列表（含题量）。
func (st *BankStore) List(userID int64) ([]model.QuestionBank, error) {
	rows, err := st.DB.Query(`
		SELECT b.id, b.user_id, b.name, b.created_at, b.updated_at,
		       (SELECT COUNT(*) FROM bank_questions q WHERE q.bank_id = b.id) AS qc
		FROM question_banks b
		WHERE b.user_id = ? AND b.deleted_at IS NULL
		ORDER BY b.updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.QuestionBank
	for rows.Next() {
		var b model.QuestionBank
		if err := rows.Scan(&b.ID, &b.UserID, &b.Name, &b.CreatedAt, &b.UpdatedAt, &b.QuestionCount); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (st *BankStore) Get(id int64) (*model.QuestionBank, error) {
	var b model.QuestionBank
	err := st.DB.QueryRow(`
		SELECT id, user_id, name, created_at, updated_at,
		       (SELECT COUNT(*) FROM bank_questions q WHERE q.bank_id = question_banks.id) AS qc
		FROM question_banks WHERE id = ? AND deleted_at IS NULL`, id,
	).Scan(&b.ID, &b.UserID, &b.Name, &b.CreatedAt, &b.UpdatedAt, &b.QuestionCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// GetOwned 取题库并校验归属。
func (st *BankStore) GetOwned(id, userID int64) (*model.QuestionBank, error) {
	b, err := st.Get(id)
	if err != nil {
		return nil, err
	}
	if b.UserID != userID {
		return nil, ErrForbidden
	}
	return b, nil
}

func (st *BankStore) Create(userID int64, name string) (*model.QuestionBank, error) {
	now := model.NowUTC()
	res, err := st.DB.Exec(
		`INSERT INTO question_banks(user_id, name, created_at, updated_at) VALUES (?,?,?,?)`,
		userID, name, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return st.Get(id)
}

// Rename 校验归属后重命名。
func (st *BankStore) Rename(id, userID int64, name string) error {
	res, err := st.DB.Exec(
		`UPDATE question_banks SET name = ?, updated_at = ? WHERE id = ? AND user_id = ? AND deleted_at IS NULL`,
		name, model.NowUTC(), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (st *BankStore) SoftDelete(id, userID int64) error {
	res, err := st.DB.Exec(
		`UPDATE question_banks SET deleted_at = ?, updated_at = ? WHERE id = ? AND user_id = ? AND deleted_at IS NULL`,
		model.NowUTC(), model.NowUTC(), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Questions 题库内全部题目，按 sort_order。
func (st *BankStore) Questions(bankID int64) ([]model.BankQuestion, error) {
	rows, err := st.DB.Query(`
		SELECT id, bank_id, type, title, required, sort_order, config, created_at
		FROM bank_questions WHERE bank_id = ? ORDER BY sort_order, id`, bankID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.BankQuestion
	for rows.Next() {
		var q model.BankQuestion
		var cfg string
		var req int
		if err := rows.Scan(&q.ID, &q.BankID, &q.Type, &q.Title, &req, &q.SortOrder, &cfg, &q.CreatedAt); err != nil {
			return nil, err
		}
		q.Required = req == 1
		if err := jsonUnmarshalConfig(cfg, &q.Config); err != nil {
			return nil, fmt.Errorf("题库题目 %d config 损坏: %w", q.ID, err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// AddQuestion 追加题库题目，sort_order 取当前最大值 +1。
func (st *BankStore) AddQuestion(bankID int64, p model.BankQuestionPayload) (int64, error) {
	tx, err := st.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var maxOrder sql.NullInt64
	if err := tx.QueryRow(
		`SELECT MAX(sort_order) FROM bank_questions WHERE bank_id = ?`, bankID).Scan(&maxOrder); err != nil {
		return 0, err
	}
	cfgJSON, err := jsonMarshalConfig(p.Config)
	if err != nil {
		return 0, err
	}
	now := model.NowUTC()
	res, err := tx.Exec(
		`INSERT INTO bank_questions(bank_id, type, title, required, sort_order, config, created_at)
		 VALUES (?,?,?,1,?,?,?)`,
		bankID, p.Type, p.Title, maxOrder.Int64+1, cfgJSON, now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.Exec(`UPDATE question_banks SET updated_at = ? WHERE id = ?`, now, bankID); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// UpdateQuestion 原地更新题库题目（保留 id）。
func (st *BankStore) UpdateQuestion(bankID, questionID int64, p model.BankQuestionPayload) error {
	cfgJSON, err := jsonMarshalConfig(p.Config)
	if err != nil {
		return err
	}
	now := model.NowUTC()
	res, err := st.DB.Exec(
		`UPDATE bank_questions SET type = ?, title = ?, config = ? WHERE id = ? AND bank_id = ?`,
		p.Type, p.Title, cfgJSON, questionID, bankID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = st.DB.Exec(`UPDATE question_banks SET updated_at = ? WHERE id = ?`, now, bankID)
	return err
}

func (st *BankStore) DeleteQuestion(bankID, questionID int64) error {
	res, err := st.DB.Exec(`DELETE FROM bank_questions WHERE id = ? AND bank_id = ?`, questionID, bankID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
