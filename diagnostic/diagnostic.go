package diagnostic

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// ============================================================
// 活字印刷式诊断系统 v2
// ============================================================
//
// 核心思想：把每个错误拆成独立的"活字块"（Atom），用 Cause 指针
// 形成因果链，最终排版成树形诊断报告。
//
//   根本原因 [E001] ──→ 连带影响 [E102] ──→ 类型后果 [E203]
//        |                                    |
//   "缺少右括号"        "解析偏移"           "类型推断失效"
//
// v2 增强：
//   - Severity 严重级别（错误/警告/提示）
//   - 源码上下文展示（行号 + 源码行 + 箭头定位）
//   - 更紧凑的渲染格式
// ============================================================

// Severity 严重级别
type Severity int

const (
	SeverityError   Severity = iota // 错误 — 必须修复
	SeverityWarning                 // 警告 — 建议修复
	SeverityNote                    // 提示 — 仅供参考
	SeverityInfo                    // 信息 — 附加说明
)

func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "错误"
	case SeverityWarning:
		return "警告"
	case SeverityNote:
		return "提示"
	case SeverityInfo:
		return "信息"
	default:
		return "未知"
	}
}

func (s Severity) Icon() string {
	switch s {
	case SeverityError:
		return "✘"
	case SeverityWarning:
		return "▲"
	case SeverityNote:
		return "●"
	case SeverityInfo:
		return "ℹ"
	default:
		return "?"
	}
}

// Level 活字块级别
type Level int

const (
	LevelRoot    Level = iota // 根本原因 — 导致连锁反应的第一个错误
	LevelCascade              // 连带影响 — 因根因而产生的误报/误判
	LevelType                 // 类型后果 — 语法错误导致的类型推断失败
)

func (l Level) String() string {
	switch l {
	case LevelRoot:
		return "根因"
	case LevelCascade:
		return "连带"
	case LevelType:
		return "类型"
	default:
		return "未知"
	}
}

func (l Level) Icon() string {
	switch l {
	case LevelRoot:
		return "◆"
	case LevelCascade:
		return "├"
	case LevelType:
		return "╰"
	default:
		return "?"
	}
}

// Atom 一个活字 = 一个独立的错误碎片
type Atom struct {
	Level      Level
	Severity   Severity // 严重级别
	Code       string   // 错误码，如 "E001"
	Line       int
	Column     int
	Message    string
	Suggestion string
	// 关键：指向根源活字（nil = 自身就是根源）
	Cause *Atom
}

// Report 诊断报告 = 多个活字的组合
type Report struct {
	atoms []*Atom
	// 按行索引的根因快查表（用于类型检查器查找同行的根因）
	rootByLine map[int]*Atom
	// 源码行缓存（按行号索引，用于显示源码上下文）
	sourceLines map[int]string
}

// New 创建空诊断报告
func New() *Report {
	return &Report{
		rootByLine:  make(map[int]*Atom),
		sourceLines: make(map[int]string),
	}
}

// SetSource 设置待检查的源码（用于错误报告中显示源码上下文）
// 传入原始源代码字符串，内部按行分割缓存
func (r *Report) SetSource(source string) {
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		r.sourceLines[i+1] = line // 行号从 1 开始
	}
}

// AddRoot 添加一个根因活字，返回它的指针（供后续 Cascade 引用）
func (r *Report) AddRoot(code string, line, col int, msg, suggestion string) *Atom {
	a := &Atom{
		Level:      LevelRoot,
		Severity:   SeverityError,
		Code:       code,
		Line:       line,
		Column:     col,
		Message:    msg,
		Suggestion: suggestion,
	}
	r.atoms = append(r.atoms, a)
	// 如果这行已有根因，保留第一个（通常是最根本的）
	if r.rootByLine[line] == nil {
		r.rootByLine[line] = a
	}
	return a
}

// AddRootWithSeverity 添加一个带严重级别的根因活字
func (r *Report) AddRootWithSeverity(severity Severity, code string, line, col int, msg, suggestion string) *Atom {
	a := &Atom{
		Level:      LevelRoot,
		Severity:   severity,
		Code:       code,
		Line:       line,
		Column:     col,
		Message:    msg,
		Suggestion: suggestion,
	}
	r.atoms = append(r.atoms, a)
	if r.rootByLine[line] == nil {
		r.rootByLine[line] = a
	}
	return a
}

// AddCascade 添加一个连带影响活字，关联到指定的根因
func (r *Report) AddCascade(cause *Atom, code string, line, col int, msg, suggestion string) *Atom {
	a := &Atom{
		Level:      LevelCascade,
		Severity:   SeverityError,
		Code:       code,
		Line:       line,
		Column:     col,
		Message:    msg,
		Suggestion: suggestion,
		Cause:      cause,
	}
	r.atoms = append(r.atoms, a)
	return a
}

// AddType 添加一个类型后果活字，关联到指定的根因
func (r *Report) AddType(cause *Atom, code string, line, col int, msg, suggestion string) *Atom {
	a := &Atom{
		Level:      LevelType,
		Severity:   SeverityError,
		Code:       code,
		Line:       line,
		Column:     col,
		Message:    msg,
		Suggestion: suggestion,
		Cause:      cause,
	}
	r.atoms = append(r.atoms, a)
	return a
}

// FindRootOnLine 查找指定行的根因活字（供类型检查器使用）
func (r *Report) FindRootOnLine(line int) *Atom {
	return r.rootByLine[line]
}

// HasErrors 是否有错误
func (r *Report) HasErrors() bool {
	return len(r.atoms) > 0
}

// Count 返回错误总数
func (r *Report) Count() int {
	return len(r.atoms)
}

// Atoms 返回所有活字（按原始顺序）
func (r *Report) Atoms() []*Atom {
	return r.atoms
}

// ============================================================
// 渲染器：活字排版 v2
// ============================================================

// Render 将诊断报告排版为树形因果链输出
func (r *Report) Render() string {
	if len(r.atoms) == 0 {
		return ""
	}

	var buf strings.Builder

	// 统计各类错误数量
	errCount := 0
	warnCount := 0
	for _, a := range r.atoms {
		switch a.Severity {
		case SeverityError:
			errCount++
		case SeverityWarning:
			warnCount++
		}
	}

	// 标题
	summary := fmt.Sprintf("发现 %d 个问题", len(r.atoms))
	if errCount > 0 {
		summary += fmt.Sprintf("（%d 个错误", errCount)
		if warnCount > 0 {
			summary += fmt.Sprintf(", %d 个警告", warnCount)
		}
		summary += "）"
	} else if warnCount > 0 {
		summary += fmt.Sprintf("（%d 个警告）", warnCount)
	}
	buf.WriteString("━━━ " + summary + " ━━━\n")

	// 分组：按"因果链"分组
	processed := make(map[*Atom]bool)
	var groups [][]*Atom

	// 先找出所有根因
	var roots []*Atom
	for _, a := range r.atoms {
		if a.Cause == nil {
			roots = append(roots, a)
		}
	}

	// 对每个根因，收集其下游所有活字
	for _, root := range roots {
		if processed[root] {
			continue
		}
		group := r.collectCausalChain(root)
		groups = append(groups, group)
		for _, a := range group {
			processed[a] = true
		}
	}

	// 也处理孤立的非根因活字
	for _, a := range r.atoms {
		if !processed[a] {
			groups = append(groups, []*Atom{a})
		}
	}

	// 按行号排序各组
	slices.SortFunc(groups, func(a, b []*Atom) int {
		if len(a) == 0 || len(b) == 0 {
			return 0
		}
		return a[0].Line - b[0].Line
	})

	// 逐组渲染
	for gi, group := range groups {
		if gi > 0 {
			buf.WriteString("───\n")
		}
		r.renderGroup(&buf, group)
	}

	return buf.String()
}

// RenderWithSource 带源码上下文的渲染（兼容旧版，自动使用已设置的 sourceLines）
func (r *Report) RenderWithSource(source string) string {
	r.SetSource(source)
	return r.Render()
}

// CollectCausalChain 从一个根因出发，收集所有因果链上的活字（导出版本，用于测试）
func (r *Report) CollectCausalChain(root *Atom) []*Atom {
	return r.collectCausalChain(root)
}

// collectCausalChain 从一个根因出发，收集所有因果链上的活字
func (r *Report) collectCausalChain(root *Atom) []*Atom {
	var chain []*Atom
	chain = append(chain, root)

	// 广度优先搜索所有以 root 为因的活字
	queue := []*Atom{root}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, a := range r.atoms {
			if a.Cause == current && a != current {
				chain = append(chain, a)
				queue = append(queue, a)
			}
		}
	}

	// 在链内按 Level 再按行号排序
	slices.SortFunc(chain[1:], func(a, b *Atom) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return int(a.Level) - int(b.Level)
	})

	return chain
}

// renderGroup 渲染一个因果链组
func (r *Report) renderGroup(buf *strings.Builder, group []*Atom) {
	if len(group) == 0 {
		return
	}

	for i, atom := range group {
		if i == 0 {
			// 根因 — 显示完整错误信息 + 源码上下文
			r.renderAtom(buf, atom, 0, true)
		} else {
			// 下游 — 缩进显示，也显示源码上下文
			r.renderAtom(buf, atom, i, false)
		}
	}
}

// renderAtom 渲染单个活字块
func (r *Report) renderAtom(buf *strings.Builder, atom *Atom, depth int, isRoot bool) {
	indent := strings.Repeat("  ", depth)

	// 图标 + 级别 + 严重度 + 错误码
	severityIcon := atom.Severity.Icon()
	levelStr := atom.Level.String()
	buf.WriteString(fmt.Sprintf("%s%s %s [%s] %s\n",
		indent, severityIcon, levelStr, atom.Code, atom.Message))

	// 源码上下文（如果有源码行缓存）
	if atom.Line > 0 {
		if sourceLine, ok := r.sourceLines[atom.Line]; ok {
			// 行号（右对齐到 4 位）
			lineNum := strconv.Itoa(atom.Line)
			buf.WriteString(fmt.Sprintf("%s  %4s │ %s\n", indent, lineNum, sourceLine))
			// 箭头定位
			if atom.Column > 0 && atom.Column <= len(sourceLine)+1 {
				arrow := strings.Repeat(" ", atom.Column-1) + "^"
				// 如果有建议，在箭头后显示
				if atom.Suggestion != "" {
					arrow += " " + atom.Suggestion
				}
				buf.WriteString(fmt.Sprintf("%s       │ %s\n", indent, arrow))
			} else if atom.Suggestion != "" {
				buf.WriteString(fmt.Sprintf("%s       │ %s\n", indent, "  "+atom.Suggestion))
			}
		} else {
			// 没有源码缓存，显示位置信息
			pos := fmt.Sprintf("行 %d", atom.Line)
			if atom.Column > 0 {
				pos += fmt.Sprintf("，列 %d", atom.Column)
			}
			buf.WriteString(fmt.Sprintf("%s  位置: %s\n", indent, pos))
			if atom.Suggestion != "" {
				buf.WriteString(fmt.Sprintf("%s  建议: %s\n", indent, atom.Suggestion))
			}
		}
	} else if atom.Suggestion != "" {
		buf.WriteString(fmt.Sprintf("%s  建议: %s\n", indent, atom.Suggestion))
	}

	// 因果链提示
	if atom.Cause != nil && !isRoot {
		buf.WriteString(fmt.Sprintf("%s  ↑ 因: %s [%s]\n", indent, atom.Cause.Message, atom.Cause.Code))
	}
}

// ============================================================
// 便捷函数
// ============================================================

// SimpleReport 从简单的错误消息列表创建报告（向后兼容）
func SimpleReport(msgs []string) *Report {
	r := New()
	for i, msg := range msgs {
		r.AddRoot(fmt.Sprintf("E%03d", i+1), 0, 0, msg, "")
	}
	return r
}