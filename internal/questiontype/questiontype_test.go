package questiontype

import (
	"encoding/json"
	"errors"
	"testing"

	"panda-survey/internal/model"
)

func opts(ids ...string) []model.Option {
	out := make([]model.Option, 0, len(ids))
	for _, id := range ids {
		out = append(out, model.Option{ID: id, Label: "选项" + id})
	}
	return out
}

func raw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func TestSingleChoice(t *testing.T) {
	a := SingleChoice{}
	cfg := model.QuestionConfig{Options: opts("o1", "o2")}
	cases := []struct {
		name    string
		in      json.RawMessage
		wantErr bool
	}{
		{"合法选项", raw("o1"), false},
		{"不存在的选项", raw("o9"), true},
		{"空值非必答", raw(""), false},
		{"传了数组", raw([]string{"o1"}), true},
		{"非JSON", json.RawMessage(`{bad`), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := a.ValidateAnswer(c.in, false, cfg)
			if (err != nil) != c.wantErr {
				t.Fatalf("wantErr=%v, got=%v", c.wantErr, err)
			}
		})
	}
	if err := a.ValidateAnswer(raw(""), true, cfg); !errors.Is(err, errRequired) {
		t.Fatalf("必答空值应返回 errRequired, got %v", err)
	}
}

func TestSingleChoiceParseConfig(t *testing.T) {
	a := SingleChoice{}
	if err := a.ParseConfig(model.QuestionConfig{Options: opts("o1")}); err == nil {
		t.Fatal("单个选项应报错")
	}
	if err := a.ParseConfig(model.QuestionConfig{Options: opts("o1", "o1")}); err == nil {
		t.Fatal("重复 id 应报错")
	}
	if err := a.ParseConfig(model.QuestionConfig{Options: opts("o1", "o2")}); err != nil {
		t.Fatalf("合法配置不应报错: %v", err)
	}
}

func TestMultipleChoice(t *testing.T) {
	a := MultipleChoice{}
	cfg := model.QuestionConfig{Options: opts("o1", "o2", "o3"), MinSelect: 1, MaxSelect: 2}
	cases := []struct {
		name    string
		in      json.RawMessage
		wantErr bool
	}{
		{"合法两选", raw([]string{"o1", "o2"}), false},
		{"超最多选数", raw([]string{"o1", "o2", "o3"}), true},
		{"低于最少选数", raw([]string{}), false}, // 空且非必答：通过
		{"含不存在选项", raw([]string{"o1", "x"}), true},
		{"重复选项", raw([]string{"o1", "o1"}), true},
		{"传了字符串", raw("o1"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := a.ValidateAnswer(c.in, false, cfg)
			if (err != nil) != c.wantErr {
				t.Fatalf("wantErr=%v, got=%v", c.wantErr, err)
			}
		})
	}
	if err := a.ValidateAnswer(raw([]string{}), true, cfg); err == nil {
		t.Fatal("必答空选应报错")
	}
	cfgMin2 := cfg
	cfgMin2.MinSelect = 2
	if err := a.ValidateAnswer(raw([]string{"o1"}), true, cfgMin2); err == nil {
		t.Fatal("必答且低于最少选数应报错")
	}
	norm, _ := a.NormalizeAnswer(raw([]string{"o2", "o1"}), cfg)
	b, _ := json.Marshal(norm)
	if string(b) != `["o2","o1"]` {
		t.Fatalf("归一化结果不符: %s", b)
	}
}

func TestTextAndTextarea(t *testing.T) {
	tests := []struct {
		name string
		a    interface {
			ValidateAnswer(json.RawMessage, bool, model.QuestionConfig) error
		}
		limit int
	}{
		{"填空", Text{}, 5},
		{"简答", Textarea{}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/长度", func(t *testing.T) {
			cfg := model.QuestionConfig{MaxLen: tt.limit}
			if tt.limit > 0 {
				if err := tt.a.ValidateAnswer(raw("123456"), false, cfg); err == nil {
					t.Fatal("超长应报错")
				}
				if err := tt.a.ValidateAnswer(raw("12345"), false, cfg); err != nil {
					t.Fatalf("边界长度不应报错: %v", err)
				}
			} else {
				if err := tt.a.ValidateAnswer(raw(longText(2001)), false, cfg); err == nil {
					t.Fatal("超过全局上限应报错")
				}
			}
			if err := tt.a.ValidateAnswer(raw("  "), true, cfg); !errors.Is(err, errRequired) {
				t.Fatalf("必答空白应报 errRequired, got %v", err)
			}
		})
	}
}

func longText(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = '字'
	}
	return string(b)
}

func TestRating(t *testing.T) {
	a := Rating{}
	cfg := model.QuestionConfig{Max: 5}
	cases := []struct {
		name    string
		in      json.RawMessage
		wantErr bool
	}{
		{"合法1分", raw(1), false},
		{"合法5分", raw(5), false},
		{"0分视为未答", raw(0), false},
		{"越界6分", raw(6), true},
		{"小数", raw(3.5), true},
		{"字符串", raw("4"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := a.ValidateAnswer(c.in, false, cfg)
			if (err != nil) != c.wantErr {
				t.Fatalf("wantErr=%v, got=%v", c.wantErr, err)
			}
		})
	}
	if err := a.ValidateAnswer(raw(0), true, cfg); !errors.Is(err, errRequired) {
		t.Fatalf("必答 0 分应报 errRequired, got %v", err)
	}
	// 默认上限 5
	if err := a.ValidateAnswer(raw(5), false, model.QuestionConfig{}); err != nil {
		t.Fatalf("默认上限应允许 5 分: %v", err)
	}
	if err := a.ValidateAnswer(raw(6), false, model.QuestionConfig{}); err == nil {
		t.Fatal("默认上限应拒绝 6 分")
	}
	norm, _ := a.NormalizeAnswer(raw(4), cfg)
	b, _ := json.Marshal(norm)
	if string(b) != "4" {
		t.Fatalf("评分应归一化为整数: %s", b)
	}
}

func TestDropdownSameAsSingle(t *testing.T) {
	cfg := model.QuestionConfig{Options: opts("o1", "o2")}
	dd := Dropdown{}
	if err := dd.ValidateAnswer(raw("o1"), true, cfg); err != nil {
		t.Fatalf("下拉题合法答案: %v", err)
	}
	if err := dd.ValidateAnswer(raw("oX"), true, cfg); err == nil {
		t.Fatal("下拉题非法选项应报错")
	}
}
