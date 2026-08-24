package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panda-survey/internal/config"
)

func TestReadTail(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "app.log")
	var sb strings.Builder
	for i := 1; i <= 250; i++ {
		sb.WriteString("line ")
		sb.WriteString(string(rune('0' + i%10)))
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(fp, []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}

	got := readTail(fp, 100)
	if len(got) != 100 {
		t.Fatalf("want 100 lines, got %d", len(got))
	}
	if got[0] != "line 1" { // 末100行起始于第151行(i=151 -> '1')
		t.Fatalf("unexpected first tail line: %q", got[0])
	}

	// 文件不存在
	miss := readTail(filepath.Join(dir, "nope.log"), 100)
	if len(miss) != 1 || miss[0] != "(无此日志)" {
		t.Fatalf("missing-file handling wrong: %v", miss)
	}
}

func TestCollectServerLogs(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log")
	if err := os.WriteFile(logPath, []byte(strings.Repeat("log line\n", 150)), 0644); err != nil {
		t.Fatal(err)
	}
	d := &Deps{
		Cfg:       config.Config{Version: "0.2.0", DBPath: filepath.Join(dir, "panda.db"), LogPath: logPath},
		StartTime: time.Now().Add(-90 * time.Second),
	}

	// 不勾选：不含日志尾部
	short := d.collectServerLogs(false)
	if strings.Contains(short, "app.log (尾") {
		t.Fatalf("include=false 不应包含日志尾部")
	}
	if !strings.Contains(short, "版本: 0.2.0") {
		t.Fatalf("诊断信息缺少版本")
	}

	// 勾选：包含日志尾部与运行时长
	full := d.collectServerLogs(true)
	if !strings.Contains(full, "app.log (尾 100 行)") {
		t.Fatalf("缺少日志尾部段落")
	}
	if !strings.Contains(full, "运行时长:") {
		t.Fatalf("缺少运行时长")
	}
}
