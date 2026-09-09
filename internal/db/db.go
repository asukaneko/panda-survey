package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// migration 按序执行，schema_version 表记录已执行到的版本。
var migrations = []string{
	// v1：初始全量建表
	`
CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          INTEGER NOT NULL DEFAULT 0,
    status        INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL
);
CREATE TABLE sessions (
    token      TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE surveys (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users(id),
    title         TEXT NOT NULL,
    description   TEXT DEFAULT '',
    status        INTEGER NOT NULL DEFAULT 0,
    deadline      TEXT,
    max_responses INTEGER,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    deleted_at    TEXT
);
CREATE TABLE questions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    survey_id   INTEGER NOT NULL REFERENCES surveys(id) ON DELETE CASCADE,
    type        TEXT NOT NULL,
    title       TEXT NOT NULL,
    required    INTEGER NOT NULL DEFAULT 0,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    config      TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL
);
CREATE TABLE responses (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    survey_id   INTEGER NOT NULL REFERENCES surveys(id) ON DELETE CASCADE,
    ip          TEXT DEFAULT '',
    user_agent  TEXT DEFAULT '',
    duration    INTEGER DEFAULT 0,
    created_at  TEXT NOT NULL
);
CREATE TABLE answers (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    response_id    INTEGER NOT NULL REFERENCES responses(id) ON DELETE CASCADE,
    question_id    INTEGER NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    question_type  TEXT NOT NULL,
    value          TEXT NOT NULL,
    created_at     TEXT NOT NULL
);
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE ai_usage (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id),
    action     TEXT NOT NULL,
    survey_id  INTEGER,
    created_at TEXT NOT NULL
);
CREATE INDEX idx_surveys_user     ON surveys(user_id, status);
CREATE INDEX idx_questions_survey ON questions(survey_id, sort_order);
CREATE INDEX idx_responses_survey ON responses(survey_id, created_at);
CREATE INDEX idx_answers_response ON answers(response_id);
CREATE INDEX idx_answers_question ON answers(question_id, question_type);
CREATE INDEX idx_ai_usage_user    ON ai_usage(user_id, created_at);
`,
	// v2：统计只读分享令牌
	`ALTER TABLE surveys ADD COLUMN share_token TEXT`,
	// v3：答题类型（kind）、答题配置、答卷得分/个人信息、题库
	`
ALTER TABLE surveys ADD COLUMN kind INTEGER NOT NULL DEFAULT 0;
ALTER TABLE surveys ADD COLUMN quiz_config TEXT NOT NULL DEFAULT '';
ALTER TABLE responses ADD COLUMN profile TEXT NOT NULL DEFAULT '';
ALTER TABLE responses ADD COLUMN score INTEGER NOT NULL DEFAULT 0;
CREATE TABLE question_banks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    deleted_at TEXT
);
CREATE TABLE bank_questions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    bank_id     INTEGER NOT NULL REFERENCES question_banks(id) ON DELETE CASCADE,
    type        TEXT NOT NULL,
    title       TEXT NOT NULL,
    required    INTEGER NOT NULL DEFAULT 1,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    config      TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL
);
CREATE INDEX idx_banks_user         ON question_banks(user_id);
CREATE INDEX idx_bank_questions_bank ON bank_questions(bank_id, sort_order);
`,
}

// Open 打开 SQLite 并执行迁移。时间统一由应用层写入 UTC RFC3339 文本。
func Open(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// 单连接串行化写，规避 SQLITE_BUSY；私有部署量级下足够
	d.SetMaxOpenConns(1)
	d.SetMaxIdleConns(1)
	if err := migrate(d); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func migrate(d *sql.DB) error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}
	var cur int
	if err := d.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&cur); err != nil {
		return fmt.Errorf("read schema_version: %w", err)
	}
	for i, m := range migrations {
		ver := i + 1
		if ver <= cur {
			continue
		}
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", ver, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_version(version) VALUES (?)`, ver); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d record: %w", ver, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
