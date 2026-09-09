package service

import (
	"encoding/json"
	"fmt"

	"panda-survey/internal/model"
	"panda-survey/internal/registry"
	"panda-survey/internal/store"
)

// SurveyService 问卷业务：保存校验与答卷提交。
type SurveyService struct {
	Surveys   *store.SurveyStore
	Responses *store.ResponseStore
}

// ValidateAndSave 校验题目结构后整体保存（任意状态可编辑，题目按 id 保留）。
// quiz 非空表示答题卷：额外校验答题配置与题目正确答案。
func (s *SurveyService) ValidateAndSave(surveyID, userID int64, title, description string,
	questions []model.QuestionPayload, quiz *model.QuizConfig, expectedUpdatedAt string) error {
	if len([]rune(title)) == 0 {
		return fmt.Errorf("问卷标题不能为空")
	}
	if len([]rune(title)) > 200 {
		return fmt.Errorf("问卷标题不超过 200 字")
	}
	if len([]rune(description)) > 1000 {
		return fmt.Errorf("问卷描述不超过 1000 字")
	}
	if len(questions) == 0 {
		return fmt.Errorf("问卷至少需要一道题目")
	}
	if len(questions) > 100 {
		return fmt.Errorf("题目数量最多 100 道")
	}
	for i := range questions {
		q := &questions[i]
		if err := registry.ValidateTitle(q.Title); err != nil {
			return fmt.Errorf("第 %d 题：%w", i+1, err)
		}
		a, err := registry.Get(q.Type)
		if err != nil {
			return fmt.Errorf("第 %d 题：%w", i+1, err)
		}
		if err := a.ParseConfig(q.Config); err != nil {
			return fmt.Errorf("第 %d 题（%s）：%w", i+1, a.Label(), err)
		}
		if quiz != nil {
			if !QuizTypes[q.Type] {
				return fmt.Errorf("第 %d 题：答题仅支持单选题、多选题、下拉题、填空题", i+1)
			}
			cfg := q.Config
			if err := ValidateQuizQuestion(q.Type, q.Title, &cfg); err != nil {
				return fmt.Errorf("第 %d 题：%w", i+1, err)
			}
			// 分值缺省归一化后写回，保证判分与总分一致
			q.Config.Score = cfg.Score
		}
		// 逻辑跳转：只允许依赖前面的单选/下拉题，且选项须存在
		if vi := q.Config.VisibleIf; vi != nil {
			if vi.QuestionIndex < 0 || vi.QuestionIndex >= i {
				return fmt.Errorf("第 %d 题：显示条件只能依赖前面的题目", i+1)
			}
			src := questions[vi.QuestionIndex]
			if src.Type != "single_choice" && src.Type != "dropdown" {
				return fmt.Errorf("第 %d 题：显示条件只能依赖单选题或下拉题", i+1)
			}
			if len(vi.OptionIDs) == 0 {
				return fmt.Errorf("第 %d 题：显示条件至少选择一个选项", i+1)
			}
			for _, oid := range vi.OptionIDs {
				if !optionIn(src.Config.Options, oid) {
					return fmt.Errorf("第 %d 题：显示条件引用了依赖题不存在的选项", i+1)
				}
			}
		}
	}
	if quiz != nil {
		if err := ValidateQuizConfig(quiz); err != nil {
			return err
		}
	}
	return s.Surveys.Save(surveyID, userID, title, description, questions, quiz, expectedUpdatedAt)
}

func optionIn(opts []model.Option, id string) bool {
	for _, o := range opts {
		if o.ID == id {
			return true
		}
	}
	return false
}

// isVisible 按题序推导可见性：依赖题（单选/下拉）命中条件选项则可见。
func isVisible(questions []model.Question, q model.Question, chosen map[int64]string) bool {
	vi := q.Config.VisibleIf
	if vi == nil {
		return true
	}
	if vi.QuestionIndex < 0 || vi.QuestionIndex >= len(questions) {
		return false
	}
	prev := questions[vi.QuestionIndex]
	val, ok := chosen[prev.ID]
	if !ok {
		return false
	}
	for _, oid := range vi.OptionIDs {
		if val == oid {
			return true
		}
	}
	return false
}

// SubmitResponse 逐题校验（必答、格式、归属、可见性），归一化后落库。
func (s *SurveyService) SubmitResponse(survey *model.Survey, inputs []model.AnswerInput,
	ip, ua string, duration int64) (int64, error) {
	questions, err := s.Surveys.Questions(survey.ID)
	if err != nil {
		return 0, err
	}
	if len(questions) == 0 {
		return 0, fmt.Errorf("问卷没有题目")
	}

	byID := map[int64]model.Question{}
	for _, q := range questions {
		byID[q.ID] = q
	}
	// 先收集单选/下拉题的已选值，用于推导后续题目的可见性
	chosen := map[int64]string{}
	for _, in := range inputs {
		q, ok := byID[in.QuestionID]
		if !ok {
			continue
		}
		if q.Type == "single_choice" || q.Type == "dropdown" {
			var s string
			if json.Unmarshal(in.Value, &s) == nil {
				chosen[q.ID] = s
			}
		}
	}

	got := map[int64]bool{}
	answerRows := make([]model.AnswerRow, 0, len(inputs))
	for _, in := range inputs {
		q, ok := byID[in.QuestionID]
		if !ok {
			return 0, fmt.Errorf("题目 %d 不属于该问卷", in.QuestionID)
		}
		if got[in.QuestionID] {
			return 0, fmt.Errorf("题目 %d 重复作答", in.QuestionID)
		}
		got[in.QuestionID] = true
		// 条件不满足的隐藏题：忽略答案、不参与必答校验
		if !isVisible(questions, q, chosen) {
			continue
		}
		a, err := registry.Get(q.Type)
		if err != nil {
			return 0, err
		}
		if err := a.ValidateAnswer(in.Value, q.Required, q.Config); err != nil {
			return 0, fmt.Errorf("第 %d 题：%w", q.SortOrder, err)
		}
		norm, err := a.NormalizeAnswer(in.Value, q.Config)
		if err != nil {
			return 0, err
		}
		valueJSON, err := json.Marshal(norm)
		if err != nil {
			return 0, err
		}
		answerRows = append(answerRows, model.AnswerRow{
			QuestionID:   q.ID,
			QuestionType: q.Type,
			Value:        string(valueJSON),
		})
	}
	for _, q := range questions {
		if q.Required && !got[q.ID] && isVisible(questions, q, chosen) {
			return 0, fmt.Errorf("第 %d 题：此题为必答", q.SortOrder)
		}
	}
	return s.Responses.Create(survey, answerRows, "", 0, ip, ua, duration)
}
