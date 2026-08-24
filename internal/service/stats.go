package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"panda-survey/internal/model"
	"panda-survey/internal/store"
)

// StatsService 逐题统计聚合。
type StatsService struct {
	Surveys   *store.SurveyStore
	Responses *store.ResponseStore
}

func (s *StatsService) Stats(survey *model.Survey) ([]model.QuestionStats, int64, error) {
	questions, err := s.Surveys.Questions(survey.ID)
	if err != nil {
		return nil, 0, err
	}
	responses, err := s.Responses.List(survey.ID)
	if err != nil {
		return nil, 0, err
	}
	total := int64(len(responses))
	out := make([]model.QuestionStats, 0, len(questions))
	for _, q := range questions {
		qs := model.QuestionStats{Question: q}
		switch q.Type {
		case "single_choice", "multiple_choice", "dropdown":
			dist, answered, err := s.Responses.ChoiceDistribution(q.ID)
			if err != nil {
				return nil, 0, err
			}
			qs.Answered = answered
			// 按选项定义顺序输出，保证图表稳定；多选 value 为数组需遍历统计
			for _, o := range q.Config.Options {
				var count int
				if q.Type == "multiple_choice" {
					count = countArrayValue(dist, o.ID)
				} else {
					count = dist[quoteJSON(o.ID)]
				}
				qs.Choices = append(qs.Choices, model.ChoiceStat{Label: o.Label, Count: count})
			}
		case "rating":
			dist, answered, err := s.Responses.ChoiceDistribution(q.ID)
			if err != nil {
				return nil, 0, err
			}
			qs.Answered = answered
			max := q.Config.Max
			if max == 0 {
				max = 5
			}
			qs.Distribution = map[string]int{}
			sum, n := 0.0, 0
			for i := 1; i <= max; i++ {
				key := strconv.Itoa(i)
				cnt := dist[key] // 评分归一化存储为整数，JSON 文本即 "3"
				qs.Distribution[key] = cnt
				sum += float64(i * cnt)
				n += cnt
			}
			if n > 0 {
				avg := sum / float64(n)
				// 保留一位小数
				avg = float64(int(avg*10+0.5)) / 10
				qs.Avg = &avg
			}
		case "text", "textarea", "date":
			texts, err := s.Responses.RecentTexts(q.ID, 200)
			if err != nil {
				return nil, 0, err
			}
			qs.Texts = texts
			qs.Answered = len(texts)
		case "matrix":
			values, err := s.Responses.QuestionValues(q.ID)
			if err != nil {
				return nil, 0, err
			}
			qs.Answered = len(values)
			for _, row := range q.Config.Rows {
				ms := model.MatrixRowStat{Row: row.Label}
				for _, col := range q.Config.Cols {
					cnt := 0
					for _, v := range values {
						var m map[string]string
						if json.Unmarshal([]byte(v), &m) == nil && m[row.ID] == col.ID {
							cnt++
						}
					}
					ms.Counts = append(ms.Counts, model.ChoiceStat{Label: col.Label, Count: cnt})
				}
				qs.Rows = append(qs.Rows, ms)
			}
		case "sorting":
			values, err := s.Responses.QuestionValues(q.ID)
			if err != nil {
				return nil, 0, err
			}
			qs.Answered = len(values)
			for _, opt := range q.Config.Options {
				sum, n, first := 0.0, 0, 0
				for _, v := range values {
					var arr []string
					if json.Unmarshal([]byte(v), &arr) != nil {
						continue
					}
					for i, id := range arr {
						if id == opt.ID {
							sum += float64(i + 1)
							n++
							if i == 0 {
								first++
							}
						}
					}
				}
				avg := 0.0
				if n > 0 {
					avg = float64(int(sum/float64(n)*10+0.5)) / 10
				}
				qs.Rankings = append(qs.Rankings, model.RankStat{
					Label: opt.Label, Avg: avg, First: first,
				})
			}
		}
		out = append(out, qs)
	}
	return out, total, nil
}

func quoteJSON(s string) string { return `"` + s + `"` }

func countArrayValue(dist map[string]int, optionID string) int {
	total := 0
	for v, n := range dist {
		var arr []string
		if err := json.Unmarshal([]byte(v), &arr); err != nil {
			continue
		}
		for _, id := range arr {
			if id == optionID {
				total += n
				break
			}
		}
	}
	return total
}

func decodeJSONString(v string) string {
	var s string
	json.Unmarshal([]byte(v), &s)
	return s
}

// ---- CSV 导出 ----

const csvBOM = "\uFEFF"

func csvEscape(s string) string {
	// 防公式注入：以 = + - @ 开头（或含制表/回车）的单元格加 ' 前缀，
	// 避免 Excel/LibreOffice 打开时按公式执行。
	if s != "" {
		switch s[0] {
		case '=', '+', '-', '@':
			s = "'" + s
		}
	}
	need := false
	for _, r := range s {
		if r == ',' || r == '"' || r == '\n' || r == '\r' {
			need = true
			break
		}
	}
	if !need {
		return s
	}
	out := `"`
	for _, r := range s {
		if r == '"' {
			out += `""`
		} else {
			out += string(r)
		}
	}
	return out + `"`
}

// AnswerDisplay 答案 JSON 文本转可读文本（选项 id 映射为标签），导出与明细列表共用。
func AnswerDisplay(qType, value string, cfg model.QuestionConfig) string {
	return answerDisplay(qType, value, cfg)
}

// answerDisplay 答案 JSON 文本转可读文本（选项 id 映射为标签）。
func answerDisplay(qType, value string, cfg model.QuestionConfig) string {
	idLabel := map[string]string{}
	for _, o := range cfg.Options {
		idLabel[o.ID] = o.Label
	}
	switch qType {
	case "single_choice", "dropdown":
		id := decodeJSONString(value)
		if l, ok := idLabel[id]; ok {
			return l
		}
		return id
	case "multiple_choice":
		var arr []string
		if err := json.Unmarshal([]byte(value), &arr); err != nil {
			return value
		}
		parts := make([]string, 0, len(arr))
		for _, id := range arr {
			if l, ok := idLabel[id]; ok {
				parts = append(parts, l)
			} else {
				parts = append(parts, id)
			}
		}
		out := ""
		for i, p := range parts {
			if i > 0 {
				out += " | "
			}
			out += p
		}
		return out
	case "rating":
		return value
	case "date":
		return decodeJSONString(value)
	case "matrix":
		var m map[string]string
		if err := json.Unmarshal([]byte(value), &m); err != nil {
			return value
		}
		rowLabel := map[string]string{}
		colLabel := map[string]string{}
		for _, r := range cfg.Rows {
			rowLabel[r.ID] = r.Label
		}
		for _, c := range cfg.Cols {
			colLabel[c.ID] = c.Label
		}
		parts := make([]string, 0, len(cfg.Rows))
		for _, r := range cfg.Rows {
			if cid, ok := m[r.ID]; ok {
				parts = append(parts, rowLabel[r.ID]+"："+colLabel[cid])
			}
		}
		return strings.Join(parts, " | ")
	case "sorting":
		var arr []string
		if err := json.Unmarshal([]byte(value), &arr); err != nil {
			return value
		}
		idLabel := map[string]string{}
		for _, o := range cfg.Options {
			idLabel[o.ID] = o.Label
		}
		parts := make([]string, 0, len(arr))
		for i, id := range arr {
			label := idLabel[id]
			if label == "" {
				label = id
			}
			parts = append(parts, fmt.Sprintf("%d.%s", i+1, label))
		}
		return strings.Join(parts, " > ")
	default:
		return decodeJSONString(value)
	}
}

// ExportDetail 明细 CSV：每行一份答卷。
func (s *StatsService) ExportDetail(survey *model.Survey) (string, error) {
	questions, err := s.Surveys.Questions(survey.ID)
	if err != nil {
		return "", err
	}
	responses, err := s.Responses.List(survey.ID)
	if err != nil {
		return "", err
	}
	answers, err := s.Responses.AnswersOfSurvey(survey.ID)
	if err != nil {
		return "", err
	}
	byRID := map[int64][]model.AnswerRow{}
	byQID := map[int64]model.Question{}
	for _, a := range answers {
		byRID[a.ResponseID] = append(byRID[a.ResponseID], a)
	}
	for _, q := range questions {
		byQID[q.ID] = q
	}

	csv := csvBOM + "序号,提交时间,填写耗时(秒)"
	for _, q := range questions {
		csv += "," + csvEscape(fmt.Sprintf("%d. %s", q.SortOrder, q.Title))
	}
	csv += "\r\n"
	// responses 按 id 倒序，导出改为正序
	for i := len(responses) - 1; i >= 0; i-- {
		r := responses[i]
		csv += fmt.Sprintf("%d,%s,%d", len(responses)-i, r.CreatedAt, r.Duration)
		for _, q := range questions {
			cell := ""
			for _, a := range byRID[r.ID] {
				if a.QuestionID == q.ID {
					cell = answerDisplay(q.Type, a.Value, q.Config)
					break
				}
			}
			csv += "," + csvEscape(cell)
		}
		csv += "\r\n"
	}
	return csv, nil
}

// ExportSummary 汇总 CSV：每题统计。
func (s *StatsService) ExportSummary(survey *model.Survey) (string, error) {
	stats, _, err := s.Stats(survey)
	if err != nil {
		return "", err
	}
	csv := csvBOM + "题目,选项/维度,计数\r\n"
	for _, st := range stats {
		q := st.Question
		if len(st.Choices) > 0 {
			for _, c := range st.Choices {
				csv += csvEscape(fmt.Sprintf("%d. %s", q.SortOrder, q.Title)) + "," +
					csvEscape(c.Label) + "," + fmt.Sprintf("%d\r\n", c.Count)
			}
		} else if st.Distribution != nil {
			max := q.Config.Max
			if max == 0 {
				max = 5
			}
			for i := 1; i <= max; i++ {
				key := strconv.Itoa(i)
				csv += csvEscape(fmt.Sprintf("%d. %s", q.SortOrder, q.Title)) + "," +
					csvEscape(key+" 分") + "," + fmt.Sprintf("%d\r\n", st.Distribution[key])
			}
		} else {
			csv += csvEscape(fmt.Sprintf("%d. %s", q.SortOrder, q.Title)) + ",作答数," +
				fmt.Sprintf("%d\r\n", st.Answered)
		}
	}
	return csv, nil
}
