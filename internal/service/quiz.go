package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"panda-survey/internal/model"
	"panda-survey/internal/registry"
)

// 答题卷业务：配置校验、题目正确答案校验、提交判分、排行榜。

// QuizTypes 答题卷支持的题型（均可自动判分）。
var QuizTypes = map[string]bool{
	"single_choice":   true,
	"multiple_choice": true,
	"dropdown":        true,
	"text":            true,
}

const (
	quizMaxDurationMin = 600  // 倒计时上限（分钟）
	quizMaxScore       = 1000 // 单题分值上限
	quizMaxQuestions   = 100
	quizMaxTextAnswers = 10 // 填空题可接受答案数上限
	quizMaxProfile     = 5  // 个人信息字段数上限
)

// ValidateQuizConfig 校验答题卷配置并归一化默认值。
func ValidateQuizConfig(qc *model.QuizConfig) error {
	if qc == nil {
		return nil
	}
	if qc.DurationMin < 0 || qc.DurationMin > quizMaxDurationMin {
		return fmt.Errorf("倒计时须在 0-%d 分钟之间（0 为不限时）", quizMaxDurationMin)
	}
	switch qc.DisplayMode {
	case "":
		qc.DisplayMode = "list"
	case "list", "paged":
	default:
		return fmt.Errorf("展示方式无效（list / paged）")
	}
	// 提交后展示答案仅分页展示（一题一页）下有意义，列表模式强制关闭
	if qc.DisplayMode == "list" {
		qc.ShowAnswer = false
	}
	switch qc.QuestionOrder {
	case "":
		qc.QuestionOrder = "sequential"
	case "sequential", "random":
	default:
		return fmt.Errorf("题目顺序无效（sequential / random）")
	}
	if qc.CollectProfile {
		if len(qc.ProfileFields) == 0 {
			return fmt.Errorf("开启个人信息收集后至少需要一个字段")
		}
		if len(qc.ProfileFields) > quizMaxProfile {
			return fmt.Errorf("个人信息字段最多 %d 个", quizMaxProfile)
		}
		seen := map[string]bool{}
		for i, f := range qc.ProfileFields {
			f.Label = strings.TrimSpace(f.Label)
			if f.Label == "" {
				return fmt.Errorf("个人信息字段 %d 名称不能为空", i+1)
			}
			if len([]rune(f.Label)) > 50 {
				return fmt.Errorf("个人信息字段名称不超过 50 字")
			}
			if seen[f.Label] {
				return fmt.Errorf("个人信息字段重复: %s", f.Label)
			}
			seen[f.Label] = true
			qc.ProfileFields[i] = f
		}
	} else {
		qc.ProfileFields = nil
	}
	return nil
}

// ValidateQuizQuestion 校验答题题目（题干、题型、选项结构与正确答案），
// 分值缺省按 1 分归一化。题库与答题卷编辑共用。
func ValidateQuizQuestion(qType, title string, cfg *model.QuestionConfig) error {
	if !QuizTypes[qType] {
		return fmt.Errorf("答题仅支持单选题、多选题、下拉题、填空题")
	}
	if err := registry.ValidateTitle(title); err != nil {
		return err
	}
	a, err := registry.Get(qType)
	if err != nil {
		return err
	}
	if err := a.ParseConfig(*cfg); err != nil {
		return err
	}
	if cfg.Score < 0 || cfg.Score > quizMaxScore {
		return fmt.Errorf("分值须在 1-%d 之间", quizMaxScore)
	}
	if cfg.Score == 0 {
		cfg.Score = 1
	}
	switch qType {
	case "single_choice", "dropdown":
		id, ok := cfg.Correct.(string)
		if !ok || id == "" {
			return fmt.Errorf("请设置正确答案")
		}
		if !optionIn(cfg.Options, id) {
			return fmt.Errorf("正确答案引用了不存在的选项")
		}
	case "multiple_choice":
		ids, err := correctIDs(cfg.Correct)
		if err != nil {
			return fmt.Errorf("正确答案须为选项 id 数组")
		}
		if len(ids) == 0 {
			return fmt.Errorf("请至少设置一个正确答案")
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id] {
				return fmt.Errorf("正确答案重复: %s", id)
			}
			seen[id] = true
			if !optionIn(cfg.Options, id) {
				return fmt.Errorf("正确答案引用了不存在的选项")
			}
		}
	case "text":
		texts, err := correctTexts(cfg.Correct)
		if err != nil {
			return fmt.Errorf("正确答案须为文本数组")
		}
		if len(texts) == 0 {
			return fmt.Errorf("请至少设置一个正确答案")
		}
		if len(texts) > quizMaxTextAnswers {
			return fmt.Errorf("可接受答案最多 %d 个", quizMaxTextAnswers)
		}
		for _, t := range texts {
			if len([]rune(t)) > 500 {
				return fmt.Errorf("正确答案不超过 500 字")
			}
		}
	}
	return nil
}

// correctIDs 把正确答案 JSON 值归一化为选项 id 数组（兼容单元素字符串）。
func correctIDs(v any) ([]string, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case string:
		return []string{t}, nil
	case []any:
		out := make([]string, 0, len(t))
		for _, it := range t {
			s, ok := it.(string)
			if !ok {
				return nil, fmt.Errorf("须为字符串数组")
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("须为字符串数组")
	}
}

// correctTexts 把填空题正确答案归一化为文本数组（兼容单个字符串）。
func correctTexts(v any) ([]string, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case string:
		if strings.TrimSpace(t) == "" {
			return nil, nil
		}
		return []string{strings.TrimSpace(t)}, nil
	case []any:
		out := make([]string, 0, len(t))
		for _, it := range t {
			s, ok := it.(string)
			if !ok {
				return nil, fmt.Errorf("须为字符串数组")
			}
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("须为字符串数组")
	}
}

// SubmitQuiz 答题卷提交：校验答案格式、判分、落库。
// 答题卷不做必答强校验（未答题按 0 分计），保证倒计时自动交卷始终可用。
func (s *SurveyService) SubmitQuiz(survey *model.Survey, inputs []model.AnswerInput,
	profile []model.ProfileValue, ip, ua string, duration int64) (int64, *model.QuizResult, error) {
	questions, err := s.Surveys.Questions(survey.ID)
	if err != nil {
		return 0, nil, err
	}
	if len(questions) == 0 {
		return 0, nil, fmt.Errorf("答题卷没有题目")
	}
	if len(questions) > quizMaxQuestions {
		return 0, nil, fmt.Errorf("题目数量超出上限")
	}

	profileJSON, err := validateProfile(survey.QuizConfig, profile)
	if err != nil {
		return 0, nil, err
	}

	byID := map[int64]model.Question{}
	for _, q := range questions {
		byID[q.ID] = q
	}
	got := map[int64]bool{}
	answerRows := make([]model.AnswerRow, 0, len(inputs))
	result := &model.QuizResult{Total: 0}
	for _, q := range questions {
		result.Total += questionScore(q)
	}
	for _, in := range inputs {
		q, ok := byID[in.QuestionID]
		if !ok {
			return 0, nil, fmt.Errorf("题目 %d 不属于该答题卷", in.QuestionID)
		}
		if got[in.QuestionID] {
			return 0, nil, fmt.Errorf("题目 %d 重复作答", in.QuestionID)
		}
		got[in.QuestionID] = true
		a, err := registry.Get(q.Type)
		if err != nil {
			return 0, nil, err
		}
		if err := a.ValidateAnswer(in.Value, false, q.Config); err != nil {
			return 0, nil, fmt.Errorf("第 %d 题：%w", q.SortOrder, err)
		}
		norm, err := a.NormalizeAnswer(in.Value, q.Config)
		if err != nil {
			return 0, nil, err
		}
		valueJSON, err := json.Marshal(norm)
		if err != nil {
			return 0, nil, err
		}
		answerRows = append(answerRows, model.AnswerRow{
			QuestionID:   q.ID,
			QuestionType: q.Type,
			Value:        string(valueJSON),
		})
	}

	// 判分：按题序补齐全部题目的判分结果（未作答记 0 分、不正确）
	for _, q := range questions {
		pts := questionScore(q)
		award := 0
		correct := false
		if got[q.ID] {
			// 找到该题归一化答案并比对
			for _, row := range answerRows {
				if row.QuestionID == q.ID {
					var norm any
					if json.Unmarshal([]byte(row.Value), &norm) == nil {
						correct = isCorrect(q, norm)
					}
					break
				}
			}
		}
		if correct {
			award = pts
			result.Score += pts
		}
		if survey.QuizConfig.ShowAnswer {
			result.Results = append(result.Results, model.QuizGrade{
				QuestionID:  q.ID,
				Correct:     correct,
				Awarded:     award,
				CorrectData: q.Config.Correct,
			})
		}
	}

	rid, err := s.Responses.Create(survey, answerRows, profileJSON, int64(result.Score), ip, ua, duration)
	if err != nil {
		return 0, nil, err
	}
	result.ResponseID = rid
	return rid, result, nil
}

func questionScore(q model.Question) int {
	if q.Config.Score > 0 {
		return q.Config.Score
	}
	return 1
}

// GradeQuestion 逐题提交判分（「提交后展示答案」分页模式）：
// 只校验本题答案并返回对错/本题得分/正确答案，不落库；
// 整卷成绩仍由最终交卷（SubmitQuiz）统一判分保存，两者逻辑一致。
func (s *SurveyService) GradeQuestion(survey *model.Survey, questionID int64, value json.RawMessage) (*model.QuizGrade, error) {
	if survey.Kind != model.KindQuiz {
		return nil, fmt.Errorf("该问卷不是答题卷")
	}
	if !survey.QuizConfig.ShowAnswer || survey.QuizConfig.DisplayMode != "paged" {
		return nil, fmt.Errorf("该答题未开启逐题提交")
	}
	questions, err := s.Surveys.Questions(survey.ID)
	if err != nil {
		return nil, err
	}
	var q model.Question
	found := false
	for _, item := range questions {
		if item.ID == questionID {
			q = item
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("题目 %d 不属于该答题卷", questionID)
	}
	a, err := registry.Get(q.Type)
	if err != nil {
		return nil, err
	}
	if err := a.ValidateAnswer(value, false, q.Config); err != nil {
		return nil, fmt.Errorf("第 %d 题：%w", q.SortOrder, err)
	}
	norm, err := a.NormalizeAnswer(value, q.Config)
	if err != nil {
		return nil, err
	}
	correct := isCorrect(q, norm)
	awarded := 0
	if correct {
		awarded = questionScore(q)
	}
	return &model.QuizGrade{
		QuestionID:  q.ID,
		Correct:     correct,
		Awarded:     awarded,
		CorrectData: q.Config.Correct,
	}, nil
}

// isCorrect 比对归一化答案与正确答案。
func isCorrect(q model.Question, norm any) bool {
	switch q.Type {
	case "single_choice", "dropdown":
		want, _ := q.Config.Correct.(string)
		got, _ := norm.(string)
		return want != "" && got == want
	case "multiple_choice":
		want, err := correctIDs(q.Config.Correct)
		if err != nil || len(want) == 0 {
			return false
		}
		// 归一化答案落库再读回为 JSON，解出的是 []any
		var got []string
		switch t := norm.(type) {
		case []string:
			got = t
		case []any:
			for _, it := range t {
				if s, ok := it.(string); ok {
					got = append(got, s)
				}
			}
		}
		if len(got) != len(want) {
			return false
		}
		set := map[string]bool{}
		for _, id := range want {
			set[id] = true
		}
		for _, id := range got {
			if !set[id] {
				return false
			}
		}
		return true
	case "text":
		want, err := correctTexts(q.Config.Correct)
		if err != nil || len(want) == 0 {
			return false
		}
		got, _ := norm.(string)
		got = strings.TrimSpace(got)
		if got == "" {
			return false
		}
		for _, w := range want {
			// 忽略大小写比较，兼容英文答案
			if strings.EqualFold(got, w) {
				return true
			}
		}
		return false
	}
	return false
}

// validateProfile 校验个人信息并序列化为存储 JSON。
func validateProfile(qc model.QuizConfig, vals []model.ProfileValue) (string, error) {
	if !qc.CollectProfile {
		return "", nil
	}
	byLabel := map[string]string{}
	for _, v := range vals {
		label := strings.TrimSpace(v.Label)
		value := strings.TrimSpace(v.Value)
		if label == "" || value == "" {
			continue
		}
		if len([]rune(value)) > 200 {
			return "", fmt.Errorf("「%s」不超过 200 字", label)
		}
		byLabel[label] = value
	}
	out := make([]model.ProfileValue, 0, len(qc.ProfileFields))
	for _, f := range qc.ProfileFields {
		label := strings.TrimSpace(f.Label)
		v := byLabel[label]
		if v == "" {
			if f.Required {
				return "", fmt.Errorf("请填写%s", label)
			}
			continue
		}
		out = append(out, model.ProfileValue{Label: label, Value: v})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
