package questiontype

import (
	"encoding/json"

	"panda-survey/internal/model"
)

// ---- 单选题 ----

type SingleChoice struct{}

func (SingleChoice) Type() string  { return "single_choice" }
func (SingleChoice) Label() string { return "单选题" }

func (SingleChoice) ParseConfig(cfg model.QuestionConfig) error {
	return validateCommonOptions(cfg.Options)
}

func (SingleChoice) ValidateAnswer(raw json.RawMessage, required bool, cfg model.QuestionConfig) error {
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
		return errFormat("单选题答案须为选项 id 字符串")
	}
	if !optionExists(cfg.Options, s) {
		return errFormat("选项不存在: %s", s)
	}
	return nil
}

func (SingleChoice) NormalizeAnswer(raw json.RawMessage, cfg model.QuestionConfig) (any, error) {
	v, err := decodeValue(raw)
	if err != nil {
		return nil, err
	}
	s, _ := v.(string)
	return s, nil
}

// ---- 下拉题（答案语义与单选一致）----

type Dropdown struct{}

func (Dropdown) Type() string  { return "dropdown" }
func (Dropdown) Label() string { return "下拉题" }

func (Dropdown) ParseConfig(cfg model.QuestionConfig) error {
	return validateCommonOptions(cfg.Options)
}

func (Dropdown) ValidateAnswer(raw json.RawMessage, required bool, cfg model.QuestionConfig) error {
	return SingleChoice{}.ValidateAnswer(raw, required, cfg)
}

func (Dropdown) NormalizeAnswer(raw json.RawMessage, cfg model.QuestionConfig) (any, error) {
	return SingleChoice{}.NormalizeAnswer(raw, cfg)
}
