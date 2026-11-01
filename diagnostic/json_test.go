package diagnostic

// json_test.go — 机器可读诊断（-c --json）的契约测试
//
// 这里锁定的是「给别人消费的接口」，不是实现细节：
//   1. 零诊断 → []（不是 null，也不是空输出）；
//   2. 每条固定 7 个字段，键顺序稳定，类型固定；
//   3. code 缺失归一化 E000，severity 越界归一化 unknown；
//   4. 按 行 → 列 → 错误码 稳定排序，同位置保留原始因果顺序；
//   5. 中文与 <>& 原样输出（关闭 HTML 转义）。

import (
	"encoding/json"
	"strings"
	"testing"
)

// jsonKeys 契约承诺的字段集合（顺序即键顺序）
var jsonKeys = []string{"code", "file", "line", "col", "message", "suggestion", "severity"}

func decodeJSONArray(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	var arr []map[string]any
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n原文:\n%s", err, data)
	}
	return arr
}

// 零诊断必须是 []，且 JSONDiagnostics 返回非 nil 切片
func TestJSONEmptyReportWritesBracketPair(t *testing.T) {
	r := New()
	if items := r.JSONDiagnostics("demo.hy"); items == nil {
		t.Fatal("JSONDiagnostics 零诊断时应返回非 nil 切片")
	} else if len(items) != 0 {
		t.Fatalf("零诊断应返回空切片，得到 %d 条", len(items))
	}

	data, err := r.JSONBytes("demo.hy")
	if err != nil {
		t.Fatalf("JSONBytes 出错: %v", err)
	}
	if got := string(data); got != "[]\n" {
		t.Fatalf("零诊断应输出 \"[]\\n\"，得到 %q", got)
	}
	if strings.Contains(string(data), "null") {
		t.Fatalf("零诊断不得输出 null，得到 %q", data)
	}
	if arr := decodeJSONArray(t, data); len(arr) != 0 {
		t.Fatalf("解析后应为空数组，得到 %d 条", len(arr))
	}
}

// 真实诊断：7 个字段齐全、类型正确、键顺序稳定
func TestJSONFieldsAlwaysComplete(t *testing.T) {
	r := New()
	r.SetFile("demo.hy")
	r.AddRoot("E102", 3, 7, "未定义的标识符: 甲", "检查拼写")

	items := r.JSONDiagnostics("ignored.hy")
	if len(items) != 1 {
		t.Fatalf("应有 1 条诊断，得到 %d", len(items))
	}
	it := items[0]
	if it.Code != "E102" || it.File != "demo.hy" || it.Line != 3 || it.Col != 7 {
		t.Errorf("字段值不符: %+v", it)
	}
	if it.Message != "未定义的标识符: 甲" || it.Suggestion != "检查拼写" {
		t.Errorf("消息/建议不符: %+v", it)
	}
	if it.Severity != "error" {
		t.Errorf("severity 应为 error，得到 %q", it.Severity)
	}

	data, err := r.JSONBytes("demo.hy")
	if err != nil {
		t.Fatalf("JSONBytes 出错: %v", err)
	}
	arr := decodeJSONArray(t, data)
	if len(arr) != 1 {
		t.Fatalf("JSON 数组应有 1 个元素，得到 %d", len(arr))
	}
	obj := arr[0]
	if len(obj) != len(jsonKeys) {
		t.Errorf("字段数应为 %d，得到 %d: %v", len(jsonKeys), len(obj), obj)
	}
	for _, k := range jsonKeys {
		if _, ok := obj[k]; !ok {
			t.Errorf("缺少字段 %q: %v", k, obj)
		}
	}
	// 类型固定：code/file/message/suggestion/severity 为字符串，line/col 为数字
	for _, k := range []string{"code", "file", "message", "suggestion", "severity"} {
		if _, ok := obj[k].(string); !ok {
			t.Errorf("字段 %q 应为字符串，得到 %T", k, obj[k])
		}
	}
	for _, k := range []string{"line", "col"} {
		if _, ok := obj[k].(float64); !ok {
			t.Errorf("字段 %q 应为数字，得到 %T", k, obj[k])
		}
	}

	// 键顺序即结构体字段顺序（文档承诺的稳定顺序）
	pos := 0
	for _, k := range jsonKeys {
		i := strings.Index(string(data), "\""+k+"\"")
		if i < 0 {
			t.Fatalf("输出中找不到键 %q", k)
		}
		if i < pos {
			t.Fatalf("键顺序被打乱：%q 出现在错误位置\n%s", k, data)
		}
		pos = i
	}
}

// code 为空必须归一化为 E000，消费者总能拿到非空错误码
func TestJSONEmptyCodeBecomesE000(t *testing.T) {
	r := New()
	r.AddRoot("", 1, 1, "未分类问题", "")

	items := r.JSONDiagnostics("a.hy")
	if len(items) != 1 {
		t.Fatalf("应有 1 条诊断，得到 %d", len(items))
	}
	if items[0].Code != "E000" {
		t.Fatalf("空错误码应归一化为 E000，得到 %q", items[0].Code)
	}
}

// severity 取值稳定且可穷举；越界值给 unknown，不能 panic 或空串
func TestJSONSeverityKeys(t *testing.T) {
	cases := []struct {
		sev  Severity
		want string
	}{
		{SeverityError, "error"},
		{SeverityWarning, "warning"},
		{SeverityNote, "note"},
		{SeverityInfo, "info"},
		{Severity(99), "unknown"},
		{Severity(-1), "unknown"},
	}
	for _, c := range cases {
		r := New()
		r.AddRootWithSeverity(c.sev, "E001", 1, 1, "消息", "")
		items := r.JSONDiagnostics("a.hy")
		if len(items) != 1 {
			t.Fatalf("severity=%d 时应有 1 条诊断，得到 %d", int(c.sev), len(items))
		}
		if items[0].Severity != c.want {
			t.Errorf("severity=%d 应映射为 %q，得到 %q", int(c.sev), c.want, items[0].Severity)
		}
	}
}

// 报告未设文件名时用 fallbackFile；报告已设则报告优先
func TestJSONFileFallback(t *testing.T) {
	r := New()
	r.AddRoot("E001", 1, 1, "消息", "")
	if got := r.JSONDiagnostics("cli.hy")[0].File; got != "cli.hy" {
		t.Errorf("无文件名时应使用 fallback，得到 %q", got)
	}

	r.SetFile("report.hy")
	if got := r.JSONDiagnostics("cli.hy")[0].File; got != "report.hy" {
		t.Errorf("报告有文件名时应优先用报告，得到 %q", got)
	}
}

// 排序：行 → 列 → 错误码；同位置保持报告内的原始（因果链）顺序
func TestJSONStableSort(t *testing.T) {
	r := New()
	r.AddRoot("E102", 3, 1, "行3", "")
	r.AddRoot("E101", 1, 5, "行1列5", "")
	r.AddRoot("E102", 1, 2, "行1列2错误码大", "")
	r.AddRoot("E101", 1, 2, "行1列2错误码小", "")
	r.AddRoot("E101", 1, 2, "行1列2同码后到", "")

	items := r.JSONDiagnostics("a.hy")
	want := []string{
		"行1列2错误码小", // 1:2 E101
		"行1列2同码后到", // 1:2 E101，同键稳定保持先到
		"行1列2错误码大", // 1:2 E102
		"行1列5",     // 1:5 E101
		"行3",       // 3:1 E102
	}
	if len(items) != len(want) {
		t.Fatalf("应有 %d 条，得到 %d", len(want), len(items))
	}
	for i, w := range want {
		if items[i].Message != w {
			t.Fatalf("第 %d 条应为 %q，得到 %q（完整顺序: %v）", i, w, items[i].Message, messagesOf(items))
		}
	}
}

func messagesOf(items []JSONDiagnostic) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Message)
	}
	return out
}

// 中文与 <>& 原样输出，便于人眼复核与 grep
func TestJSONNoHTMLEscape(t *testing.T) {
	r := New()
	r.AddRoot("E001", 1, 1, "比较 a<b 与 c>d 时出错 & 建议改用", "把 < 换到括号内")

	data, err := r.JSONBytes("a.hy")
	if err != nil {
		t.Fatalf("JSONBytes 出错: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "a<b") || !strings.Contains(s, "c>d") || !strings.Contains(s, "&") {
		t.Errorf("<>& 不应被转义，得到:\n%s", s)
	}
	if strings.Contains(s, `\u003c`) || strings.Contains(s, `\u003e`) || strings.Contains(s, `\u0026`) {
		t.Errorf("出现了 HTML 转义序列:\n%s", s)
	}
	if !strings.Contains(s, "比较") {
		t.Errorf("中文应原样输出（不得转成 \\u 序列）:\n%s", s)
	}
}

// 因果链三条都要出现在 JSON 里（根因/连带/类型后果各一条，不合并）
func TestJSONKeepsCausalChain(t *testing.T) {
	r := New()
	root := r.AddRoot("E001", 1, 5, "根因", "补全")
	r.AddCascade(root, "E102", 2, 1, "连带", "")
	r.AddType(root, "E203", 3, 4, "类型后果", "")

	items := r.JSONDiagnostics("a.hy")
	if len(items) != 3 {
		t.Fatalf("因果链 3 条都应输出，得到 %d", len(items))
	}
	codes := []string{items[0].Code, items[1].Code, items[2].Code}
	joined := strings.Join(codes, ",")
	if joined != "E001,E102,E203" {
		t.Errorf("应按行序输出 E001,E102,E203，得到 %s", joined)
	}
	if items[0].Severity != "error" {
		t.Errorf("根因 severity 应为 error，得到 %q", items[0].Severity)
	}
}

// WriteJSON 写到任意 Writer；缩进为两空格，末尾有换行（便于 shell 拼接）
func TestJSONWriteIndentAndTrailingNewline(t *testing.T) {
	r := New()
	r.AddRoot("E001", 1, 1, "消息", "")

	data, err := r.JSONBytes("a.hy")
	if err != nil {
		t.Fatalf("JSONBytes 出错: %v", err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Errorf("输出应以换行结尾，得到 %q", data)
	}
	if !strings.HasPrefix(string(data), "[\n  {") {
		t.Errorf("应为两空格缩进的数组，得到:\n%s", data)
	}
}
