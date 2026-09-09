package service

import (
	"testing"

	"panda-survey/internal/model"
	"panda-survey/internal/questiontype"
)

// TestIsCorrect 判分边界：大小写、空答案、多选顺序无关、下拉
func TestIsCorrect(t *testing.T) {
	cases := []struct {
		name string
		q    model.Question
		norm any
		want bool
	}{
		{
			name: "单选正确",
			q:    model.Question{Type: "single_choice", Config: model.QuestionConfig{Correct: "o2"}},
			norm: "o2", want: true,
		},
		{
			name: "单选错误",
			q:    model.Question{Type: "single_choice", Config: model.QuestionConfig{Correct: "o2"}},
			norm: "o1", want: false,
		},
		{
			name: "多选顺序无关",
			q:    model.Question{Type: "multiple_choice", Config: model.QuestionConfig{Correct: []any{"o1", "o2"}}},
			norm: []any{"o2", "o1"}, want: true,
		},
		{
			name: "多选多选少选",
			q:    model.Question{Type: "multiple_choice", Config: model.QuestionConfig{Correct: []any{"o1", "o2"}}},
			norm: []any{"o1"}, want: false,
		},
		{
			name: "多选空数组",
			q:    model.Question{Type: "multiple_choice", Config: model.QuestionConfig{Correct: []any{"o1"}}},
			norm: []any{}, want: false,
		},
		{
			name: "填空忽略大小写与空格",
			q:    model.Question{Type: "text", Config: model.QuestionConfig{Correct: []any{"Beijing", "北京"}}},
			norm: "  BEIJING  ", want: true,
		},
		{
			name: "填空不匹配",
			q:    model.Question{Type: "text", Config: model.QuestionConfig{Correct: []any{"北京"}}},
			norm: "上海", want: false,
		},
		{
			name: "未设置正确答案",
			q:    model.Question{Type: "single_choice", Config: model.QuestionConfig{}},
			norm: "o1", want: false,
		},
	}
	for _, c := range cases {
		if got := isCorrect(c.q, c.norm); got != c.want {
			t.Errorf("%s: isCorrect = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestValidateQuizQuestion 校验规则：题型限制、分值归一化、正确答案存在性
func TestValidateQuizQuestion(t *testing.T) {
	questiontype.RegisterAll()
	// 非答题题型
	err := ValidateQuizQuestion("rating", "评分", &model.QuestionConfig{Max: 5, Score: 1, Correct: "3"})
	if err == nil {
		t.Fatal("评分题应被拒")
	}
	// 单选正确答案不存在
	err = ValidateQuizQuestion("single_choice", "题目", &model.QuestionConfig{
		Options: []model.Option{{ID: "o1", Label: "A"}, {ID: "o2", Label: "B"}},
		Score:   5, Correct: "oX",
	})
	if err == nil {
		t.Fatal("不存在的正确答案应被拒")
	}
	// 分值缺省归一化为 1
	cfg := model.QuestionConfig{
		Options: []model.Option{{ID: "o1", Label: "A"}, {ID: "o2", Label: "B"}},
		Correct: "o1",
	}
	if err := ValidateQuizQuestion("single_choice", "题目", &cfg); err != nil {
		t.Fatalf("合法题目应通过: %v", err)
	}
	if cfg.Score != 1 {
		t.Fatalf("分值应归一化为 1: %d", cfg.Score)
	}
	// 填空正确答案为空
	err = ValidateQuizQuestion("text", "题目", &model.QuestionConfig{Score: 1, Correct: []any{}})
	if err == nil {
		t.Fatal("空正确答案应被拒")
	}
}

// TestValidateQuizConfigShowAnswerMode 展示答案仅分页模式可用：列表模式强制关闭
func TestValidateQuizConfigShowAnswerMode(t *testing.T) {
	// 列表模式：show_answer 被强制关闭
	qc := &model.QuizConfig{DisplayMode: "list", ShowAnswer: true}
	if err := ValidateQuizConfig(qc); err != nil {
		t.Fatalf("合法配置应通过: %v", err)
	}
	if qc.ShowAnswer {
		t.Fatal("列表模式下 show_answer 应被强制关闭")
	}
	// 分页模式：show_answer 保留
	qc = &model.QuizConfig{DisplayMode: "paged", ShowAnswer: true}
	if err := ValidateQuizConfig(qc); err != nil {
		t.Fatalf("合法配置应通过: %v", err)
	}
	if !qc.ShowAnswer {
		t.Fatal("分页模式下 show_answer 应保留")
	}
	// 缺省展示方式按 list 处理，show_answer 同样关闭
	qc = &model.QuizConfig{ShowAnswer: true}
	if err := ValidateQuizConfig(qc); err != nil {
		t.Fatalf("合法配置应通过: %v", err)
	}
	if qc.DisplayMode != "list" || qc.ShowAnswer {
		t.Fatalf("缺省应归一化为 list 且关闭 show_answer: %+v", qc)
	}
}
