package ai

import (
	"context"
	"fmt"
	"strings"
)

// SummarizeAnswers 开放题答案 AI 摘要：归纳要点（每条一行，最多 5 条）。
func SummarizeAnswers(ctx context.Context, c *Client, questionTitle string, answers []string) (string, error) {
	if len(answers) == 0 {
		return "", fmt.Errorf("该题暂无答案可总结")
	}
	if len(answers) > 500 {
		answers = answers[:500]
	}
	sys := `你是调研分析助手。用户给出一道开放题的全部答案文本，请归纳出主要观点要点。
要求：最多 5 条，每条一行、以「- 」开头，概括同类意见并体现占比感受（如"多数用户提到……"）；没有明显共性时列出最有代表性的个别意见；不要编造答案中不存在的内容；直接输出要点列表，不要其他说明。`
	var sb strings.Builder
	sb.WriteString("题目：" + questionTitle + "\n共 " + fmt.Sprint(len(answers)) + " 条答案：\n")
	for i, a := range answers {
		a = strings.TrimSpace(a)
		if len([]rune(a)) > 200 {
			a = string([]rune(a)[:200])
		}
		if a == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, a))
	}
	resp, err := c.Chat(ctx, chatRequest{
		Messages: []Message{
			{Role: "system", Content: sys},
			{Role: "user", Content: sb.String()},
		},
		Temperature: 0.2,
	})
	if err != nil {
		return "", err
	}
	summary := strings.TrimSpace(resp.Choices[0].Message.Content)
	if summary == "" {
		return "", fmt.Errorf("AI 返回了空结果，请重试")
	}
	return summary, nil
}
