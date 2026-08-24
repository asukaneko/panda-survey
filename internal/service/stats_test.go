package service

import "testing"

// TestCSVEscape 验证 CSV 转义与公式注入防护。
func TestCSVEscape(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"普通文本", "普通文本"},
		{"含,逗号", `"含,逗号"`},
		{`含"引号"`, `"含""引号"""`},
		{"多\n行", "\"多\n行\""},
		{"=SUM(A1:A2)", `'=SUM(A1:A2)`}, // 公式注入：加 ' 前缀
		{"+1", "'+1"},
		{"-1", "'-1"},
		{"@cmd", "'@cmd"},
		{"x", "x"},
	}
	for _, c := range cases {
		if got := csvEscape(c.in); got != c.want {
			t.Errorf("csvEscape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}