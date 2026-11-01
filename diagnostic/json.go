package diagnostic

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
)

// ============================================================
// 机器可读诊断（zuzo -c --json）
// ============================================================
//
// 这是给编辑器、CI、测试脚本消费的稳定契约，改动需同步 docs/diagnostics.md：
//
//  1. 输出永远是 JSON 数组；零诊断输出 []（不是 null，也不是空输出）；
//  2. 每个元素固定 7 个字段 code/file/line/col/message/suggestion/severity，
//     字段永远存在（不使用 omitempty），类型固定，不随诊断种类变化；
//  3. severity 取 error / warning / note / info（取值越界时为 unknown）；
//  4. code 为空时归一化为 E000（未分类），消费者总能拿到非空错误码；
//  5. 人读文本（Render 的树形报告、✓ 通过提示）不进 stdout，由调用方写 stderr；
//  6. 元素按行、列、错误码稳定排序，同一位置保持原始因果顺序（稳定排序）。
// ============================================================

// JSONDiagnostic 单条机器可读诊断。
// 字段顺序即 JSON 键顺序，也是文档中承诺的稳定顺序。
type JSONDiagnostic struct {
	Code       string `json:"code"`       // 错误码，如 E102；缺失时为 E000
	File       string `json:"file"`       // 源文件名（报告级）；未知时为空串
	Line       int    `json:"line"`       // 1 起始行号；0 表示无位置
	Col        int    `json:"col"`        // 1 起始列号（按字符计）；0 表示无列
	Message    string `json:"message"`    // 人类可读消息（中文）
	Suggestion string `json:"suggestion"` // 修复建议；无建议时为空串
	Severity   string `json:"severity"`   // error / warning / note / info / unknown
}

// Key 返回严重级别的机器可读名称。
// 与 String()（中文人读名）分开，避免 CLI 文案改动破坏 JSON 契约。
func (s Severity) Key() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	case SeverityNote:
		return "note"
	case SeverityInfo:
		return "info"
	default:
		return "unknown"
	}
}

// JSONDiagnostics 把报告转换为机器可读诊断数组。
// fallbackFile 在报告自身未设置文件名时使用（通常传命令行给出的路径）。
// 返回的切片永远非 nil，零诊断时长度为 0，序列化为 []。
func (r *Report) JSONDiagnostics(fallbackFile string) []JSONDiagnostic {
	file := r.file
	if file == "" {
		file = fallbackFile
	}

	items := make([]JSONDiagnostic, 0, len(r.atoms))
	for _, a := range r.atoms {
		code := a.Code
		if code == "" {
			code = "E000"
		}
		items = append(items, JSONDiagnostic{
			Code:       code,
			File:       file,
			Line:       a.Line,
			Col:        a.Column,
			Message:    a.Message,
			Suggestion: a.Suggestion,
			Severity:   a.Severity.Key(),
		})
	}

	// 稳定排序：行 → 列 → 错误码；同位置保留报告内的因果链顺序。
	slices.SortStableFunc(items, func(a, b JSONDiagnostic) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		if a.Col != b.Col {
			return a.Col - b.Col
		}
		switch {
		case a.Code < b.Code:
			return -1
		case a.Code > b.Code:
			return 1
		default:
			return 0
		}
	})
	return items
}

// WriteJSON 把诊断写成 JSON 数组（两空格缩进 + 末尾换行）。
// 空报告写出 []\n；编码失败会返回错误，由调用方决定退出码。
func (r *Report) WriteJSON(w io.Writer, fallbackFile string) error {
	items := r.JSONDiagnostics(fallbackFile)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // 保留中文与 <>& 原样，便于人眼复核
	enc.SetIndent("", "  ")
	return enc.Encode(items)
}

// JSONBytes 返回诊断的 JSON 编码（含末尾换行），便于测试与嵌入。
func (r *Report) JSONBytes(fallbackFile string) ([]byte, error) {
	var buf bytes.Buffer
	if err := r.WriteJSON(&buf, fallbackFile); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
