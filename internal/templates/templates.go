// Package templates 内置问卷模板库，从模板一键创建草稿。
package templates

import "panda-survey/internal/model"

type Template struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Questions   []model.QuestionPayload `json:"-"`
}

func q(tp, title string, required bool, cfg model.QuestionConfig) model.QuestionPayload {
	return model.QuestionPayload{Type: tp, Title: title, Required: required, Config: cfg}
}

func opts(labels ...string) []model.Option {
	out := make([]model.Option, 0, len(labels))
	for i, l := range labels {
		out = append(out, model.Option{ID: "o" + string(rune('1'+i)), Label: l})
	}
	return out
}

var List = []Template{
	{
		ID:          "employee-satisfaction",
		Name:        "员工满意度调查",
		Description: "了解员工对工作环境、管理与发展的满意度",
		Questions: []model.QuestionPayload{
			q("single_choice", "你所在的部门", true, model.QuestionConfig{Options: opts(
				"技术", "产品", "运营", "市场", "行政/人事")}),
			q("rating", "对公司整体满意度打分", true, model.QuestionConfig{Max: 5}),
			q("multiple_choice", "你认为公司哪些方面做得好", true, model.QuestionConfig{Options: opts(
				"薪酬福利", "团队氛围", "成长空间", "管理制度", "工作强度")}),
			q("single_choice", "你是否有离职打算", true, model.QuestionConfig{Options: opts(
				"没有", "偶尔想过", "正在考虑")}),
			q("textarea", "你最希望公司改进的一点是什么", true, model.QuestionConfig{MaxLen: 300}),
		},
	},
	{
		ID:          "event-signup",
		Name:        "活动报名",
		Description: "收集报名信息与参与意愿",
		Questions: []model.QuestionPayload{
			q("text", "你的姓名", true, model.QuestionConfig{MaxLen: 30}),
			q("text", "联系电话", true, model.QuestionConfig{MaxLen: 20}),
			q("single_choice", "参加人数", true, model.QuestionConfig{Options: opts(
				"1 人", "2 人", "3 人及以上")}),
			q("date", "期望的活动日期", true, model.QuestionConfig{}),
			q("textarea", "饮食禁忌或备注", false, model.QuestionConfig{MaxLen: 200}),
		},
	},
	{
		ID:          "product-feedback",
		Name:        "产品使用反馈",
		Description: "收集功能使用情况与改进建议",
		Questions: []model.QuestionPayload{
			q("single_choice", "你使用产品的频率", true, model.QuestionConfig{Options: opts(
				"每天", "每周几次", "每月几次", "几乎不用")}),
			q("multiple_choice", "你常用的功能", true, model.QuestionConfig{Options: opts(
				"问卷设计", "数据统计", "AI 能力", "导出分享")}),
			q("matrix", "请为以下维度打分", true, model.QuestionConfig{
				Rows: []model.Option{{ID: "r1", Label: "易用性"}, {ID: "r2", Label: "稳定性"}, {ID: "r3", Label: "性能"}},
				Cols: []model.Option{{ID: "c1", Label: "满意"}, {ID: "c2", Label: "一般"}, {ID: "c3", Label: "不满意"}},
			}),
			q("sorting", "请按重要性排序以下能力", true, model.QuestionConfig{Options: opts(
				"速度", "安全", "价格")}),
			q("textarea", "你的改进建议", true, model.QuestionConfig{MaxLen: 500}),
		},
	},
	{
		ID:          "course-evaluation",
		Name:        "课程评价",
		Description: "培训/课程结束后的教学评价",
		Questions: []model.QuestionPayload{
			q("single_choice", "课程难度", true, model.QuestionConfig{Options: opts(
				"偏易", "适中", "偏难")}),
			q("rating", "讲师整体水平", true, model.QuestionConfig{Max: 5}),
			q("matrix", "请评价课程的各个方面", true, model.QuestionConfig{
				Rows: []model.Option{{ID: "r1", Label: "内容安排"}, {ID: "r2", Label: "讲义质量"}, {ID: "r3", Label: "互动答疑"}},
				Cols: []model.Option{{ID: "c1", Label: "优秀"}, {ID: "c2", Label: "良好"}, {ID: "c3", Label: "待改进"}},
			}),
			q("text", "最喜欢的课程环节", false, model.QuestionConfig{MaxLen: 100}),
			q("textarea", "对课程的建议", false, model.QuestionConfig{MaxLen: 300}),
		},
	},
	{
		ID:          "market-research",
		Name:        "消费习惯调研",
		Description: "了解目标人群的消费偏好",
		Questions: []model.QuestionPayload{
			q("dropdown", "你的年龄段", true, model.QuestionConfig{Options: opts(
				"18 岁以下", "18-25 岁", "26-35 岁", "36-45 岁", "45 岁以上")}),
			q("single_choice", "月度可自由支配支出", true, model.QuestionConfig{Options: opts(
				"1000 元以内", "1000-3000 元", "3000-8000 元", "8000 元以上")}),
			q("multiple_choice", "网购时你最看重", true, model.QuestionConfig{Options: opts(
				"价格", "质量", "品牌", "售后", "物流速度")}),
			q("text", "最近买过最满意的一件商品", false, model.QuestionConfig{MaxLen: 100}),
		},
	},
}

// Get 按 ID 取模板。
func Get(id string) *Template {
	for i := range List {
		if List[i].ID == id {
			return &List[i]
		}
	}
	return nil
}
