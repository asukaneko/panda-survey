package questiontype

import (
	"encoding/json"
	"testing"

	"panda-survey/internal/model"
)

func mopt(ids ...string) []model.Option {
	out := make([]model.Option, 0, len(ids))
	for _, id := range ids {
		out = append(out, model.Option{ID: id, Label: "项" + id})
	}
	return out
}

func TestDate(t *testing.T) {
	a := Date{}
	cases := []struct {
		name    string
		in      json.RawMessage
		wantErr bool
	}{
		{"合法日期", raw("2026-08-22"), false},
		{"非法格式", raw("2026/08/22"), true},
		{"非法月份", raw("2026-13-01"), true},
		{"空值非必答", raw(""), false},
		{"数字", raw(20260822), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := a.ValidateAnswer(c.in, false, model.QuestionConfig{})
			if (err != nil) != c.wantErr {
				t.Fatalf("wantErr=%v, got=%v", c.wantErr, err)
			}
		})
	}
}

func TestMatrix(t *testing.T) {
	a := Matrix{}
	cfg := model.QuestionConfig{
		Rows: []model.Option{{ID: "r1", Label: "速度"}, {ID: "r2", Label: "服务"}},
		Cols: []model.Option{{ID: "c1", Label: "满意"}, {ID: "c2", Label: "不满意"}},
	}
	if err := a.ParseConfig(cfg); err != nil {
		t.Fatalf("合法配置: %v", err)
	}
	if err := a.ParseConfig(model.QuestionConfig{Rows: mopt("r1"), Cols: mopt("c1", "c2")}); err == nil {
		t.Fatal("单行应报错")
	}
	good := raw(map[string]string{"r1": "c1", "r2": "c2"})
	if err := a.ValidateAnswer(good, true, cfg); err != nil {
		t.Fatalf("完整作答应通过: %v", err)
	}
	missing := raw(map[string]string{"r1": "c1"})
	if err := a.ValidateAnswer(missing, true, cfg); err == nil {
		t.Fatal("必答缺行应报错")
	}
	badCol := raw(map[string]string{"r1": "cX", "r2": "c2"})
	if err := a.ValidateAnswer(badCol, true, cfg); err == nil {
		t.Fatal("非法列应报错")
	}
}

func TestSorting(t *testing.T) {
	a := Sorting{}
	cfg := model.QuestionConfig{Options: mopt("o1", "o2", "o3")}
	if err := a.ValidateAnswer(raw([]string{"o3", "o1", "o2"}), true, cfg); err != nil {
		t.Fatalf("完整排列应通过: %v", err)
	}
	if err := a.ValidateAnswer(raw([]string{"o1", "o2"}), true, cfg); err == nil {
		t.Fatal("缺少选项应报错")
	}
	if err := a.ValidateAnswer(raw([]string{"o1", "o1", "o2"}), true, cfg); err == nil {
		t.Fatal("重复选项应报错")
	}
	if err := a.ValidateAnswer(raw([]string{"o1", "o2", "x"}), true, cfg); err == nil {
		t.Fatal("未知选项应报错")
	}
	norm, _ := a.NormalizeAnswer(raw([]string{"o2", "o1", "o3"}), cfg)
	b, _ := json.Marshal(norm)
	if string(b) != `["o2","o1","o3"]` {
		t.Fatalf("排序归一化不符: %s", b)
	}
}
