package mix

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Lang 混合文件中的语言归属
type Lang int

const (
	// LangZuzo zuzo 中文语法块
	LangZuzo Lang = iota
	// LangPy Python 语法块
	LangPy
)

func (l Lang) String() string {
	if l == LangPy {
		return "Python"
	}
	return "zuzo"
}

// 块标记：整行出现，大小写不敏感，行首可有空白
const (
	MarkerZuzo = "##ZUZO"
	MarkerPy   = "##PY"
)

// Block 一个语言块
type Block struct {
	Lang      Lang
	StartLine int    // 原文中块内第一行的行号（1 基）
	EndLine   int    // 原文中块内最后一行的行号（含）
	Raw       string // 块内原文（不含行号填充）
	Src       string // 已用空行填充到原文件行号的源码，直接喂给对应语言的词法器
}

// SplitError 切块错误（带原文件行号）
type SplitError struct {
	Line int
	Msg  string
}

func (e *SplitError) Error() string {
	return fmt.Sprintf("第 %d 行：%s", e.Line, e.Msg)
}

// IsMixFile 判断是否为混合源文件后缀
func IsMixFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".hypy")
}

// Split 把 .hypy 源码切成按语言归属的顺序块。
//
// 规则：
//   - 整行（忽略首尾空白）为 ##ZUZO 或 ##PY 时切换当前块语言；
//   - 首个标记之前只允许空行与注释（// 与 # 开头），否则报错；
//   - 完全没有标记时整文件按 zuzo 执行（宽容回退）；
//   - 每个块的 Src 前置等量空行，使块内语句的行号等于原文件行号（错误定位不偏移）。
func Split(src string) ([]Block, error) {
	src = strings.TrimPrefix(src, "\ufeff")
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	lines := strings.Split(src, "\n")

	var blocks []Block
	lang := LangZuzo
	started := false
	start := 1
	bareLine := 0 // 首个标记之前的裸代码行（>0 且后续出现标记 = 报错）
	buf := make([]string, 0, len(lines))

	flush := func(end int) {
		raw := strings.Join(buf, "\n")
		buf = buf[:0]
		if strings.TrimSpace(raw) == "" {
			return
		}
		blocks = append(blocks, Block{
			Lang:      lang,
			StartLine: start,
			EndLine:   end,
			Raw:       raw,
			Src:       strings.Repeat("\n", start-1) + raw,
		})
	}

	for i, line := range lines {
		ln := i + 1
		if l, ok := markerLang(line); ok {
			if bareLine > 0 {
				return nil, &SplitError{
					Line: bareLine,
					Msg:  "首个块标记（" + MarkerZuzo + " / " + MarkerPy + "）之前不允许出现代码",
				}
			}
			if started {
				flush(ln - 1)
			}
			started = true
			lang = l
			start = ln + 1
			continue
		}
		if !started {
			if isPreamble(line) {
				continue
			}
			if bareLine == 0 {
				bareLine = ln
			}
			continue
		}
		buf = append(buf, line)
	}

	if !started {
		// 全程没有块标记：整文件按 zuzo 执行（宽容回退）
		return []Block{{Lang: LangZuzo, StartLine: 1, EndLine: len(lines), Raw: src, Src: src}}, nil
	}
	flush(len(lines))
	return blocks, nil
}

// markerLang 判定整行是否为块标记
func markerLang(line string) (Lang, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "##") {
		return 0, false
	}
	switch strings.ToUpper(strings.TrimSpace(t[2:])) {
	case "ZUZO", "HY":
		return LangZuzo, true
	case "PY", "PYTHON":
		return LangPy, true
	}
	return 0, false
}

// isPreamble 首块之前允许的行：空行、// 注释、# 注释
func isPreamble(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "#")
}
