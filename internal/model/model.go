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

// 问卷种类：0 普通问卷 1 答题卷（可判分）。
const (
	KindSurvey = 0
	KindQuiz   = 1
)

type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ProfileField 答题卷答题前收集的个人信息字段。
type ProfileField struct {
	Label    string `json:"label"`
	Required bool   `json:"required"`
}

// ProfileValue 答卷中记录的一条个人信息。
type ProfileValue struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// QuizConfig 答题卷专属配置（kind=1 时生效），整体存于 surveys.quiz_config。
type QuizConfig struct {
	DurationMin    int            `json:"duration_min,omitempty"`   // 倒计时分钟数，0 不限时
	ShowAnswer     bool           `json:"show_answer"`              // 提交后是否展示答案
	DisplayMode    string         `json:"display_mode,omitempty"`   // list 列表 | paged 分页（一题一页），二者互斥
	QuestionOrder  string         `json:"question_order,omitempty"` // sequential 顺序 | random 随机
	CollectProfile bool           `json:"collect_profile"`          // 答题前收集个人信息
	ProfileFields  []ProfileField `json:"profile_fields,omitempty"` // 个人信息字段（CollectProfile 时必填）
	ShowRanking    bool           `json:"show_ranking"`             // 答题后是否可查看排行榜
}

// QuizGrade 单题判分结果。
type QuizGrade struct {
	QuestionID  int64 `json:"question_id"`
	Correct     bool  `json:"correct"`
	Awarded     int   `json:"awarded"`
	CorrectData any   `json:"correct_data,omitempty"` // 正确答案原始值（show_answer 时回传）
}

// QuizResult 整卷判分结果。
type QuizResult struct {
	ResponseID int64       `json:"response_id"`
	Score      int         `json:"score"`
	Total      int         `json:"total"`
	Results    []QuizGrade `json:"results,omitempty"`
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
// Score 与 Correct 为答题卷题目属性：分值与正确答案。
type QuestionConfig struct {
	Options        []Option   `json:"options,omitempty"`
	MinSelect      int        `json:"min_select,omitempty"`
	MaxSelect      int        `json:"max_select,omitempty"`
	MaxLen         int        `json:"max_len,omitempty"`
	Max            int        `json:"max,omitempty"`  // 评分题上限
	Rows           []Option   `json:"rows,omitempty"` // 矩阵题行
	Cols           []Option   `json:"cols,omitempty"` // 矩阵题列
	VisibleIf      *VisibleIf `json:"visible_if,omitempty"`
	PageBreakAfter bool       `json:"page_break_after,omitempty"`
	Score          int        `json:"score,omitempty"`   // 答题分值（1-1000）
	Correct        any        `json:"correct,omitempty"` // 正确答案：单选/下拉为选项 id；多选为 id 数组；填空为可接受文本数组
}

type Survey struct {
	ID            int64      `json:"id"`
	UserID        int64      `json:"-"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Status        int        `json:"status"` // 0 草稿 1 发布中 2 已停止
	Kind          int        `json:"kind"`   // 0 问卷 1 答题
	QuizConfig    QuizConfig `json:"quiz_config,omitempty"`
	Deadline      string     `json:"deadline,omitempty"`
	MaxResponses  *int64     `json:"max_responses,omitempty"`
	CreatedAt     string     `json:"created_at"`
	UpdatedAt     string     `json:"updated_at"`
	ResponseCount int64      `json:"response_count"`
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
// ID > 0 表示保留该题目原地更新（历史答卷的题目关联不失效）；0 或缺失表示新增题目。
type QuestionPayload struct {
	ID       int64           `json:"id"`
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
	Profile   string `json:"profile,omitempty"` // 个人信息 JSON（答题卷）
	Score     int64  `json:"score,omitempty"`   // 得分（答题卷）
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

// GeneratedQuiz AI 生成的答题卷草稿：题目带分值/正确答案，附答题配置。
type GeneratedQuiz struct {
	Title       string            `json:"title"`
	Description string            `json:"description"`
	QuizConfig  QuizConfig        `json:"quiz_config"`
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

// ---- 题库 ----

type QuestionBank struct {
	ID            int64  `json:"id"`
	UserID        int64  `json:"-"`
	Name          string `json:"name"`
	QuestionCount int64  `json:"question_count"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type BankQuestion struct {
	ID        int64          `json:"id"`
	BankID    int64          `json:"-"`
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Required  bool           `json:"required"`
	SortOrder int            `json:"sort_order"`
	Config    QuestionConfig `json:"config"`
	CreatedAt string         `json:"-"`
}

// BankQuestionPayload 题库题目保存结构（整体替换语义由前端单题提交）。
type BankQuestionPayload struct {
	ID     int64          `json:"id"`
	Type   string         `json:"type"`
	Title  string         `json:"title"`
	Config QuestionConfig `json:"config"`
}

// ---- 排行榜 ----

type LeaderboardEntry struct {
	Rank        int    `json:"rank"`
	ResponseID  int64  `json:"response_id"`
	Name        string `json:"name"`
	Score       int64  `json:"score"`
	Duration    int64  `json:"duration"`
	CreatedAt   string `json:"created_at"`
}

// JSONNull 用于扫描可空列。
func JSONNull(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

func NowUTC() string { return time.Now().UTC().Format(time.RFC3339Nano) }
