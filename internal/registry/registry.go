package registry

import (
	"encoding/json"
	"fmt"

	"panda-survey/internal/model"
)

// Adapter 题型适配器：新增题型只需实现此接口并调用 Register。
type Adapter interface {
	Type() string
	Label() string
	// ParseConfig 校验题目 config 结构（选项数量/id/上限等）
	ParseConfig(cfg model.QuestionConfig) error
	// ValidateAnswer 校验答案格式：required=false 且答案为空时直接通过
	ValidateAnswer(raw json.RawMessage, required bool, cfg model.QuestionConfig) error
	// NormalizeAnswer 把原始答案归一化为存储格式（可直接 json.Marshal 的值）
	NormalizeAnswer(raw json.RawMessage, cfg model.QuestionConfig) (any, error)
}

var (
	adapters = map[string]Adapter{}
	order    []string
)

func Register(a Adapter) {
	if _, dup := adapters[a.Type()]; dup {
		return // 幂等：重复注册同一题型直接忽略
	}
	adapters[a.Type()] = a
	order = append(order, a.Type())
}

func Get(t string) (Adapter, error) {
	a, ok := adapters[t]
	if !ok {
		return nil, fmt.Errorf("未知题型: %s", t)
	}
	return a, nil
}

// All 按注册顺序返回全部适配器。
func All() []Adapter {
	out := make([]Adapter, 0, len(order))
	for _, t := range order {
		out = append(out, adapters[t])
	}
	return out
}

// ValidateTitle 题干通用校验。
func ValidateTitle(title string) error {
	if len([]rune(title)) == 0 {
		return fmt.Errorf("题干不能为空")
	}
	if len([]rune(title)) > 500 {
		return fmt.Errorf("题干超过 500 字")
	}
	return nil
}
