package model

import (
	"database/sql"
	"encoding/json"
	"time"
)

// 全部时间字段在 API 中以 UTC RFC3339 字符串传输，展示层转本地时区。

type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      int    `json:"role"` // 0 普通用户 1 管理员
	CreatedAt string `json:"created_at"`
}

type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// VisibleIf 逻辑跳转：依赖题（question_index，按题目顺序 0 起）选中指定选项之一时本题可见。
// 仅允许依赖排序在前的单选/下拉题；编辑端以题序索引存储，避免整体保存时题目 id 重建导致引用失效。
type VisibleIf struct {
	QuestionIndex int      `json:"question_index"`
	OptionIDs     []string `json:"option_ids"`
}

// QuestionConfig 各题型共用一个结构，字段按题型取用；
// 新增题型属性优先加字段（JSON 透传），不迁移表结构。
// VisibleIf 与 PageBreakAfter 为题目级属性，借存于 config JSON。
type QuestionConfig struct {
	Options        []Option   `json:"options,omitempty"`
	MinSelect      int        `json:"min_select,omitempty"`
	MaxSelect      int        `json:"max_select,omitempty"`
	MaxLen         int        `json:"max_len,omitempty"`
	Max            int        `json:"max,omitempty"` // 评分题上限
	Rows           []Option   `json:"rows,omitempty"` // 矩阵题行
	Cols           []Option   `json:"cols,omitempty"` // 矩阵题列
	VisibleIf      *VisibleIf `json:"visible_if,omitempty"`
	PageBreakAfter bool       `json:"page_break_after,omitempty"`
}

type Survey struct {
	ID            int64  `json:"id"`
	UserID        int64  `json:"-"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Status        int    `json:"status"` // 0 草稿 1 发布中 2 已停止
	Deadline      string `json:"deadline,omitempty"`
	MaxResponses  *int64 `json:"max_responses,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	ResponseCount int64  `json:"response_count"`
}

type Question struct {
	ID        int64           `json:"id"`
	SurveyID  int64           `json:"-"`
	Type      string          `json:"type"`
	Title     string          `json:"title"`
	Required  bool            `json:"required"`
	SortOrder int             `json:"sort_order"`
	Config    QuestionConfig  `json:"config"`
	CreatedAt string          `json:"-"`
}

// QuestionPayload 编辑保存时前端提交的题目结构（整体替换语义）。
type QuestionPayload struct {
	Type     string          `json:"type"`
	Title    string          `json:"title"`
	Required bool            `json:"required"`
	Config   QuestionConfig  `json:"config"`
}

type ResponseRow struct {
	ID        int64  `json:"id"`
	SurveyID  int64  `json:"-"`
	IP        string `json:"ip"`
	Duration  int64  `json:"duration"`
	CreatedAt string `json:"created_at"`
}

type AnswerRow struct {
	ID           int64
	ResponseID   int64
	QuestionID   int64
	QuestionType string
	Value        string // 归一化 JSON 文本
	CreatedAt    string
}

type AnswerInput struct {
	QuestionID int64           `json:"question_id"`
	Value      json.RawMessage `json:"value"`
}

// ---- 统计结构 ----

type ChoiceStat struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type QuestionStats struct {
	Question      Question            `json:"question"`
	Answered      int                 `json:"answered"`
	Choices       []ChoiceStat        `json:"choices,omitempty"`
	Avg           *float64            `json:"avg,omitempty"`
	Distribution  map[string]int      `json:"distribution,omitempty"` // 评分题：分值 -> 计数
	Texts         []string            `json:"texts,omitempty"`       // 文本题：最近的答案文本
	Rows          []MatrixRowStat     `json:"rows,omitempty"`        // 矩阵题：逐行分布
	Rankings      []RankStat          `json:"rankings,omitempty"`    // 排序题：平均位次
}

type MatrixRowStat struct {
	Row    string       `json:"row"`
	Counts []ChoiceStat `json:"counts"`
}

type RankStat struct {
	Label string  `json:"label"`
	Avg   float64 `json:"avg"`   // 平均名次（1 为最好）
	First int     `json:"first"` // 排第一的次数
}

// ---- AI 结构 ----

type GeneratedSurvey struct {
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Questions   []QuestionPayload `json:"questions"`
}

type AgentStep struct {
	Tool    string `json:"tool"`
	Detail  string `json:"detail"`
	Success bool   `json:"success"`
}

type AgentResult struct {
	Steps    []AgentStep     `json:"steps"`
	Survey   Survey          `json:"survey"`
	Questions []Question     `json:"questions"`
	Summary  string          `json:"summary"`
}

type OptimizedQuestion struct {
	Original   QuestionPayload `json:"original"`
	Optimized  QuestionPayload `json:"optimized"`
	Reason     string          `json:"reason,omitempty"`
}

// JSONNull 用于扫描可空列。
func JSONNull(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

func NowUTC() string { return time.Now().UTC().Format(time.RFC3339Nano) }
