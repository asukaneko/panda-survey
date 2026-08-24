package questiontype

import (
	"encoding/json"
	"fmt"

	"panda-survey/internal/model"
)

// ---- 多选题 ----

type MultipleChoice struct{}

func (MultipleChoice) Type() string  { return "multiple_choice" }
func (MultipleChoice) Label() string { return "多选题" }

func (MultipleChoice) ParseConfig(cfg model.QuestionConfig) error {
	if err := validateCommonOptions(cfg.Options); err != nil {
		return err
	}
	if cfg.MinSelect < 0 || cfg.MinSelect > len(cfg.Options) {
		return fmt.Errorf("最少选数须在 0 到选项数之间")
	}
	if cfg.MaxSelect < 0 || cfg.MaxSelect > len(cfg.Options) {
		return fmt.Errorf("最多选数须在 0 到选项数之间")
	}
	if cfg.MinSelect > 0 && cfg.MaxSelect > 0 && cfg.MinSelect > cfg.MaxSelect {
		return fmt.Errorf("最少选数不能大于最多选数")
	}
	return nil
}

func (MultipleChoice) ValidateAnswer(raw json.RawMessage, required bool, cfg model.QuestionConfig) error {
	v, err := decodeValue(raw)
	if err != nil {
		return err
	}
	if isEmptyValue(v) {
		if required {
			return errRequired
		}
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return errFormat("多选题答案须为选项 id 数组")
	}
	seen := map[string]bool{}
	for _, it := range arr {
		s, ok := it.(string)
		if !ok {
			return errFormat("多选题答案须为选项 id 数组")
		}
		if !optionExists(cfg.Options, s) {
			return errFormat("选项不存在: %s", s)
		}
		if seen[s] {
			return errFormat("选项重复: %s", s)
		}
		seen[s] = true
	}
	n := len(arr)
	if required && cfg.MinSelect > 0 && n < cfg.MinSelect {
		return fmt.Errorf("至少选择 %d 项", cfg.MinSelect)
	}
	if !required && n > 0 && cfg.MinSelect > 0 && n < cfg.MinSelect {
		// 非必答但已作答：同样遵守最少选数
		return fmt.Errorf("至少选择 %d 项", cfg.MinSelect)
	}
	max := cfg.MaxSelect
	if max == 0 {
		max = len(cfg.Options)
	}
	if n > max {
		return fmt.Errorf("最多选择 %d 项", max)
	}
	return nil
}

func (MultipleChoice) NormalizeAnswer(raw json.RawMessage, cfg model.QuestionConfig) (any, error) {
	v, err := decodeValue(raw)
	if err != nil {
		return nil, err
	}
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out, nil
}
