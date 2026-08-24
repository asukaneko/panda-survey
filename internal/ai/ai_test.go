package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"panda-survey/internal/model"
	"panda-survey/internal/questiontype"
)

// 注册全部题型，供生成/Agent 校验使用。
func init() { questiontype.RegisterAll() }

// fakeAI 按顺序返回预设响应，模拟 OpenAI 兼容服务。
type fakeAI struct {
	responses []string
	calls     int
	server    *httptest.Server
}

func newFakeAI(t *testing.T, responses ...string) *fakeAI {
	f := &fakeAI{responses: responses}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		f.calls++
		idx := f.calls - 1
		if idx >= len(f.responses) {
			idx = len(f.responses) - 1
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(f.responses[idx]))
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeAI) client() *Client {
	return NewClient(Config{BaseURL: f.server.URL, APIKey: "sk-test", Model: "test-model"})
}

func contentMsg(content string) string {
	b, _ := json.Marshal(content)
	return fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}]}`, b)
}

func toolCallMsg(id, name, args string) string {
	ib, _ := json.Marshal(id)
	nb, _ := json.Marshal(name)
	ab, _ := json.Marshal(args)
	return fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[
		{"id":%s,"type":"function","function":{"name":%s,"arguments":%s}}]},
		"finish_reason":"tool_calls"}]}`, ib, nb, ab)
}

func TestGenerateSurveyValid(t *testing.T) {
	gen := `{"title":"员工满意度调查","description":"感谢参与","questions":[
		{"type":"single_choice","title":"你的岗位","required":true,
		 "config":{"options":[{"label":"技术"},{"label":"产品"},{"label":"运营"}]}},
		{"type":"rating","title":"总体满意度","required":true,"config":{}}]}`
	f := newFakeAI(t, contentMsg("```json\n"+gen+"\n```"))
	got, err := GenerateSurvey(context.Background(), f.client(), "员工满意度调查")
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if got.Title != "员工满意度调查" {
		t.Fatalf("标题不符: %s", got.Title)
	}
	if len(got.Questions) != 2 {
		t.Fatalf("题目数不符: %d", len(got.Questions))
	}
	// 选项 id 应被自动补齐
	if got.Questions[0].Config.Options[0].ID != "o1" {
		t.Fatalf("选项 id 未自动生成: %+v", got.Questions[0].Config.Options)
	}
	if got.Questions[1].Config.Max != 5 {
		t.Fatalf("评分默认上限应为 5: %d", got.Questions[1].Config.Max)
	}
}

func TestGenerateSurveyRetry(t *testing.T) {
	bad := `{"title":"测试","questions":[{"type":"single_choice","title":"缺选项","config":{"options":[]}}]}`
	good := `{"title":"测试","questions":[
		{"type":"text","title":"你的建议","required":false,"config":{"max_len":100}}]}`
	f := newFakeAI(t, contentMsg(bad), contentMsg(good))
	got, err := GenerateSurvey(context.Background(), f.client(), "随便生成")
	if err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if f.calls != 2 {
		t.Fatalf("应重试一次，实际调用 %d 次", f.calls)
	}
	if got.Questions[0].Type != "text" {
		t.Fatalf("题型不符: %s", got.Questions[0].Type)
	}
}

func TestAgentEditToolLoop(t *testing.T) {
	addArgs := `{"type":"multiple_choice","title":"你常用的功能","required":false,
		"config":{"options":[{"label":"问卷设计"},{"label":"数据统计"}]}}`
	f := newFakeAI(t,
		toolCallMsg("call1", "add_question", addArgs),
		toolCallMsg("call2", "move_question", `{"index":1,"direction":"down"}`),
		contentMsg("已按你的要求添加题目并调整顺序。"),
	)
	survey := model.Survey{ID: 1, Title: "产品反馈", Description: ""}
	questions := []model.Question{{ID: 1, Type: "text", Title: "你的昵称", SortOrder: 1}}
	res, err := RunAgentEdit(context.Background(), f.client(), survey, questions, "加一道多选题并把它移到第一位")
	if err != nil {
		t.Fatalf("Agent 执行失败: %v", err)
	}
	if f.calls != 3 {
		t.Fatalf("应调用 3 次（两次工具+一次总结），实际 %d", f.calls)
	}
	if len(res.Questions) != 2 {
		t.Fatalf("应有 2 道题: %d", len(res.Questions))
	}
	if res.Questions[0].Type != "multiple_choice" {
		t.Fatalf("第一题应为新加的多选: %s", res.Questions[0].Type)
	}
	if len(res.Steps) != 2 || !res.Steps[0].Success || !res.Steps[1].Success {
		t.Fatalf("步骤记录不符: %+v", res.Steps)
	}
	if res.Summary == "" {
		t.Fatal("应有总结")
	}
	// 提案模式：不应改动原 questions 切片
	if len(questions) != 1 {
		t.Fatalf("原数据被改动: %d", len(questions))
	}
}

func TestAgentEditInvalidIndex(t *testing.T) {
	f := newFakeAI(t,
		toolCallMsg("call1", "delete_question", `{"index":9}`),
		contentMsg("序号超出范围。"),
	)
	survey := model.Survey{ID: 1, Title: "T", Description: ""}
	questions := []model.Question{{ID: 1, Type: "text", Title: "题一"}}
	res, err := RunAgentEdit(context.Background(), f.client(), survey, questions, "删除第9题")
	if err != nil {
		t.Fatalf("工具失败不应中断: %v", err)
	}
	if res.Steps[0].Success {
		t.Fatal("越界删除应记录为失败步骤")
	}
}

func TestClientUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, APIKey: "bad", Model: "m"})
	_, err := c.Chat(context.Background(), chatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("应返回 401 错误: %v", err)
	}
}

func TestClientToolsUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"tools is not supported"}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	_, err := c.Chat(context.Background(), chatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Tools:    toolDefs(),
	})
	if err != ErrToolsUnsupported {
		t.Fatalf("应返回 ErrToolsUnsupported: %v", err)
	}
}

func TestClientSSEStreamFallback(t *testing.T) {
	// 部分网关忽略 stream=false：返回 SSE 流，客户端应聚合 chunks
	sse := "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"你\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"好\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	f := newFakeAI(t, sse)
	f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sse))
	})
	resp, err := f.client().Chat(context.Background(), chatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("SSE 响应应被聚合解析: %v", err)
	}
	if resp.Choices[0].Message.Content != "你好" {
		t.Fatalf("聚合内容不符: %q", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish_reason 不符: %s", resp.Choices[0].FinishReason)
	}
}

func TestClientSSEToolCallAggregation(t *testing.T) {
	sse := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"function\":{\"name\":\"update_\",\"arguments\":\"{\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"survey_meta\",\"arguments\":\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"
	f := newFakeAI(t, sse)
	f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sse))
	})
	resp, err := f.client().Chat(context.Background(), chatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("SSE 工具调用应被聚合: %v", err)
	}
	calls := resp.Choices[0].Message.ToolCalls
	if len(calls) != 1 || calls[0].ID != "c1" ||
		calls[0].Function.Name != "update_survey_meta" || calls[0].Function.Arguments != "{}" {
		t.Fatalf("工具调用聚合不符: %+v", calls)
	}
}

func TestConfigCryptoRoundtrip(t *testing.T) {
	secret := "my-32-byte-secret-key-1234567890"
	plain := "sk-abc123xyz"
	enc, err := encryptMaybe(secret, plain)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if strings.Contains(enc, plain) {
		t.Fatal("密文不应包含明文")
	}
	got, err := decryptMaybe(secret, enc)
	if err != nil || got != plain {
		t.Fatalf("解密不符: %q err=%v", got, err)
	}
	// 错误密钥应失败
	if _, err := decryptMaybe("wrong-key", enc); err == nil {
		t.Fatal("错误密钥应解密失败")
	}
	// 无密钥时明文存储
	plainStored, _ := encryptMaybe("", plain)
	if !strings.HasPrefix(plainStored, "plain:") {
		t.Fatalf("无密钥应带 plain 前缀: %s", plainStored)
	}
	back, _ := decryptMaybe("", plainStored)
	if back != plain {
		t.Fatalf("明文往返不符: %q", back)
	}
	// 掩码
	m := Masked(Config{APIKey: "sk-1234567890abcdef"})
	if m.APIKey != "****cdef" {
		t.Fatalf("掩码不符: %s", m.APIKey)
	}
	if !KeyMasked(m.APIKey) {
		t.Fatal("掩码值应被识别")
	}
}
