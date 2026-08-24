package questiontype

import (
	"encoding/json"
	"fmt"

	"panda-survey/internal/model"
)

// ---- 填空题 / 简答题 ----

func validateTextConfig(maxLen int) error {
	if maxLen < 0 || maxLen > maxTextLenLimit {
		return fmt.Errorf("长度限制须在 0 到 %d 之间", maxTextLenLimit)
	}
	return nil
}

func validateTextAnswer(raw json.RawMessage, required bool, maxLen int) error {
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
	s, ok := v.(string)
	if !ok {
		return errFormat("答案须为文本")
	}
	limit := maxTextLenLimit
	if maxLen > 0 {
		limit = maxLen
	}
	if len([]rune(s)) > limit {
		return fmt.Errorf("答案不超过 %d 字", limit)
	}
	return nil
}

type Text struct{}

func (Text) Type() string  { return "text" }
func (Text) Label() string { return "填空题" }

func (Text) ParseConfig(cfg model.QuestionConfig) error {
	return validateTextConfig(cfg.MaxLen)
}

func (Text) ValidateAnswer(raw json.RawMessage, required bool, cfg model.QuestionConfig) error {
	return validateTextAnswer(raw, required, cfg.MaxLen)
}

func (Text) NormalizeAnswer(raw json.RawMessage, cfg model.QuestionConfig) (any, error) {
	v, _ := decodeValue(raw)
	s, _ := v.(string)
	return s, nil
}

type Textarea struct{}

func (Textarea) Type() string  { return "textarea" }
func (Textarea) Label() string { return "简答题" }

func (Textarea) ParseConfig(cfg model.QuestionConfig) error {
	return validateTextConfig(cfg.MaxLen)
}

func (Textarea) ValidateAnswer(raw json.RawMessage, required bool, cfg model.QuestionConfig) error {
	return validateTextAnswer(raw, required, cfg.MaxLen)
}

func (Textarea) NormalizeAnswer(raw json.RawMessage, cfg model.QuestionConfig) (any, error) {
	v, _ := decodeValue(raw)
	s, _ := v.(string)
	return s, nil
}

// ---- 评分题 ----

type Rating struct{}

func (Rating) Type() string  { return "rating" }
func (Rating) Label() string { return "评分题" }

func (Rating) ParseConfig(cfg model.QuestionConfig) error {
	if cfg.Max == 0 {
		return nil // 默认 5，由使用方填充
	}
	if cfg.Max < minRatingMax || cfg.Max > maxRatingMax {
		return fmt.Errorf("评分上限须在 %d 到 %d 之间", minRatingMax, maxRatingMax)
	}
	return nil
}

func (Rating) ValidateAnswer(raw json.RawMessage, required bool, cfg model.QuestionConfig) error {
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
	f, ok := v.(float64)
	if !ok || f != float64(int(f)) {
		return errFormat("评分答案须为整数")
	}
	max := cfg.Max
	if max == 0 {
		max = 5
	}
	n := int(f)
	if n < 1 || n > max {
		return fmt.Errorf("评分须在 1 到 %d 之间", max)
	}
	return nil
}

func (Rating) NormalizeAnswer(raw json.RawMessage, cfg model.QuestionConfig) (any, error) {
	v, _ := decodeValue(raw)
	f, _ := v.(float64)
	return int(f), nil
}
