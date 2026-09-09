package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"panda-survey/internal/model"
	"panda-survey/internal/registry"
	"panda-survey/internal/service"
)

const generateSystemPrompt = `你是问卷设计专家。根据用户需求生成一份问卷，只输出 JSON，不要输出任何其他文字或代码块标记。
JSON 结构如下：
{
  "title": "问卷标题（20 字内）",
  "description": "卷首描述（80 字内）",
  "questions": [
    {"type": "single_choice", "title": "题干", "required": true,
     "config": {"options": [{"id": "o1", "label": "选项文本"}, {"id": "o2", "label": "选项文本"}]}},
    {"type": "multiple_choice", "title": "题干", "required": false,
     "config": {"options": [{"id": "o1", "label": "选项"}], "min_select": 0, "max_select": 0}},
    {"type": "dropdown", "title": "题干", "required": false,
     "config": {"options": [{"id": "o1", "label": "选项"}]}},
    {"type": "text", "title": "题干", "required": false, "config": {"max_len": 100}},
    {"type": "textarea", "title": "题干", "required": false, "config": {"max_len": 500}},
    {"type": "rating", "title": "题干", "required": false, "config": {"max": 5}}
  ]
}
规则：type 只能取上述六种；选项 id 可省略（后端自动生成）；选项 2 到 20 个；评分 max 取 2 到 10；题目 3 到 30 道；题干简明、口语化、无歧义，不引导答案。`

// GenerateSurvey 需求描述生成整卷草稿（不落库）。结构非法时带错误重试最多 2 次。
func GenerateSurvey(ctx context.Context, c *Client, prompt string) (*model.GeneratedSurvey, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, fmt.Errorf("请描述你想生成的问卷")
	}
	if len([]rune(prompt)) > 2000 {
		return nil, fmt.Errorf("需求描述不超过 2000 字")
	}
	messages := []Message{
		{Role: "system", Content: generateSystemPrompt},
		{Role: "user", Content: prompt},
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := c.Chat(ctx, chatRequest{Messages: messages, Temperature: 0.5})
		if err != nil {
			return nil, err
		}
		content := resp.Choices[0].Message.Content
		gen, perr := parseGeneratedSurvey(content)
		if perr == nil {
			return gen, nil
		}
		lastErr = perr
		messages = append(messages,
			Message{Role: "assistant", Content: content},
			Message{Role: "user", Content: "你输出的 JSON 不符合要求：" + perr.Error() + "。请修正后重新只输出 JSON。"})
	}
	return nil, fmt.Errorf("AI 生成的问卷结构未通过校验: %w", lastErr)
}

// extractJSON 从模型输出中截取 JSON 主体（容忍 ```json 代码块包裹与前后说明文字）。
func extractJSON(s string) string {
	if i := strings.Index(s, "```"); i >= 0 {
		if j := strings.Index(s[i:], "\n"); j >= 0 {
			s = s[i+j+1:]
		}
		if k := strings.LastIndex(s, "```"); k >= 0 {
			s = s[:k]
		}
	}
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i < 0 || j <= i {
		return s
	}
	return s[i : j+1]
}

func parseGeneratedSurvey(content string) (*model.GeneratedSurvey, error) {
	var gen model.GeneratedSurvey
	if err := json.Unmarshal([]byte(extractJSON(content)), &gen); err != nil {
		return nil, fmt.Errorf("不是合法 JSON: %v", err)
	}
	gen.Title = strings.TrimSpace(gen.Title)
	if gen.Title == "" {
		return nil, fmt.Errorf("缺少问卷标题")
	}
	if len(gen.Questions) == 0 {
		return nil, fmt.Errorf("至少需要一道题目")
	}
	if len(gen.Questions) > 50 {
		gen.Questions = gen.Questions[:50]
	}
	for i := range gen.Questions {
		q := &gen.Questions[i]
		q.Title = strings.TrimSpace(q.Title)
		if q.Title == "" {
			return nil, fmt.Errorf("第 %d 题缺少题干", i+1)
		}
		normalizeConfig(q)
		a, err := registry.Get(q.Type)
		if err != nil {
			return nil, fmt.Errorf("第 %d 题: %v", i+1, err)
		}
		if err := a.ParseConfig(q.Config); err != nil {
			return nil, fmt.Errorf("第 %d 题（%s）: %v", i+1, a.Label(), err)
		}
	}
	return &gen, nil
}

// normalizeConfig 补默认值并整理选项 id：缺失或重复时统一重排为 o1..on。
func normalizeConfig(p *model.QuestionPayload) {
	switch p.Type {
	case "rating":
		if p.Config.Max == 0 {
			p.Config.Max = 5
		}
	case "single_choice", "multiple_choice", "dropdown":
		if len(p.Config.Options) > 26 {
			p.Config.Options = p.Config.Options[:26]
		}
		idsOK := len(p.Config.Options) >= 2
		seen := map[string]bool{}
		for _, o := range p.Config.Options {
			if o.ID == "" || seen[o.ID] {
				idsOK = false
				break
			}
			seen[o.ID] = true
		}
		if !idsOK {
			for i := range p.Config.Options {
				p.Config.Options[i].ID = fmt.Sprintf("o%d", i+1)
				p.Config.Options[i].Label = strings.TrimSpace(p.Config.Options[i].Label)
			}
		}
		if p.Config.MinSelect < 0 {
			p.Config.MinSelect = 0
		}
		if p.Config.MaxSelect < 0 {
			p.Config.MaxSelect = 0
		}
	}
}

/* ==================== 答题卷 / 题库 生成 ==================== */

const generateQuizSystemPrompt = `你是出题专家。根据用户需求生成一份答题卷（测验），只输出 JSON，不要输出任何其他文字或代码块标记。
JSON 结构如下：
{
  "title": "答题标题（20 字内）",
  "description": "卷首说明（80 字内）",
  "quiz_config": {"duration_min": 10, "show_answer": true, "display_mode": "list", "question_order": "sequential", "collect_profile": false, "profile_fields": [], "show_ranking": false},
  "questions": [
    {"type": "single_choice", "title": "题干", "required": false,
     "config": {"options": [{"id": "o1", "label": "选项文本"}, {"id": "o2", "label": "选项文本"}], "score": 5, "correct": "o1"}},
    {"type": "multiple_choice", "title": "题干", "required": false,
     "config": {"options": [{"id": "o1", "label": "选项"}], "score": 10, "correct": ["o1", "o2"]}},
    {"type": "dropdown", "title": "题干", "required": false,
     "config": {"options": [{"id": "o1", "label": "选项"}], "score": 5, "correct": "o2"}},
    {"type": "text", "title": "题干", "required": false,
     "config": {"max_len": 100, "score": 5, "correct": ["可接受答案1", "可接受答案2"]}}
  ]
}
规则：type 只能取 single_choice / multiple_choice / dropdown / text 四种；每题必须带分值 score（1 到 1000）与正确答案 correct（单选/下拉为选项 id 字符串，多选为选项 id 数组，填空为可接受文本数组，数组元素 1 到 10 个）；选项 id 可省略（后端自动生成，correct 须与选项一一对应）；选项 2 到 26 个；题目 3 到 30 道；quiz_config 字段均可省略（后端补默认），duration_min 为倒计时分钟（0 不限时），display_mode 取 list 或 paged，question_order 取 sequential 或 random；题干简明、无歧义、不引导答案，正确答案客观唯一。`

// GenerateQuiz 需求描述生成整份答题卷草稿（不落库）。结构非法时带错误重试最多 2 次。
func GenerateQuiz(ctx context.Context, c *Client, prompt string) (*model.GeneratedQuiz, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, fmt.Errorf("请描述你想生成的答题卷")
	}
	if len([]rune(prompt)) > 2000 {
		return nil, fmt.Errorf("需求描述不超过 2000 字")
	}
	messages := []Message{
		{Role: "system", Content: generateQuizSystemPrompt},
		{Role: "user", Content: prompt},
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := c.Chat(ctx, chatRequest{Messages: messages, Temperature: 0.5})
		if err != nil {
			return nil, err
		}
		content := resp.Choices[0].Message.Content
		gen, perr := parseGeneratedQuiz(content)
		if perr == nil {
			return gen, nil
		}
		lastErr = perr
		messages = append(messages,
			Message{Role: "assistant", Content: content},
			Message{Role: "user", Content: "你输出的 JSON 不符合要求：" + perr.Error() + "。请修正后重新只输出 JSON。"})
	}
	return nil, fmt.Errorf("AI 生成的答题卷结构未通过校验: %w", lastErr)
}

// parseGeneratedQuiz 解析并校验答题卷 JSON。
func parseGeneratedQuiz(content string) (*model.GeneratedQuiz, error) {
	var gen model.GeneratedQuiz
	if err := json.Unmarshal([]byte(extractJSON(content)), &gen); err != nil {
		return nil, fmt.Errorf("不是合法 JSON: %v", err)
	}
	gen.Title = strings.TrimSpace(gen.Title)
	if gen.Title == "" {
		return nil, fmt.Errorf("缺少答题标题")
	}
	if len(gen.Questions) == 0 {
		return nil, fmt.Errorf("至少需要一道题目")
	}
	if len(gen.Questions) > 50 {
		gen.Questions = gen.Questions[:50]
	}
	for i := range gen.Questions {
		q := &gen.Questions[i]
		q.Title = strings.TrimSpace(q.Title)
		if q.Title == "" {
			return nil, fmt.Errorf("第 %d 题缺少题干", i+1)
		}
		normalizeQuizConfig(q)
		cfg := q.Config
		if err := service.ValidateQuizQuestion(q.Type, q.Title, &cfg); err != nil {
			return nil, fmt.Errorf("第 %d 题: %v", i+1, err)
		}
		q.Config = cfg
	}
	if err := service.ValidateQuizConfig(&gen.QuizConfig); err != nil {
		return nil, err
	}
	return &gen, nil
}

// GenerateBankQuestions 需求描述生成一组题库题目（不落库），供题库页预览导入。
func GenerateBankQuestions(ctx context.Context, c *Client, prompt string) ([]model.BankQuestionPayload, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, fmt.Errorf("请描述你想生成的题目")
	}
	if len([]rune(prompt)) > 2000 {
		return nil, fmt.Errorf("需求描述不超过 2000 字")
	}
	sys := `你是出题专家。根据用户需求生成一组题库题目，只输出 JSON，不要输出任何其他文字或代码块标记。
JSON 结构：{"questions": [ ... 题目数组 ... ]}
每题格式与规则：
{"type": "single_choice|multiple_choice|dropdown|text", "title": "题干",
 "config": {"options": [{"id":"o1","label":"选项"},...], "score": 5, "correct": "o1" 或 ["o1","o2"] 或 ["可接受答案"]}}
规则：type 只能取 single_choice / multiple_choice / dropdown / text；每题必须带分值 score（1 到 1000）与正确答案 correct（单选/下拉为选项 id，多选为 id 数组，填空为可接受文本数组 1 到 10 个）；选项 id 可省略（后端自动生成）；选项 2 到 26 个；题目 3 到 30 道；题干简明、无歧义，答案客观唯一。`
	messages := []Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: prompt},
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := c.Chat(ctx, chatRequest{Messages: messages, Temperature: 0.5})
		if err != nil {
			return nil, err
		}
		content := resp.Choices[0].Message.Content
		qs, perr := parseBankQuestions(content)
		if perr == nil {
			return qs, nil
		}
		lastErr = perr
		messages = append(messages,
			Message{Role: "assistant", Content: content},
			Message{Role: "user", Content: "你输出的 JSON 不符合要求：" + perr.Error() + "。请修正后重新只输出 JSON。"})
	}
	return nil, fmt.Errorf("AI 生成的题目结构未通过校验: %w", lastErr)
}

// parseBankQuestions 解析并校验题库题目数组。
func parseBankQuestions(content string) ([]model.BankQuestionPayload, error) {
	var wrapper struct {
		Questions []model.BankQuestionPayload `json:"questions"`
	}
	if err := json.Unmarshal([]byte(extractJSON(content)), &wrapper); err != nil {
		return nil, fmt.Errorf("不是合法 JSON: %v", err)
	}
	if len(wrapper.Questions) == 0 {
		return nil, fmt.Errorf("至少需要一道题目")
	}
	if len(wrapper.Questions) > 50 {
		wrapper.Questions = wrapper.Questions[:50]
	}
	for i := range wrapper.Questions {
		q := &wrapper.Questions[i]
		q.Title = strings.TrimSpace(q.Title)
		if q.Title == "" {
			return nil, fmt.Errorf("第 %d 题缺少题干", i+1)
		}
		p := model.QuestionPayload{Type: q.Type, Title: q.Title, Config: q.Config}
		normalizeQuizConfig(&p)
		cfg := p.Config
		if err := service.ValidateQuizQuestion(q.Type, q.Title, &cfg); err != nil {
			return nil, fmt.Errorf("第 %d 题: %v", i+1, err)
		}
		q.Config = cfg
	}
	return wrapper.Questions, nil
}

// normalizeQuizConfig 归一化答题题目：选项 id 重排时同步映射正确答案，保证 correct 仍指向对应选项。
func normalizeQuizConfig(p *model.QuestionPayload) {
	if p.Type != "single_choice" && p.Type != "multiple_choice" && p.Type != "dropdown" {
		// 填空等无选项题型只做基础处理
		normalizeConfig(p)
		return
	}
	oldIDToIdx := map[string]int{}
	for i, o := range p.Config.Options {
		if o.ID != "" {
			if _, dup := oldIDToIdx[o.ID]; !dup {
				oldIDToIdx[o.ID] = i // 重复 id 取首次出现，与 normalizeConfig 的重排语义一致
			}
		}
	}
	oldCorrect := p.Config.Correct
	normalizeConfig(p)
	p.Config.Correct = remapCorrect(oldCorrect, oldIDToIdx, p.Config.Options)
}

// remapCorrect 按旧选项 id -> 新选项下标映射正确答案；无法映射的保留原值（后续校验兜底）。
func remapCorrect(old any, oldIDToIdx map[string]int, newOpts []model.Option) any {
	mapID := func(id string) string {
		if idx, ok := oldIDToIdx[id]; ok && idx < len(newOpts) {
			return newOpts[idx].ID
		}
		return id
	}
	switch t := old.(type) {
	case string:
		return mapID(t)
	case []any:
		out := make([]any, 0, len(t))
		for _, it := range t {
			if s, ok := it.(string); ok {
				out = append(out, mapID(s))
			}
		}
		return out
	default:
		return old
	}
}
