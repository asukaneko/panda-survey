package questiontype

import (
	"panda-survey/internal/registry"
)

// RegisterAll 注册全部内置题型，main 启动时调用一次。
func RegisterAll() {
	registry.Register(SingleChoice{})
	registry.Register(MultipleChoice{})
	registry.Register(Text{})
	registry.Register(Textarea{})
	registry.Register(Dropdown{})
	registry.Register(Rating{})
	registry.Register(Date{})
	registry.Register(Matrix{})
	registry.Register(Sorting{})
}
