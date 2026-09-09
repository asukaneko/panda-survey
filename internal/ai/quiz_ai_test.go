package ai

import (
	"context"
	"testing"

	"panda-survey/internal/model"
)

// TestGenerateQuizValid AI 生成答题卷：答案/分值校验、选项 id 补齐、quiz_config 默认值。
func TestGenerateQuizValid(t *testing.T) {
	gen := `{"title":"Java 基础测验","description":"测一测","quiz_config":{"duration_min":10,"show_answer":true,"display_mode":"paged","question_order":"random"},
	"questions":[
		{"type":"single_choice","title":"Java 中 int 占多少位？","config":{"options":[{"label":"16"},{"label":"32"},{"label":"64"}],"score":5,"correct":"o2"}},
		{"type":"multiple_choice","title":"以下哪些是 Java 关键字？","config":{"options":[{"label":"class"},{"label":"int"},{"label":"var"}],"score":10,"correct":["o1","o2"]}},
		{"type":"text","title":"写出 System 输出的方法名","config":{"max_len":50,"score":5,"correct":["println","out.println"]}}
	]}`
	f := newFakeAI(t, contentMsg("```json\n"+gen+"\n```"))
	got, err := GenerateQuiz(context.Background(), f.client(), "Java 基础测验，10 分钟限时")
	if err != nil {
		t.Fatalf("生成答题卷失败: %v", err)
	}
	if got.Title != "Java 基础测验" {
		t.Fatalf("标题不符: %s", got.Title)
	}
	if len(got.Questions) != 3 {
		t.Fatalf("题目数不符: %d", len(got.Questions))
	}
	// 选项 id 应补齐，correct 随重排映射到正确选项
	q1 := got.Questions[0]
	if q1.Config.Options[1].ID != "o2" || q1.Config.Correct != "o2" {
		t.Fatalf("单选 correct 映射错误: %+v", q1.Config)
	}
	if q1.Config.Score != 5 {
		t.Fatalf("分值不符: %d", q1.Config.Score)
	}
	// 多选 correct 为 id 数组
	q2 := got.Questions[1]
	ids, ok := q2.Config.Correct.([]any)
	if !ok || len(ids) != 2 || ids[0] != "o1" || ids[1] != "o2" {
		t.Fatalf("多选 correct 不符: %+v", q2.Config.Correct)
	}
	// 答题配置归一化
	if got.QuizConfig.DurationMin != 10 || got.QuizConfig.DisplayMode != "paged" ||
		got.QuizConfig.QuestionOrder != "random" || !got.QuizConfig.ShowAnswer {
		t.Fatalf("quiz_config 不符: %+v", got.QuizConfig)
	}
}

// TestGenerateQuizRejectsBadType AI 生成答题卷不允许非可判分题型。
func TestGenerateQuizRejectsBadType(t *testing.T) {
	bad := `{"title":"坏卷","questions":[{"type":"rating","title":"评分","config":{"max":5,"score":1,"correct":"3"}}]}`
	good := `{"title":"好卷","questions":[{"type":"text","title":"填空","config":{"max_len":100,"score":1,"correct":["a"]}}]}`
	f := newFakeAI(t, contentMsg(bad), contentMsg(good))
	got, err := GenerateQuiz(context.Background(), f.client(), "生成一份测验")
	if err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if f.calls != 2 {
		t.Fatalf("应重试一次，实际 %d 次", f.calls)
	}
	if got.Questions[0].Type != "text" {
		t.Fatalf("题型不符: %s", got.Questions[0].Type)
	}
}

// TestGenerateBankQuestions AI 生成题库题目数组。
func TestGenerateBankQuestions(t *testing.T) {
	gen := `{"questions":[
		{"type":"single_choice","title":"1+1=？","config":{"options":[{"label":"2"},{"label":"3"}],"score":10,"correct":"o1"}},
		{"type":"text","title":"水的化学式","config":{"max_len":20,"score":5,"correct":["H2O","h2o"]}}
	]}`
	f := newFakeAI(t, contentMsg(gen))
	got, err := GenerateBankQuestions(context.Background(), f.client(), "出 2 道小学常识题")
	if err != nil {
		t.Fatalf("生成题库题目失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("题目数不符: %d", len(got))
	}
	if got[0].Type != "single_choice" || got[0].Config.Score != 10 {
		t.Fatalf("第 1 题不符: %+v", got[0])
	}
	if got[1].Config.Correct == nil {
		t.Fatalf("第 2 题缺正确答案: %+v", got[1])
	}
}

// TestRemapCorrect 选项 id 重排时正确答案同步映射。
func TestRemapCorrect(t *testing.T) {
	p := model.QuestionPayload{
		Type:  "single_choice",
		Title: "题目",
		Config: model.QuestionConfig{
			Options: []model.Option{{Label: "A"}, {Label: "B"}, {Label: "C"}}, // id 缺失触发重排
			Score:   5,
			Correct: "o2",
		},
	}
	normalizeQuizConfig(&p)
	if p.Config.Options[1].ID != "o2" || p.Config.Correct != "o2" {
		t.Fatalf("correct 未随重排映射: options=%+v correct=%v", p.Config.Options, p.Config.Correct)
	}
	// 多选数组映射
	p2 := model.QuestionPayload{
		Type:  "multiple_choice",
		Title: "题目",
		Config: model.QuestionConfig{
			Options: []model.Option{{ID: "x1", Label: "A"}, {ID: "x2", Label: "B"}, {ID: "x3", Label: "C"}, {ID: "x1", Label: "D"}}, // 重复 id 触发重排
			Score:   5,
			Correct: []any{"x1", "x3"},
		},
	}
	normalizeQuizConfig(&p2)
	ids, ok := p2.Config.Correct.([]any)
	if !ok || len(ids) != 2 || ids[0] != "o1" || ids[1] != "o3" {
		t.Fatalf("多选 correct 映射不符: %+v", p2.Config.Correct)
	}
}
