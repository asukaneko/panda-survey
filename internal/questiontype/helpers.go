package questiontype

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"panda-survey/internal/model"
)

// ---- 公共校验上限（AI 生成与手工编辑共用同一套规则）----

const (
	maxOptions      = 26
	maxOptionLabel  = 200
	maxOptionIDLen  = 64
	maxTextLenLimit = 2000
	maxRatingMax    = 10
	minRatingMax    = 2
)

var errRequired = errors.New("此题为必答")

func errFormat(format string, a ...any) error {
	return fmt.Errorf("答案格式错误: %s", fmt.Sprintf(format, a...))
}

func decodeValue(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("答案不是合法 JSON")
	}
	return v, nil
}

func isEmptyValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []any:
		return len(t) == 0
	case float64:
		return t == 0
	}
	return false
}

// validateCommonOptions 单选/多选/下拉共用的选项结构校验。
func validateCommonOptions(opts []model.Option) error {
	if len(opts) < 2 {
		return fmt.Errorf("至少需要 2 个选项")
	}
	if len(opts) > maxOptions {
		return fmt.Errorf("选项数量最多 %d 个", maxOptions)
	}
	seen := map[string]bool{}
	for _, o := range opts {
		if o.ID == "" || len(o.ID) > maxOptionIDLen {
			return fmt.Errorf("选项 id 不能为空且不超过 %d 字符", maxOptionIDLen)
		}
		if strings.TrimSpace(o.Label) == "" {
			return fmt.Errorf("选项文本不能为空")
		}
		if len([]rune(o.Label)) > maxOptionLabel {
			return fmt.Errorf("选项文本不超过 %d 字", maxOptionLabel)
		}
		if seen[o.ID] {
			return fmt.Errorf("选项 id 重复: %s", o.ID)
		}
		seen[o.ID] = true
	}
	return nil
}

func optionExists(opts []model.Option, id string) bool {
	for _, o := range opts {
		if o.ID == id {
			return true
		}
	}
	return false
}
