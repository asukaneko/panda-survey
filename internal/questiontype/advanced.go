package questiontype

import (
	"encoding/json"
	"fmt"
	"strings"

	"panda-survey/internal/model"
)

// ---- 日期题 ----

type Date struct{}

func (Date) Type() string  { return "date" }
func (Date) Label() string { return "日期题" }

func (Date) ParseConfig(cfg model.QuestionConfig) error { return nil }

func validDate(s string) bool {
	if len(s) != 10 {
		return false
	}
	if s[4] != '-' || s[7] != '-' {
		return false
	}
	for i, c := range s {
		if i == 4 || i == 7 {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	m := (s[5]-'0')*10 + (s[6] - '0')
	d := (s[8]-'0')*10 + (s[9] - '0')
	return m >= 1 && m <= 12 && d >= 1 && d <= 31
}

func (Date) ValidateAnswer(raw json.RawMessage, required bool, cfg model.QuestionConfig) error {
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
	if !ok || !validDate(s) {
		return errFormat("日期答案须为 YYYY-MM-DD")
	}
	return nil
}

func (Date) NormalizeAnswer(raw json.RawMessage, cfg model.QuestionConfig) (any, error) {
	v, _ := decodeValue(raw)
	s, _ := v.(string)
	return s, nil
}

// ---- 矩阵题 ----

type Matrix struct{}

func (Matrix) Type() string  { return "matrix" }
func (Matrix) Label() string { return "矩阵题" }

func validateMatrixItems(items []model.Option, what string) error {
	if len(items) < 2 || len(items) > 10 {
		return fmt.Errorf("矩阵%s数量须在 2 到 10 之间", what)
	}
	seen := map[string]bool{}
	for _, o := range items {
		if o.ID == "" || strings.TrimSpace(o.Label) == "" {
			return fmt.Errorf("矩阵%s id 与文本不能为空", what)
		}
		if len([]rune(o.Label)) > maxOptionLabel {
			return fmt.Errorf("矩阵%s文本过长", what)
		}
		if seen[o.ID] {
			return fmt.Errorf("矩阵%s id 重复: %s", what, o.ID)
		}
		seen[o.ID] = true
	}
	return nil
}

func (Matrix) ParseConfig(cfg model.QuestionConfig) error {
	if err := validateMatrixItems(cfg.Rows, "行"); err != nil {
		return err
	}
	return validateMatrixItems(cfg.Cols, "列")
}

// value：{"行id": "列id", ...}，每行均须作答（必答时）
func (Matrix) ValidateAnswer(raw json.RawMessage, required bool, cfg model.QuestionConfig) error {
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
	obj, ok := v.(map[string]any)
	if !ok {
		return errFormat("矩阵题答案须为 行id -> 列id 对象")
	}
	colOK := func(id string) bool {
		for _, c := range cfg.Cols {
			if c.ID == id {
				return true
			}
		}
		return false
	}
	for _, row := range cfg.Rows {
		cv, ok := obj[row.ID]
		if !ok {
			if required {
				return fmt.Errorf("矩阵第「%s」行未作答", row.Label)
			}
			continue
		}
		cs, ok := cv.(string)
		if !ok || !colOK(cs) {
			return errFormat("矩阵第「%s」行的列选项无效", row.Label)
		}
	}
	if len(obj) > len(cfg.Rows) {
		return errFormat("矩阵答案含未知行")
	}
	return nil
}

func (Matrix) NormalizeAnswer(raw json.RawMessage, cfg model.QuestionConfig) (any, error) {
	v, _ := decodeValue(raw)
	obj, _ := v.(map[string]any)
	out := map[string]string{}
	for k, cv := range obj {
		if s, ok := cv.(string); ok {
			out[k] = s
		}
	}
	return out, nil
}

// ---- 排序题 ----

type Sorting struct{}

func (Sorting) Type() string  { return "sorting" }
func (Sorting) Label() string { return "排序题" }

func (Sorting) ParseConfig(cfg model.QuestionConfig) error {
	return validateCommonOptions(cfg.Options)
}

// value：全部选项 id 的排列（完整排序）
func (Sorting) ValidateAnswer(raw json.RawMessage, required bool, cfg model.QuestionConfig) error {
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
		return errFormat("排序题答案须为选项 id 数组")
	}
	if len(arr) != len(cfg.Options) {
		return fmt.Errorf("排序题须包含全部 %d 个选项", len(cfg.Options))
	}
	seen := map[string]bool{}
	for _, it := range arr {
		s, ok := it.(string)
		if !ok || !optionExists(cfg.Options, s) {
			return errFormat("排序题含无效选项")
		}
		if seen[s] {
			return errFormat("排序题选项重复: %s", s)
		}
		seen[s] = true
	}
	return nil
}

func (Sorting) NormalizeAnswer(raw json.RawMessage, cfg model.QuestionConfig) (any, error) {
	v, _ := decodeValue(raw)
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out, nil
}
