package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panda-survey/internal/middleware"
)

// handleFeedback 接收前端反馈，补充服务端诊断信息与运行日志尾部，
// 再以通用反馈协议转发至 panda 主页。策略参照同生态的 openvpn-client：
// 浏览器拿不到服务端日志，故由后端读取本地日志尾部一并上报。
func (d *Deps) handleFeedback(w http.ResponseWriter, r *http.Request) {
	// 同域限频：每 IP 每分钟最多 5 次，防滥用。
	if !d.FeedbackLimit.Allow(middleware.ClientIP(r)) {
		fail(w, http.StatusTooManyRequests, 1005, "提交过于频繁，请稍后再试")
		return
	}

	var req struct {
		Category    string `json:"category"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Contact     string `json:"contact"`
		Logs        string `json:"logs"` // 前端已采集的客户端诊断信息
		IncludeLogs bool   `json:"includeLogs"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" {
		fail(w, http.StatusBadRequest, 1001, "标题不能为空")
		return
	}

	serverDiag := d.collectServerLogs(req.IncludeLogs)
	combined := req.Logs
	if strings.TrimSpace(serverDiag) != "" {
		if combined != "" {
			combined += "\n\n"
		}
		combined += serverDiag
	}

	payload := map[string]string{
		"app":         "pandasurvey",
		"version":     d.Cfg.Version,
		"category":    req.Category,
		"title":       strings.TrimSpace(req.Title),
		"description": req.Description,
		"contact":     req.Contact,
		"logs":        combined,
	}
	pb, _ := json.Marshal(payload)
	req2, err := http.NewRequest(http.MethodPost, "https://www.aykeji.cn/api/app-feedback/pandasurvey", bytes.NewReader(pb))
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, "构造反馈请求失败")
		return
	}
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Feedback-Token", "fnos-pandasurvey-feedback")

	resp, err := http.DefaultClient.Do(req2)
	if err != nil {
		fail(w, http.StatusBadGateway, 1, "无法连接反馈服务: "+err.Error())
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

// collectServerLogs 收集服务端诊断信息：版本 / 数据目录 / 数据库 / 运行时长，
// 勾选时附上运行日志尾部（readTail 截取最后 100 行）。
func (d *Deps) collectServerLogs(include bool) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "=== 熊猫问卷诊断信息 ===\n")
	fmt.Fprintf(&sb, "版本: %s\n", d.Cfg.Version)
	fmt.Fprintf(&sb, "数据目录: %s\n", filepath.Dir(d.Cfg.DBPath))
	fmt.Fprintf(&sb, "数据库: %s\n", d.Cfg.DBPath)
	if !d.StartTime.IsZero() {
		fmt.Fprintf(&sb, "运行时长: %s\n", time.Since(d.StartTime).Round(time.Second))
	}
	if !include {
		return sb.String()
	}
	if d.Cfg.LogPath == "" {
		fmt.Fprintf(&sb, "\n--- 未配置日志路径 ---\n")
		return sb.String()
	}
	lines := readTail(d.Cfg.LogPath, 100)
	fmt.Fprintf(&sb, "\n--- %s (尾 %d 行) ---\n%s\n", filepath.Base(d.Cfg.LogPath), len(lines), strings.Join(lines, "\n"))
	return sb.String()
}

// readTail 读取文件最后 n 行（与 openvpn-client 同策略：整文件读入、按行拆分、取末尾 n 行）。
func readTail(fp string, n int) []string {
	b, err := os.ReadFile(fp)
	if err != nil {
		return []string{"(无此日志)"}
	}
	all := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all
}
