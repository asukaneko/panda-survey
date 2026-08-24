package store

import (
	"testing"

	"panda-survey/internal/db"
)

// TestAdminDeleteUser 验证 DeleteUser 级联清空用户的问卷、题目、答卷、答案、会话与 AI 用量。
func TestAdminDeleteUser(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	// 用户 A（会被删除）与用户 B（不受影响）
	d.Exec(`INSERT INTO users(id,username,password_hash,role,created_at) VALUES (1,'delme','x',0,'t')`)
	d.Exec(`INSERT INTO users(id,username,password_hash,role,created_at) VALUES (2,'keep','x',0,'t')`)
	// A 的问卷 + 题目 + 答卷 + 答案 + 会话 + AI 用量
	d.Exec(`INSERT INTO surveys(id,user_id,title,status,created_at,updated_at) VALUES (10,1,'A问卷',1,'t','t')`)
	d.Exec(`INSERT INTO questions(id,survey_id,type,title,sort_order,config,created_at) VALUES (100,10,'text','q1',1,'{}','t')`)
	d.Exec(`INSERT INTO responses(id,survey_id,ip,created_at) VALUES (1000,10,'','t')`)
	d.Exec(`INSERT INTO answers(id,response_id,question_id,question_type,value,created_at) VALUES (1,1000,100,'text','"hi"','t')`)
	d.Exec(`INSERT INTO sessions(token,user_id,expires_at,created_at) VALUES ('tok1',1,'t','t')`)
	d.Exec(`INSERT INTO ai_usage(id,user_id,action,created_at) VALUES (1,1,'gen','t')`)
	// B 的问卷 + 答案（不受影响）
	d.Exec(`INSERT INTO surveys(id,user_id,title,status,created_at,updated_at) VALUES (11,2,'B问卷',1,'t','t')`)
	d.Exec(`INSERT INTO questions(id,survey_id,type,title,sort_order,config,created_at) VALUES (101,11,'text','q1',1,'{}','t')`)
	d.Exec(`INSERT INTO responses(id,survey_id,ip,created_at) VALUES (1001,11,'','t')`)
	d.Exec(`INSERT INTO answers(id,response_id,question_id,question_type,value,created_at) VALUES (2,1001,101,'text','"keep"','t')`)

	ad := &AdminStore{DB: d}
	if err := ad.DeleteUser(1); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	// 断言：A 的表全部归零，B 的表不受影响
	check := func(tbl, cond string, want int) {
		t.Helper()
		var n int
		if err := d.QueryRow(`SELECT COUNT(*) FROM `+tbl+` WHERE `+cond).Scan(&n); err != nil {
			t.Fatalf("count %s WHERE %s: %v", tbl, cond, err)
		}
		if n != want {
			t.Errorf("%s WHERE %s 应有 %d 行，实际 %d", tbl, cond, want, n)
		}
	}
	check("users", "id=1", 0)
	check("users", "id=2", 1)
	check("surveys", "user_id=1", 0)
	check("surveys", "user_id=2", 1)
	check("questions", "survey_id=10", 0)
	check("questions", "survey_id=11", 1)
	check("responses", "survey_id=10", 0)
	check("responses", "survey_id=11", 1)
	check("answers", "id=1", 0)
	check("answers", "id=2", 1)
	check("sessions", "user_id=1", 0)
	check("ai_usage", "user_id=1", 0)
}