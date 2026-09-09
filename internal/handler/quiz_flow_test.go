package handler

import (
	"fmt"
	"testing"
)

// quizQuestions 一组答题卷题目：单选 2 分(o1 对)、多选 3 分(o1,o2 对)、填空 5 分(北京/beijing)
func quizQuestions() []map[string]any {
	return []map[string]any{
		{"type": "single_choice", "title": "中国的首都是？", "required": false,
			"config": map[string]any{
				"options": []map[string]any{{"id": "o1", "label": "北京"}, {"id": "o2", "label": "上海"}},
				"score":   2, "correct": "o1"}},
		{"type": "multiple_choice", "title": "以下哪些是水果？", "required": false,
			"config": map[string]any{
				"options": []map[string]any{{"id": "o1", "label": "苹果"}, {"id": "o2", "label": "香蕉"}, {"id": "o3", "label": "土豆"}},
				"score":   3, "correct": []string{"o1", "o2"}}},
		{"type": "text", "title": "写出首都", "required": false,
			"config": map[string]any{"max_len": 100, "score": 5, "correct": []string{"北京", "beijing"}}},
	}
}

func quizConfig(overrides map[string]any) map[string]any {
	cfg := map[string]any{
		"duration_min":    0,
		"show_answer":     true,
		"display_mode":    "paged",
		"question_order":  "sequential",
		"collect_profile": true,
		"profile_fields": []map[string]any{
			{"label": "姓名", "required": true},
			{"label": "手机号", "required": false},
		},
		"show_ranking": false,
	}
	for k, v := range overrides {
		cfg[k] = v
	}
	return cfg
}

// TestQuizFullFlow 答题卷全流程：题库 -> 建卷 -> 判分 -> 排行榜 -> 统计
func TestQuizFullFlow(t *testing.T) {
	e := newTestEnv(t)
	admin := registerAndLogin(e, "quizadmin")

	// ---- 题库 ----
	st, m := e.do(admin, "POST", "/api/banks", map[string]string{"name": "常识题库"})
	if st != 200 || code(m) != 0 {
		t.Fatalf("创建题库失败: %v", m)
	}
	bankID := fmt.Sprintf("%.0f", dataMap(m)["id"].(float64))

	st, m = e.do(admin, "POST", "/api/banks/"+bankID+"/questions", map[string]any{
		"type": "single_choice", "title": "1+1=？",
		"config": map[string]any{
			"options": []map[string]any{{"id": "o1", "label": "2"}, {"id": "o2", "label": "3"}},
			"score":   10, "correct": "o1"},
	})
	if st != 200 || code(m) != 0 {
		t.Fatalf("题库加题失败: %v", m)
	}
	// 非法题目：无正确答案
	st, m = e.do(admin, "POST", "/api/banks/"+bankID+"/questions", map[string]any{
		"type": "single_choice", "title": "无答案题",
		"config": map[string]any{"options": []map[string]any{{"id": "o1", "label": "A"}, {"id": "o2", "label": "B"}}},
	})
	if st != 400 {
		t.Fatalf("无正确答案的题库题目应 400: %v", m)
	}
	// 非答题题型被拒
	st, m = e.do(admin, "POST", "/api/banks/"+bankID+"/questions", map[string]any{
		"type": "rating", "title": "评分",
		"config": map[string]any{"max": 5, "score": 1, "correct": "3"},
	})
	if st != 400 {
		t.Fatalf("答题库不允许评分题: %v", m)
	}
	st, m = e.do(admin, "GET", "/api/banks/"+bankID+"/questions", nil)
	if st != 200 {
		t.Fatalf("题库题目列表失败: %v", m)
	}
	if len(dataMap(m)["questions"].([]any)) != 1 {
		t.Fatalf("题库应有 1 题: %v", m)
	}
	// 编辑库内题目
	bqid := fmt.Sprintf("%.0f", dataMap(m)["questions"].([]any)[0].(map[string]any)["id"].(float64))
	st, m = e.do(admin, "PUT", "/api/banks/"+bankID+"/questions/"+bqid, map[string]any{
		"type": "single_choice", "title": "1+1=？（改）",
		"config": map[string]any{
			"options": []map[string]any{{"id": "o1", "label": "2"}, {"id": "o2", "label": "3"}},
			"score":   10, "correct": "o2"},
	})
	if st != 200 {
		t.Fatalf("编辑题库题目失败: %v", m)
	}
	// 越权：他人题库
	other := registerAndLogin(e, "quizother")
	st, m = e.do(other, "GET", "/api/banks/"+bankID+"/questions", nil)
	if st != 403 {
		t.Fatalf("越权访问题库应 403: %v", m)
	}

	// ---- 创建答题卷 ----
	st, m = e.do(admin, "POST", "/api/surveys", map[string]any{
		"title": "常识测验", "description": "测一测", "kind": 1,
	})
	if st != 200 || code(m) != 0 {
		t.Fatalf("创建答题卷失败: %v", m)
	}
	quiz := dataMap(m)
	if quiz["kind"].(float64) != 1 {
		t.Fatalf("kind 应为 1: %v", quiz)
	}
	sid := fmt.Sprintf("%.0f", quiz["id"].(float64))

	st, m = e.do(admin, "PUT", "/api/surveys/"+sid, map[string]any{
		"title": "常识测验", "description": "测一测",
		"updated_at":  quiz["updated_at"].(string),
		"questions":   quizQuestions(),
		"quiz_config": quizConfig(nil),
	})
	if st != 200 || code(m) != 0 {
		t.Fatalf("保存答题卷失败: %v", m)
	}

	// 反向：答题卷不允许非可判分题型 / 缺少正确答案
	st, m = e.do(admin, "POST", "/api/surveys", map[string]any{"title": "坏卷", "kind": 1})
	badID := fmt.Sprintf("%.0f", dataMap(m)["id"].(float64))
	// 400 响应不带 data，先取 updated_at 作为乐观锁版本
	st, m = e.do(admin, "GET", "/api/surveys/"+badID, nil)
	badUpdatedAt := dataMap(m)["survey"].(map[string]any)["updated_at"].(string)
	st, m = e.do(admin, "PUT", "/api/surveys/"+badID, map[string]any{
		"title": "坏卷", "updated_at": badUpdatedAt,
		"questions": []map[string]any{
			{"type": "textarea", "title": "简答", "config": map[string]any{"max_len": 100, "score": 1, "correct": []string{"x"}}},
		},
		"quiz_config": quizConfig(nil),
	})
	if st != 400 {
		t.Fatalf("答题卷含简答题应被拒: %v", m)
	}
	st, m = e.do(admin, "PUT", "/api/surveys/"+badID, map[string]any{
		"title": "坏卷", "updated_at": badUpdatedAt,
		"questions": []map[string]any{
			{"type": "single_choice", "title": "无答案", "config": map[string]any{
				"options": []map[string]any{{"id": "o1", "label": "A"}, {"id": "o2", "label": "B"}}, "score": 1}},
		},
		"quiz_config": quizConfig(nil),
	})
	if st != 400 {
		t.Fatalf("缺少正确答案应被拒: %v", m)
	}
	// 反向：展示方式非法（list/paged 二选一）
	st, m = e.do(admin, "PUT", "/api/surveys/"+badID, map[string]any{
		"title": "坏卷", "updated_at": badUpdatedAt,
		"questions":   quizQuestions(),
		"quiz_config": quizConfig(map[string]any{"display_mode": "both"}),
	})
	if st != 400 {
		t.Fatalf("非法展示方式应被拒: %v", m)
	}

	// ---- 发布 ----
	st, m = e.do(admin, "POST", "/api/surveys/"+sid+"/publish", map[string]any{})
	if st != 200 || dataMap(m)["status"].(float64) != 1 {
		t.Fatalf("发布答题卷失败: %v", m)
	}

	// ---- 公开视图：不含正确答案、含 quiz_config ----
	anon := e.newClient()
	st, m = e.do(anon, "GET", "/api/surveys/"+sid+"/public", nil)
	if st != 200 {
		t.Fatalf("公开视图失败: %v", m)
	}
	pub := dataMap(m)
	pubCfg := pub["survey"].(map[string]any)["quiz_config"].(map[string]any)
	if pubCfg["collect_profile"] != true {
		t.Fatalf("公开视图应带 quiz_config: %v", pub)
	}
	pubQs := pub["questions"].([]any)
	if len(pubQs) != 3 {
		t.Fatalf("公开视图题目数不符: %d", len(pubQs))
	}
	for _, q := range pubQs {
		cfg := q.(map[string]any)["config"].(map[string]any)
		if _, leak := cfg["correct"]; leak {
			t.Fatalf("正确答案不应出现在公开视图: %v", cfg)
		}
	}

	// ---- 逐题提交判分（展示答案模式）：每题判分并回传正确答案，不落库 ----
	gradeQuiz := func(q float64, value any) (int, map[string]any) {
		return e.do(anon, "POST", "/api/surveys/"+sid+"/grade-question", map[string]any{
			"question_id": q, "value": value,
		})
	}
	// 第 1 题答对：得 2 分并回传正确答案
	st, m = gradeQuiz(qid(pubQs, 1), "o1")
	if st != 200 || code(m) != 0 {
		t.Fatalf("逐题提交失败: %v", m)
	}
	gd := dataMap(m)
	if gd["correct"] != true || gd["awarded"].(float64) != 2 {
		t.Fatalf("第 1 题应判对且得 2 分: %v", gd)
	}
	if gd["correct_data"] != "o1" {
		t.Fatalf("应回传正确答案 o1: %v", gd)
	}
	// 第 2 题多选缺项 -> 错 0 分
	st, m = gradeQuiz(qid(pubQs, 2), []string{"o1"})
	if st != 200 || dataMap(m)["correct"] != false || dataMap(m)["awarded"].(float64) != 0 {
		t.Fatalf("多选缺项应判错 0 分: %v", m)
	}
	// 第 3 题填空忽略大小写与空格 -> 对
	st, m = gradeQuiz(qid(pubQs, 3), "  BEIJING  ")
	if st != 200 || dataMap(m)["correct"] != true {
		t.Fatalf("填空应判对: %v", m)
	}
	// 非法选项 -> 400
	st, m = gradeQuiz(qid(pubQs, 1), "oX")
	if st != 400 {
		t.Fatalf("非法选项应 400: %v", m)
	}
	// 不属于该卷的题目 -> 400
	st, m = gradeQuiz(999999, "o1")
	if st != 400 {
		t.Fatalf("不属于该卷的题目应 400: %v", m)
	}
	// 逐题提交不落库：答卷列表仍为空
	st, m = e.do(admin, "GET", "/api/surveys/"+sid+"/responses", nil)
	if st != 200 {
		t.Fatalf("答卷列表失败: %v", m)
	}
	if arr, ok := m["data"].([]any); !ok || len(arr) != 0 {
		t.Fatalf("逐题提交不应落库: %v", m)
	}

	// ---- 提交判分 ----
	submitQuiz := func(answers []map[string]any, profile []map[string]any) (int, map[string]any) {
		return e.do(anon, "POST", "/api/surveys/"+sid+"/responses", map[string]any{
			"answers": answers, "profile": profile, "duration": 60,
		})
	}
	// 全对：2+3+5=10 分
	st, m = submitQuiz([]map[string]any{
		{"question_id": qid(pubQs, 1), "value": "o1"},
		{"question_id": qid(pubQs, 2), "value": []string{"o1", "o2"}},
		{"question_id": qid(pubQs, 3), "value": "beijing"},
	}, []map[string]any{{"label": "姓名", "value": "小明"}, {"label": "手机号", "value": "13800000000"}})
	if st != 200 || code(m) != 0 {
		t.Fatalf("提交失败: %v", m)
	}
	rd := dataMap(m)
	if rd["score"].(float64) != 10 || rd["total"].(float64) != 10 {
		t.Fatalf("全对应得 10/10: %v", rd)
	}
	results := rd["results"].([]any)
	if len(results) != 3 {
		t.Fatalf("show_answer 应返回逐题结果: %v", rd)
	}
	if results[0].(map[string]any)["correct"] != true {
		t.Fatalf("第 1 题应对: %v", results[0])
	}

	// 部分对 + 未答：多选错、填空未答 -> 2 分
	st, m = submitQuiz([]map[string]any{
		{"question_id": qid(pubQs, 1), "value": "o1"},
		{"question_id": qid(pubQs, 2), "value": []string{"o1", "o3"}},
	}, []map[string]any{{"label": "姓名", "value": "小红"}})
	if st != 200 {
		t.Fatalf("部分对提交失败: %v", m)
	}
	if dataMap(m)["score"].(float64) != 2 {
		t.Fatalf("部分对应得 2 分: %v", dataMap(m))
	}

	// 必填个人信息缺失 -> 报错
	st, m = submitQuiz([]map[string]any{{"question_id": qid(pubQs, 1), "value": "o1"}}, nil)
	if st != 400 {
		t.Fatalf("缺必填个人信息应 400: %v", m)
	}

	// 非法选项 -> 报错
	st, m = submitQuiz([]map[string]any{{"question_id": qid(pubQs, 1), "value": "oX"}},
		[]map[string]any{{"label": "姓名", "value": "张三"}})
	if st != 400 {
		t.Fatalf("非法选项应 400: %v", m)
	}

	// ---- 排行榜：默认关闭 ----
	st, m = e.do(anon, "GET", "/api/surveys/"+sid+"/leaderboard", nil)
	if st != 404 {
		t.Fatalf("未开启排行榜应 404: %v", m)
	}

	// ---- 开启排行榜 + 关闭展示答案 ----
	st, m = e.do(admin, "GET", "/api/surveys/"+sid, nil)
	cur := dataMap(m)
	curSurvey := cur["survey"].(map[string]any)
	cfg := quizConfig(map[string]any{"show_ranking": true, "show_answer": false})
	st, m = e.do(admin, "PUT", "/api/surveys/"+sid, map[string]any{
		"title": "常识测验", "description": "测一测",
		"updated_at":  curSurvey["updated_at"].(string),
		"questions":   cur["questions"],
		"quiz_config": cfg,
	})
	if st != 200 {
		t.Fatalf("更新答题配置失败: %v", m)
	}
	// 提交后 results 为空
	st, m = submitQuiz([]map[string]any{{"question_id": qid(pubQs, 1), "value": "o1"}},
		[]map[string]any{{"label": "姓名", "value": "小刚"}})
	if st != 200 {
		t.Fatalf("提交失败: %v", m)
	}
	if rd := dataMap(m); rd["results"] != nil {
		t.Fatalf("show_answer=false 不应返回逐题结果: %v", rd)
	}
	// 关闭展示答案后逐题提交应 404
	st, m = e.do(anon, "POST", "/api/surveys/"+sid+"/grade-question", map[string]any{
		"question_id": qid(pubQs, 1), "value": "o1",
	})
	if st != 404 {
		t.Fatalf("show_answer=false 逐题提交应 404: %v", m)
	}

	// 排行榜：得分降序，姓名取个人信息
	st, m = e.do(anon, "GET", "/api/surveys/"+sid+"/leaderboard", nil)
	if st != 200 {
		t.Fatalf("排行榜失败: %v", m)
	}
	entries := dataMap(m)["entries"].([]any)
	if len(entries) != 3 {
		t.Fatalf("应有 3 条排行: %v", m)
	}
	first := entries[0].(map[string]any)
	if first["score"].(float64) != 10 || first["name"] != "小明" {
		t.Fatalf("榜首应为小明 10 分: %v", first)
	}

	// ---- 统计：平均分 = (10+2+2)/3 = 4.7 ----
	st, m = e.do(admin, "GET", "/api/surveys/"+sid+"/stats", nil)
	if st != 200 {
		t.Fatalf("统计失败: %v", m)
	}
	sd := dataMap(m)
	if sd["total"].(float64) != 3 {
		t.Fatalf("回收量应为 3: %v", sd)
	}
	if sd["avg_score"].(float64) != 4.7 {
		t.Fatalf("平均分应为 4.7: %v", sd)
	}
	// 明细包含得分（列表按 id 倒序，最新在前）
	st, m = e.do(admin, "GET", "/api/surveys/"+sid+"/responses", nil)
	if st != 200 {
		t.Fatalf("明细失败: %v", m)
	}
	items := m["data"].([]any)
	if len(items) != 3 {
		t.Fatalf("明细应有 3 条: %v", m)
	}
	oldest := items[2].(map[string]any)
	if oldest["score"].(float64) != 10 {
		t.Fatalf("明细应含得分: %v", oldest)
	}
	if oldest["profile"].(string) == "" {
		t.Fatalf("明细应含个人信息: %v", oldest)
	}

	// ---- 答题卷复制 ----
	st, m = e.do(admin, "POST", "/api/surveys/"+sid+"/copy", map[string]any{})
	if st != 200 {
		t.Fatalf("复制答题卷失败: %v", m)
	}
	if dataMap(m)["kind"].(float64) != 1 {
		t.Fatalf("副本 kind 应为 1: %v", dataMap(m))
	}
}
