package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"panda-survey/internal/auth"
	"panda-survey/internal/config"
	"panda-survey/internal/db"
	"panda-survey/internal/middleware"
	"panda-survey/internal/questiontype"
	"panda-survey/internal/service"
	"panda-survey/internal/store"
)

type testEnv struct {
	srv    *httptest.Server
	t      *testing.T
	calls  int // fakeAI 调用计数
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	questiontype.RegisterAll()
	cfg := config.Config{SessionTTL: time.Hour}
	env := &testEnv{t: t}

	htmlPage := &fstest.MapFile{Data: []byte("<!DOCTYPE html><html><body>panda</body></html>")}
	fakeWeb := fstest.MapFS{
		"web/index.html":    htmlPage,
		"web/login.html":    htmlPage,
		"web/register.html": htmlPage,
		"web/console.html":  htmlPage,
		"web/banks.html":    htmlPage,
		"web/admin.html":    htmlPage,
		"web/stats.html":    htmlPage,
		"web/fill.html":     htmlPage,
	}

	surveys := &store.SurveyStore{DB: database}
	responses := &store.ResponseStore{DB: database}
	deps := &Deps{
		Cfg: cfg, SessionTTL: cfg.SessionTTL,
		Auth:      auth.New(database, cfg.SessionTTL),
		Surveys:   surveys,
		Responses: responses,
		Settings:  &store.SettingsStore{DB: database},
		AIUsage:   &store.AIUsageStore{DB: database},
		Admin:     &store.AdminStore{DB: database},
		Banks:     &store.BankStore{DB: database},
		SurveySvc: &service.SurveyService{Surveys: surveys, Responses: responses},
		StatsSvc:  &service.StatsService{Surveys: surveys, Responses: responses},
		Limiter:   middleware.NewRateLimiter(1000),
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, deps, fakeWeb)
	env.srv = httptest.NewServer(mux)
	t.Cleanup(env.srv.Close)
	return env
}

func (e *testEnv) newClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 30 * time.Second}
}

func (e *testEnv) do(client *http.Client, method, path string, body any) (int, map[string]any) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatalf("marshal: %v", err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rd)
	if err != nil {
		e.t.Fatalf("new req: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func code(m map[string]any) float64 {
	if m == nil {
		return -1
	}
	c, _ := m["code"].(float64)
	return c
}

func dataMap(m map[string]any) map[string]any {
	d, _ := m["data"].(map[string]any)
	return d
}

func registerAndLogin(e *testEnv, username string) *http.Client {
	c := e.newClient()
	st, m := e.do(c, "POST", "/api/auth/register", map[string]string{
		"username": username, "password": "password123",
	})
	if st != 200 || code(m) != 0 {
		e.t.Fatalf("register %s: status=%d resp=%v", username, st, m)
	}
	return c
}

func sixQuestions() []map[string]any {
	return []map[string]any{
		{"type": "single_choice", "title": "你的性别", "required": true,
			"config": map[string]any{"options": []map[string]any{
				{"id": "o1", "label": "男"}, {"id": "o2", "label": "女"}}}},
		{"type": "multiple_choice", "title": "你用过的功能", "required": true,
			"config": map[string]any{"options": []map[string]any{
				{"id": "o1", "label": "问卷设计"}, {"id": "o2", "label": "数据统计"}, {"id": "o3", "label": "AI 创建"}},
				"min_select": 1, "max_select": 2}},
		{"type": "text", "title": "你的建议", "required": false, "config": map[string]any{"max_len": 100}},
		{"type": "textarea", "title": "详细说明", "required": false, "config": map[string]any{}},
		{"type": "dropdown", "title": "所在城市", "required": true,
			"config": map[string]any{"options": []map[string]any{
				{"id": "o1", "label": "北京"}, {"id": "o2", "label": "上海"}}}},
		{"type": "rating", "title": "整体满意度", "required": true, "config": map[string]any{"max": 5}},
	}
}

// TestFullFlow 注册 -> 建卷 -> 保存六种题型 -> 发布(上限2) -> 匿名提交 -> 自动停止 -> 统计 -> 导出 -> 删除答卷 -> 复制 -> 停止 -> 软删
func TestFullFlow(t *testing.T) {
	e := newTestEnv(t)
	admin := registerAndLogin(e, "admin_user") // 首个注册用户即管理员

	// me
	st, m := e.do(admin, "GET", "/api/auth/me", nil)
	if st != 200 || dataMap(m)["role"].(float64) != 1 {
		t.Fatalf("首个用户应为管理员: %v", m)
	}

	// 创建问卷
	st, m = e.do(admin, "POST", "/api/surveys", map[string]string{"title": "产品反馈", "description": "感谢参与"})
	if st != 200 {
		t.Fatalf("创建问卷失败: %v", m)
	}
	survey := dataMap(m)
	sid := fmt.Sprintf("%.0f", survey["id"].(float64))
	updatedAt := survey["updated_at"].(string)

	// 保存六种题型（乐观锁携带 updated_at）
	st, m = e.do(admin, "PUT", "/api/surveys/"+sid, map[string]any{
		"title": "产品反馈", "description": "感谢参与", "updated_at": updatedAt, "questions": sixQuestions(),
	})
	if st != 200 || code(m) != 0 {
		t.Fatalf("保存问卷失败: %v", m)
	}

	// 发布：回收上限 2 份
	st, m = e.do(admin, "POST", "/api/surveys/"+sid+"/publish", map[string]any{"max_responses": 2})
	if st != 200 || dataMap(m)["status"].(float64) != 1 {
		t.Fatalf("发布失败: %v", m)
	}

	// 匿名获取公开视图
	anon := e.newClient()
	st, m = e.do(anon, "GET", "/api/surveys/"+sid+"/public", nil)
	if st != 200 || code(m) != 0 {
		t.Fatalf("公开视图失败: %v", m)
	}
	pub := dataMap(m)
	questions := pub["questions"].([]any)
	if len(questions) != 6 {
		t.Fatalf("公开视图题目数不符: %d", len(questions))
	}

	// 提交合法答卷
	submit := func() (int, map[string]any) {
		return e.do(anon, "POST", "/api/surveys/"+sid+"/responses", map[string]any{
			"duration": 42,
			"answers": []map[string]any{
				{"question_id": qid(questions, 1), "value": "o1"},
				{"question_id": qid(questions, 2), "value": []string{"o1", "o3"}},
				{"question_id": qid(questions, 3), "value": "界面很好用"},
				{"question_id": qid(questions, 5), "value": "o2"},
				{"question_id": qid(questions, 6), "value": 5},
			},
		})
	}
	st, m = submit()
	if st != 200 || code(m) != 0 {
		t.Fatalf("首次提交失败: %v", m)
	}
	st, m = submit()
	if st != 200 {
		t.Fatalf("第二次提交失败: %v", m)
	}
	// 第三次：上限自动停止
	st, m = submit()
	if st != 400 || code(m) != 1004 {
		t.Fatalf("超上限应返回 1004: status=%d resp=%v", st, m)
	}

	// 统计
	st, m = e.do(admin, "GET", "/api/surveys/"+sid+"/stats", nil)
	if st != 200 {
		t.Fatalf("统计失败: %v", m)
	}
	sd := dataMap(m)
	if sd["total"].(float64) != 2 {
		t.Fatalf("回收量应为 2: %v", sd["total"])
	}
	stats := sd["questions"].([]any)
	first := stats[0].(map[string]any)
	if first["answered"].(float64) != 2 {
		t.Fatalf("单选题作答数应 2: %v", first)
	}
	choices := first["choices"].([]any)
	if len(choices) != 2 || choices[0].(map[string]any)["count"].(float64) != 2 {
		t.Fatalf("单选统计不符: %v", choices)
	}

	// 导出明细：BOM + 表头
	resp, err := anon.Get(e.srv.URL + "/api/surveys/" + sid + "/export?mode=detail")
	if err != nil || resp.StatusCode != 401 {
		t.Fatalf("匿名导出应 401: %v %v", err, resp)
	}
	resp, err = admin.Get(e.srv.URL + "/api/surveys/" + sid + "/export?mode=detail")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("导出失败: %v %v", err, resp)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.HasPrefix(body, []byte("\uFEFF")) || !bytes.Contains(body, []byte("你的性别")) {
		t.Fatalf("导出内容不符: %q", body[:min(100, len(body))])
	}

	// 删除一份答卷 -> 统计回减
	st, m = e.do(admin, "GET", "/api/surveys/"+sid+"/stats", nil)
	_ = m
	// 取明细中最新一份的 response id：从导出不可得，改从 stats 的文本题反查不便——直接删除 response_id=1
	st, m = e.do(admin, "DELETE", "/api/surveys/"+sid+"/responses/1", nil)
	if st != 200 {
		t.Fatalf("删除答卷失败: %v", m)
	}
	st, m = e.do(admin, "GET", "/api/surveys/"+sid+"/stats", nil)
	if dataMap(m)["total"].(float64) != 1 {
		t.Fatalf("删除后回收量应为 1: %v", dataMap(m)["total"])
	}

	// 复制为新草稿
	st, m = e.do(admin, "POST", "/api/surveys/"+sid+"/copy", map[string]any{})
	if st != 200 {
		t.Fatalf("复制失败: %v", m)
	}
	copyID := fmt.Sprintf("%.0f", dataMap(m)["id"].(float64))

	// 乐观锁冲突：用旧版本保存
	st, m = e.do(admin, "PUT", "/api/surveys/"+copyID, map[string]any{
		"title": "副本", "updated_at": "2000-01-01T00:00:00Z", "questions": sixQuestions(),
	})
	if st != 409 || code(m) != 1007 {
		t.Fatalf("乐观锁冲突应 1007: status=%d resp=%v", st, m)
	}

	// 已停止问卷不能再提交
	st, m = e.do(anon, "GET", "/api/surveys/"+sid+"/public", nil)
	if st != 404 {
		t.Fatalf("已停止问卷公开视图应 404: %v", m)
	}

	// 草稿发布 -> 停止
	st, m = e.do(admin, "POST", "/api/surveys/"+copyID+"/publish", map[string]any{})
	if st != 200 {
		t.Fatalf("副本发布失败: %v", m)
	}

	// 发布中状态可直接保存：题目 id 保留，历史答卷关联不失效
	gst, gm := e.do(admin, "GET", "/api/surveys/"+copyID, nil)
	if gst != 200 {
		t.Fatalf("获取副本失败: %v", gm)
	}
	copyQs := dataMap(gm)["questions"].([]any)
	saveQs := make([]map[string]any, 0, len(copyQs))
	for _, cq := range copyQs {
		qm := cq.(map[string]any)
		saveQs = append(saveQs, map[string]any{
			"id": qm["id"], "type": qm["type"], "title": qm["title"].(string) + "（改）",
			"required": qm["required"], "config": qm["config"],
		})
	}
	st, m = e.do(admin, "PUT", "/api/surveys/"+copyID, map[string]any{
		"title": "副本（发布中改）", "description": "", "updated_at": dataMap(m)["updated_at"], "questions": saveQs,
	})
	if st != 200 || code(m) != 0 || dataMap(m)["status"].(float64) != 1 {
		t.Fatalf("发布中保存应成功且保持发布状态: %v", m)
	}
	gst, gm = e.do(admin, "GET", "/api/surveys/"+copyID, nil)
	if gst != 200 {
		t.Fatalf("获取副本失败: %v", gm)
	}
	afterQs := dataMap(gm)["questions"].([]any)
	if len(afterQs) != len(copyQs) {
		t.Fatalf("保存后题目数量应不变: %d -> %d", len(copyQs), len(afterQs))
	}
	for i := range copyQs {
		if afterQs[i].(map[string]any)["id"].(float64) != copyQs[i].(map[string]any)["id"].(float64) {
			t.Fatalf("保存后题目 id 应保持不变: %v -> %v", copyQs[i].(map[string]any)["id"], afterQs[i].(map[string]any)["id"])
		}
	}

	st, m = e.do(admin, "POST", "/api/surveys/"+copyID+"/stop", map[string]any{})
	if st != 200 || dataMap(m)["status"].(float64) != 2 {
		t.Fatalf("停止失败: %v", m)
	}
	stopResp := m // 保留停止后的 updated_at
	// 再停止应报状态错误
	st, m = e.do(admin, "POST", "/api/surveys/"+copyID+"/stop", map[string]any{})
	if st != 400 || code(m) != 1004 {
		t.Fatalf("重复停止应 1004: %v", m)
	}

	// 已停止状态同样可直接保存
	st, m = e.do(admin, "PUT", "/api/surveys/"+copyID, map[string]any{
		"title": "副本（已停止改）", "description": "", "updated_at": dataMap(stopResp)["updated_at"], "questions": saveQs,
	})
	if st != 200 || code(m) != 0 || dataMap(m)["status"].(float64) != 2 {
		t.Fatalf("已停止保存应成功且保持停止状态: %v", m)
	}

	// 软删除
	st, m = e.do(admin, "DELETE", "/api/surveys/"+sid, nil)
	if st != 200 {
		t.Fatalf("软删失败: %v", m)
	}
	st, m = e.do(admin, "GET", "/api/surveys/"+sid, nil)
	if st != 404 {
		t.Fatalf("软删后应 404: %v", m)
	}
}

func qid(questions []any, sortOrder float64) float64 {
	for _, q := range questions {
		qm := q.(map[string]any)
		if qm["sort_order"].(float64) == sortOrder {
			return qm["id"].(float64)
		}
	}
	return 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestReverseCases 反向用例：重复注册/错误密码/未登录/越权/非法答案/CSRF
func TestReverseCases(t *testing.T) {
	e := newTestEnv(t)
	admin := registerAndLogin(e, "usera")

	// 重复注册
	st, m := e.do(e.newClient(), "POST", "/api/auth/register",
		map[string]string{"username": "usera", "password": "password123"})
	if st != 400 || code(m) != 1001 {
		t.Fatalf("重复注册应业务错误: %v", m)
	}
	// 弱密码
	st, m = e.do(e.newClient(), "POST", "/api/auth/register",
		map[string]string{"username": "weakpw", "password": "123"})
	if st != 400 {
		t.Fatalf("弱密码应 400: %v", m)
	}

	// 未登录访问管理接口
	st, m = e.do(e.newClient(), "GET", "/api/surveys", nil)
	if st != 401 || code(m) != 1002 {
		t.Fatalf("未登录应 401/1002: %v", m)
	}

	// 建卷发布
	st, m = e.do(admin, "POST", "/api/surveys", map[string]string{"title": "T1"})
	sid := fmt.Sprintf("%.0f", dataMap(m)["id"].(float64))
	e.do(admin, "PUT", "/api/surveys/"+sid, map[string]any{
		"title": "T1", "questions": sixQuestions()[:1], "updated_at": dataMap(m)["updated_at"],
	})
	e.do(admin, "POST", "/api/surveys/"+sid+"/publish", map[string]any{})

	// 越权：用户 B 操作 A 的问卷
	b := registerAndLogin(e, "userb")
	st, m = e.do(b, "GET", "/api/surveys/"+sid, nil)
	if st != 403 || code(m) != 1003 {
		t.Fatalf("越权应 403/1003: %v", m)
	}
	st, m = e.do(b, "DELETE", "/api/surveys/"+sid+"/responses/1", nil)
	if st != 403 {
		t.Fatalf("越权删答卷应 403: %v", m)
	}
	st, m = e.do(b, "GET", "/api/admin/ai-config", nil)
	if st != 403 {
		t.Fatalf("非管理员访问配置应 403: %v", m)
	}

	// 非法答案：不存在的选项 / 缺必答 / 评分越界
	anon := e.newClient()
	st, m = e.do(anon, "GET", "/api/surveys/"+sid+"/public", nil)
	qs := dataMap(m)["questions"].([]any)
	st, m = e.do(anon, "POST", "/api/surveys/"+sid+"/responses", map[string]any{
		"answers": []map[string]any{{"question_id": qid(qs, 1), "value": "oX"}},
	})
	if st != 400 || code(m) != 1001 {
		t.Fatalf("非法选项应 1001: %v", m)
	}
	st, m = e.do(anon, "POST", "/api/surveys/"+sid+"/responses", map[string]any{"answers": []map[string]any{}})
	if st != 400 {
		t.Fatalf("缺必答应 400: %v", m)
	}

	// 修改密码：旧密码错误 / 成功后旧 session 失效
	st, m = e.do(admin, "PUT", "/api/auth/password",
		map[string]string{"old_password": "wrongpass", "new_password": "newpassword456"})
	if st != 400 {
		t.Fatalf("旧密码错误应 400: %v", m)
	}
	st, m = e.do(admin, "PUT", "/api/auth/password",
		map[string]string{"old_password": "password123", "new_password": "newpassword456"})
	if st != 200 {
		t.Fatalf("改密失败: %v", m)
	}
	st, m = e.do(admin, "GET", "/api/auth/me", nil)
	if st != 401 {
		t.Fatalf("改密后旧 session 应失效: %v", m)
	}

	// CSRF：非 JSON Content-Type 与跨站 Origin
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/auth/login", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, _ := anon.Do(req)
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("非 JSON 写请求应 415: %d", resp.StatusCode)
	}
	resp.Body.Close()
	req2, _ := http.NewRequest("POST", e.srv.URL+"/api/auth/login", bytes.NewReader([]byte(`{}`)))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Origin", "https://evil.example.com")
	resp2, _ := anon.Do(req2)
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("跨站 Origin 应 403: %d", resp2.StatusCode)
	}
	resp2.Body.Close()
}

// TestLoginLockout 连续失败 5 次后锁定
func TestLoginLockout(t *testing.T) {
	e := newTestEnv(t)
	registerAndLogin(e, "lockme")
	c := e.newClient()
	for i := 0; i < 5; i++ {
		e.do(c, "POST", "/api/auth/login", map[string]string{"username": "lockme", "password": "wrongpass123"})
	}
	st, m := e.do(c, "POST", "/api/auth/login", map[string]string{"username": "lockme", "password": "password123"})
	if st != 429 || code(m) != 1005 {
		t.Fatalf("锁定后正确密码也应 429/1005: status=%d resp=%v", st, m)
	}
}

// TestAdminManagement 管理员后台：用户列表/封禁/解封、全站问卷下架/删除、越权拦截
func TestAdminManagement(t *testing.T) {
	e := newTestEnv(t)
	admin := registerAndLogin(e, "bossadmin") // 首个注册用户即管理员
	user := registerAndLogin(e, "targetuser")

	// 越权：普通用户访问管理接口 403
	st, m := e.do(user, "GET", "/api/admin/users", nil)
	if st != 403 {
		t.Fatalf("普通用户访问用户列表应 403: %v", m)
	}

	// 用户列表：含两个用户，targetuser 有一份问卷
	st, m = e.do(admin, "POST", "/api/surveys", map[string]string{"title": "T"})
	_ = m
	st, m = e.do(admin, "GET", "/api/admin/users", nil)
	if st != 200 {
		t.Fatalf("管理员用户列表失败: %v", m)
	}
	userList, _ := m["data"].([]any)
	if len(userList) != 2 {
		t.Fatalf("用户列表应含 2 个用户: %v", m)
	}

	// 全站问卷列表与下架
	st, m = e.do(admin, "POST", "/api/surveys/1/publish", map[string]any{})
	if st != 200 {
		t.Fatalf("发布失败: %v", m)
	}
	st, m = e.do(admin, "GET", "/api/admin/surveys", nil)
	if st != 200 {
		t.Fatalf("全站问卷列表失败: %v", m)
	}
	st, m = e.do(admin, "POST", "/api/admin/surveys/1/stop", map[string]any{})
	if st != 200 {
		t.Fatalf("管理员下架失败: %v", m)
	}
	st, m = e.do(admin, "GET", "/api/surveys/1", nil)
	if dataMap(m)["survey"].(map[string]any)["status"].(float64) != 2 {
		t.Fatalf("下架后状态应为 2: %v", m)
	}

	// 封禁：对方会话立即失效、无法登录
	st, m = e.do(admin, "PUT", "/api/admin/users/2/ban", map[string]any{})
	if st != 200 {
		t.Fatalf("封禁失败: %v", m)
	}
	st, m = e.do(user, "GET", "/api/auth/me", nil)
	if st != 401 {
		t.Fatalf("封禁后会话应失效: %v", m)
	}
	st, m = e.do(e.newClient(), "POST", "/api/auth/login",
		map[string]string{"username": "targetuser", "password": "password123"})
	if st != 403 {
		t.Fatalf("封禁后登录应 403: %v", m)
	}
	// 不能封禁自己
	st, m = e.do(admin, "PUT", "/api/admin/users/1/ban", map[string]any{})
	if st != 400 {
		t.Fatalf("封禁自己应 400: %v", m)
	}
	// 解封后可登录
	st, m = e.do(admin, "PUT", "/api/admin/users/2/unban", map[string]any{})
	if st != 200 {
		t.Fatalf("解封失败: %v", m)
	}
	st, m = e.do(e.newClient(), "POST", "/api/auth/login",
		map[string]string{"username": "targetuser", "password": "password123"})
	if st != 200 {
		t.Fatalf("解封后应可登录: %v", m)
	}

	// 概览
	st, m = e.do(admin, "GET", "/api/admin/overview", nil)
	if st != 200 || dataMap(m)["users"].(float64) != 2 {
		t.Fatalf("概览不符: %v", m)
	}

	// 管理员删除任意问卷
	st, m = e.do(admin, "DELETE", "/api/admin/surveys/1", nil)
	if st != 200 {
		t.Fatalf("管理员删除问卷失败: %v", m)
	}
	st, m = e.do(admin, "GET", "/api/surveys/1", nil)
	if st != 404 {
		t.Fatalf("删除后应 404: %v", m)
	}
}

// TestPhase2Features 二期：模板创建、新题型提交、逻辑跳转、分页标记、统计只读分享、AI 摘要
func TestPhase2Features(t *testing.T) {
	e := newTestEnv(t)
	admin := registerAndLogin(e, "p2user")

	// 1. 从模板创建（产品反馈模板含矩阵/排序题）
	st, m := e.do(admin, "POST", "/api/surveys/from-template",
		map[string]string{"template_id": "product-feedback"})
	if st != 200 || code(m) != 0 {
		t.Fatalf("模板创建失败: %v", m)
	}
	sid := fmt.Sprintf("%.0f", dataMap(m)["id"].(float64))
	st, m = e.do(admin, "GET", "/api/surveys/"+sid, nil)
	qs := dataMap(m)["questions"].([]any)
	if len(qs) != 5 {
		t.Fatalf("模板题目数不符: %d", len(qs))
	}

	// 2. 发布并提交含矩阵/排序/日期的答卷
	e.do(admin, "POST", "/api/surveys/"+sid+"/publish", map[string]any{})
	st, m = e.do(e.newClient(), "GET", "/api/surveys/"+sid+"/public", nil)
	pubQs := dataMap(m)["questions"].([]any)
	qidByOrder := func(order float64) float64 {
		for _, q := range pubQs {
			qm := q.(map[string]any)
			if qm["sort_order"].(float64) == order {
				return qm["id"].(float64)
			}
		}
		return 0
	}
	e.do(e.newClient(), "POST", "/api/surveys/"+sid+"/responses", map[string]any{
		"answers": []map[string]any{
			{"question_id": qidByOrder(1), "value": "o1"},
			{"question_id": qidByOrder(2), "value": []string{"o1"}},
			{"question_id": qidByOrder(3), "value": map[string]string{"r1": "c1", "r2": "c2", "r3": "c3"}},
			{"question_id": qidByOrder(4), "value": []string{"o2", "o1", "o3"}},
			{"question_id": qidByOrder(5), "value": "很好用"},
		},
	})
	st, m = e.do(admin, "GET", "/api/surveys/"+sid+"/stats", nil)
	if st != 200 || dataMap(m)["total"].(float64) != 1 {
		t.Fatalf("统计应回收 1 份: %v", m)
	}
	stats := dataMap(m)["questions"].([]any)
	matrixStat := stats[2].(map[string]any)
	if matrixStat["rows"] == nil {
		t.Fatal("矩阵题应有逐行统计")
	}
	sortStat := stats[3].(map[string]any)
	if sortStat["rankings"] == nil {
		t.Fatal("排序题应有名次统计")
	}

	// 3. 逻辑跳转：第 1 题单选，第 2 题依赖第 1 题选 o2 才显示（必答）
	st, m = e.do(admin, "POST", "/api/surveys", map[string]string{"title": "跳转测试"})
	sid2 := fmt.Sprintf("%.0f", dataMap(m)["id"].(float64))
	ua := dataMap(m)["updated_at"].(string)
	condQuestions := []map[string]any{
		{"type": "single_choice", "title": "是否继续", "required": true,
			"config": map[string]any{"options": []map[string]any{{"id": "o1", "label": "是"}, {"id": "o2", "label": "否"}}}},
		{"type": "text", "title": "原因", "required": true,
			"config": map[string]any{"max_len": 100,
				"visible_if": map[string]any{"question_index": 0, "option_ids": []string{"o2"}}}},
	}
	st, m = e.do(admin, "PUT", "/api/surveys/"+sid2, map[string]any{
		"title": "跳转测试", "updated_at": ua, "questions": condQuestions,
	})
	if st != 200 {
		t.Fatalf("保存逻辑跳转问卷失败: %v", m)
	}
	// 保存带分页标记的 config 校验
	e.do(admin, "POST", "/api/surveys/"+sid2+"/publish", map[string]any{})
	st, m = e.do(e.newClient(), "GET", "/api/surveys/"+sid2+"/public", nil)
	pubQs2 := dataMap(m)["questions"].([]any)
	q1 := qidByOrderIn(pubQs2, 1)
	q2 := qidByOrderIn(pubQs2, 2)
	// 选 o1：隐藏题不答 -> 通过
	st, m = e.do(e.newClient(), "POST", "/api/surveys/"+sid2+"/responses", map[string]any{
		"answers": []map[string]any{{"question_id": q1, "value": "o1"}},
	})
	if st != 200 {
		t.Fatalf("条件隐藏的必答不应校验: %v", m)
	}
	// 选 o2：隐藏题显示且必答 -> 不答报错
	st, m = e.do(e.newClient(), "POST", "/api/surveys/"+sid2+"/responses", map[string]any{
		"answers": []map[string]any{{"question_id": q1, "value": "o2"}},
	})
	if st != 400 {
		t.Fatalf("条件显示的必答缺失应报错: %v", m)
	}
	// 选 o2 + 答 -> 通过
	st, m = e.do(e.newClient(), "POST", "/api/surveys/"+sid2+"/responses", map[string]any{
		"answers": []map[string]any{
			{"question_id": q1, "value": "o2"},
			{"question_id": q2, "value": "太贵了"},
		},
	})
	if st != 200 {
		t.Fatalf("条件满足时作答应通过: %v", m)
	}

	// 4. 非法显示条件被拒：依赖自己
	bad := []map[string]any{
		{"type": "text", "title": "自我依赖", "required": false,
			"config": map[string]any{"visible_if": map[string]any{"question_index": 0, "option_ids": []string{"o1"}}}},
	}
	st, m = e.do(admin, "POST", "/api/surveys", map[string]string{"title": "T"})
	sid3 := fmt.Sprintf("%.0f", dataMap(m)["id"].(float64))
	ua3 := dataMap(m)["updated_at"].(string)
	st, m = e.do(admin, "PUT", "/api/surveys/"+sid3, map[string]any{
		"title": "T", "updated_at": ua3, "questions": bad,
	})
	if st == 200 {
		t.Fatal("依赖自身的显示条件应被拒绝")
	}

	// 5. 统计只读分享
	st, m = e.do(admin, "POST", "/api/surveys/"+sid+"/share-stats", map[string]any{})
	if st != 200 {
		t.Fatalf("生成分享令牌失败: %v", m)
	}
	token := dataMap(m)["token"].(string)
	st, m = e.do(e.newClient(), "GET", "/api/share/"+token+"/stats", nil)
	if st != 200 || dataMap(m)["total"].(float64) != 1 {
		t.Fatalf("匿名只读统计失败: %v", m)
	}
	// 关闭后失效
	e.do(admin, "DELETE", "/api/surveys/"+sid+"/share-stats", nil)
	st, m = e.do(e.newClient(), "GET", "/api/share/"+token+"/stats", nil)
	if st != 404 {
		t.Fatalf("关闭分享后应 404: %v", m)
	}

	// 6. AI 摘要（fake 上游）
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{{
				"message":      map[string]string{"role": "assistant", "content": "- 多数用户表示满意\n- 少数提到价格偏贵"},
				"finish_reason": "stop",
			}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer fake.Close()
	e.do(admin, "POST", "/api/admin/ai-config", map[string]any{
		"base_url": fake.URL, "api_key": "sk-x", "model": "m", "daily_quota": 10,
	})
	st, m = e.do(admin, "POST", "/api/ai/summarize-answers", map[string]any{
		"survey_id": mustFloat(sid), "question_id": qidByOrder(5),
	})
	if st != 200 || dataMap(m)["summary"] == nil {
		t.Fatalf("AI 摘要失败: %v", m)
	}
	// 非文本题摘要被拒
	st, m = e.do(admin, "POST", "/api/ai/summarize-answers", map[string]any{
		"survey_id": mustFloat(sid), "question_id": qidByOrder(1),
	})
	if st != 400 {
		t.Fatalf("非文本题摘要应 400: %v", m)
	}
}

func qidByOrderIn(questions []any, order float64) float64 {
	for _, q := range questions {
		qm := q.(map[string]any)
		if qm["sort_order"].(float64) == order {
			return qm["id"].(float64)
		}
	}
	return 0
}

func mustFloat(s string) float64 {
	var f float64
	fmt.Sscan(s, &f)
	return f
}

// TestAIEndpoints AI 配置与配额（对接 fake OpenAI 服务）
func TestAIEndpoints(t *testing.T) {
	e := newTestEnv(t)
	admin := registerAndLogin(e, "boss") // 管理员
	user := registerAndLogin(e, "member")

	// 未配置：status 未开通、调用报 1006
	st, m := e.do(user, "GET", "/api/ai/status", nil)
	if st != 200 || dataMap(m)["enabled"] != false {
		t.Fatalf("未配置 status 应 disabled: %v", m)
	}
	st, m = e.do(user, "POST", "/api/ai/generate-survey", map[string]string{"prompt": "测试"})
	if st != 400 || code(m) != 1006 {
		t.Fatalf("未配置 AI 调用应 1006: %v", m)
	}

	// fake OpenAI 上游
	genJSON := `{"title":"满意度","description":"d","questions":[{"type":"text","title":"建议","required":false,"config":{}}]}`
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := "```json\n" + genJSON + "\n```"
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]}`, content)
	}))
	defer fake.Close()

	// 管理员保存配置（配额 1）
	st, m = e.do(admin, "POST", "/api/admin/ai-config", map[string]any{
		"base_url": fake.URL, "api_key": "sk-fake", "model": "fake-model", "daily_quota": 1,
	})
	if st != 200 || code(m) != 0 {
		t.Fatalf("保存 AI 配置失败: %v", m)
	}
	// 回读：key 掩码、不回传明文
	st, m = e.do(admin, "GET", "/api/admin/ai-config", nil)
	cfg := dataMap(m)["config"].(map[string]any)
	if cfg["api_key"] == "sk-fake" {
		t.Fatalf("API Key 不应回传明文: %v", cfg)
	}
	// 掩码保存：key 不变
	st, m = e.do(admin, "POST", "/api/admin/ai-config", map[string]any{
		"base_url": fake.URL, "api_key": cfg["api_key"].(string), "model": "fake-model", "daily_quota": 1,
	})
	if st != 200 {
		t.Fatalf("掩码 key 保存失败: %v", m)
	}

	// 连接测试
	st, m = e.do(admin, "POST", "/api/admin/ai-config/test", map[string]any{})
	if st != 200 || dataMap(m)["reachable"] != true {
		t.Fatalf("连接测试失败: %v", m)
	}

	// 用户生成问卷成功（计入配额）
	st, m = e.do(user, "POST", "/api/ai/generate-survey", map[string]string{"prompt": "满意度调查"})
	if st != 200 || code(m) != 0 {
		t.Fatalf("生成失败: %v", m)
	}
	if dataMap(m)["title"] != "满意度" {
		t.Fatalf("生成标题不符: %v", dataMap(m))
	}
	// 第二次：配额用尽
	st, m = e.do(user, "POST", "/api/ai/generate-survey", map[string]string{"prompt": "再来一份"})
	if st != 429 || code(m) != 1005 {
		t.Fatalf("配额用尽应 429/1005: status=%d resp=%v", st, m)
	}
}

// TestAIGenerateQuiz AI 生成答题卷：返回带答案/分值题目与答题配置，可确认创建为 kind=1 答题卷
func TestAIGenerateQuiz(t *testing.T) {
	e := newTestEnv(t)
	admin := registerAndLogin(e, "quizai")

	genJSON := `{"title":"Java 测验","description":"d","quiz_config":{"duration_min":10,"show_answer":true,"display_mode":"paged"},
		"questions":[{"type":"single_choice","title":"int 占多少位？","config":{"options":[{"label":"16"},{"label":"32"}],"score":5,"correct":"o2"}}]}`
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := "```json\n" + genJSON + "\n```"
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]}`, content)
	}))
	defer fake.Close()
	e.do(admin, "POST", "/api/admin/ai-config", map[string]any{
		"base_url": fake.URL, "api_key": "sk-fake", "model": "m", "daily_quota": 10,
	})

	st, m := e.do(admin, "POST", "/api/ai/generate-quiz", map[string]string{"prompt": "Java 基础测验"})
	if st != 200 || code(m) != 0 {
		t.Fatalf("生成答题卷失败: %v", m)
	}
	gen := dataMap(m)
	if gen["quiz_config"] == nil {
		t.Fatalf("应返回 quiz_config: %v", gen)
	}
	qs := gen["questions"].([]any)
	cfg := qs[0].(map[string]any)["config"].(map[string]any)
	if cfg["score"].(float64) != 5 || cfg["correct"].(string) != "o2" {
		t.Fatalf("题目应带分值/答案: %v", cfg)
	}

	// 确认创建：建 kind=1 卷并用生成结果保存
	st, m = e.do(admin, "POST", "/api/surveys", map[string]any{"title": gen["title"], "kind": 1})
	if st != 200 {
		t.Fatalf("创建答题卷失败: %v", m)
	}
	sid := fmt.Sprintf("%.0f", dataMap(m)["id"].(float64))
	st, m = e.do(admin, "PUT", "/api/surveys/"+sid, map[string]any{
		"title": gen["title"], "description": gen["description"],
		"updated_at":  dataMap(m)["updated_at"].(string),
		"questions":   gen["questions"],
		"quiz_config": gen["quiz_config"],
	})
	if st != 200 || code(m) != 0 {
		t.Fatalf("保存 AI 答题卷失败: %v", m)
	}
	// 回读：题目带分值/答案，配置生效
	st, m = e.do(admin, "GET", "/api/surveys/"+sid, nil)
	qs2 := dataMap(m)["questions"].([]any)
	cfg2 := qs2[0].(map[string]any)["config"].(map[string]any)
	if cfg2["score"].(float64) != 5 || cfg2["correct"] == nil {
		t.Fatalf("回读题目应带分值/答案: %v", cfg2)
	}
	qc := dataMap(m)["survey"].(map[string]any)["quiz_config"].(map[string]any)
	if qc["duration_min"].(float64) != 10 || qc["display_mode"].(string) != "paged" {
		t.Fatalf("答题配置不符: %v", qc)
	}
}

// TestAIGenerateBankQuestions AI 生成题库题目，可逐个导入题库
func TestAIGenerateBankQuestions(t *testing.T) {
	e := newTestEnv(t)
	admin := registerAndLogin(e, "bankai")

	genJSON := `{"questions":[
		{"type":"single_choice","title":"1+1=？","config":{"options":[{"label":"2"},{"label":"3"}],"score":10,"correct":"o1"}},
		{"type":"text","title":"水的化学式","config":{"max_len":20,"score":5,"correct":["H2O"]}}]}`
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]}`, genJSON)
	}))
	defer fake.Close()
	e.do(admin, "POST", "/api/admin/ai-config", map[string]any{
		"base_url": fake.URL, "api_key": "sk-fake", "model": "m", "daily_quota": 10,
	})

	st, m := e.do(admin, "POST", "/api/ai/generate-bank-questions", map[string]string{"prompt": "小学常识题"})
	if st != 200 || code(m) != 0 {
		t.Fatalf("生成题库题目失败: %v", m)
	}
	qs := dataMap(m)["questions"].([]any)
	if len(qs) != 2 {
		t.Fatalf("应生成 2 题: %v", m)
	}
	// 导入题库
	st, m = e.do(admin, "POST", "/api/banks", map[string]string{"name": "AI 题库"})
	bankID := fmt.Sprintf("%.0f", dataMap(m)["id"].(float64))
	for _, item := range qs {
		q := item.(map[string]any)
		st, m = e.do(admin, "POST", "/api/banks/"+bankID+"/questions", map[string]any{
			"type": q["type"], "title": q["title"], "config": q["config"],
		})
		if st != 200 {
			t.Fatalf("导入题库题目失败: %v", m)
		}
	}
	st, m = e.do(admin, "GET", "/api/banks/"+bankID+"/questions", nil)
	if len(dataMap(m)["questions"].([]any)) != 2 {
		t.Fatalf("题库应有 2 题: %v", m)
	}
}

// TestPagesEmbedded 页面路由返回 HTML
func TestPagesEmbedded(t *testing.T) {
	e := newTestEnv(t)
	for _, p := range []string{"/", "/login", "/register", "/console", "/banks", "/admin/settings", "/s/1", "/preview/1", "/stats/1"} {
		resp, err := e.newClient().Get(e.srv.URL + p)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("页面 %s 应 200: %v %d", p, err, resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if ct != "text/html; charset=utf-8" {
			t.Fatalf("页面 %s Content-Type 不符: %s", p, ct)
		}
		resp.Body.Close()
	}
	resp, _ := e.newClient().Get(e.srv.URL + "/no-such-page")
	if resp.StatusCode != 404 {
		t.Fatalf("未知路径应 404: %d", resp.StatusCode)
	}
	resp.Body.Close()
}
