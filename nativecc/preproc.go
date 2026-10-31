package nativecc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Macro 宏定义（对象宏 / 函数宏）
type Macro struct {
	Name   string
	Params []string
	Body   []Token
	IsFunc bool
}

// Preprocessor 极简 C 预处理器：支持 #include、对象宏、#ifdef/#ifndef/#if defined/#else/#elif/#endif
type Preprocessor struct {
	macros      map[string]*Macro
	once        map[string]bool
	builtin     map[string]string
	includeDirs []string
	out         []Token
	depth       int
}

// NewPreprocessor 构造预处理器；builtin 为内置系统头（stdio.h → 内容）
func NewPreprocessor(builtin map[string]string, includeDirs []string) *Preprocessor {
	p := &Preprocessor{
		macros:      map[string]*Macro{},
		once:        map[string]bool{},
		builtin:     builtin,
		includeDirs: includeDirs,
	}
	// 预定义宏
	for _, kv := range [][2]string{
		{"_WIN32", "1"}, {"__STDC__", "1"}, {"_M_X64", "1"},
		{"__x86_64__", "1"}, {"_WIN64", "1"}, {"__STDC_VERSION__", "201112L"},
	} {
		p.macros[kv[0]] = &Macro{Name: kv[0], Body: []Token{{Kind: TInt, Text: kv[1], IVal: 1}}}
	}
	return p
}

type condFrame struct {
	parentActive bool
	active       bool
	taken        bool
}

// ProcessFile 处理一个源文件，结果追加到 p.out
func (p *Preprocessor) ProcessFile(file, src string) error {
	if p.depth > 64 {
		return fmt.Errorf("预处理 #include 深度超限（>64）：%s", file)
	}
	if p.once[file] {
		return nil
	}
	p.depth++
	defer func() { p.depth-- }()

	clean := stripComments(src)
	lines := strings.Split(clean, "\n")

	var stack []condFrame
	active := func() bool {
		if len(stack) == 0 {
			return true
		}
		return stack[len(stack)-1].active
	}

	// 行拼接：反斜杠结尾续行
	type logicalLine struct {
		text string
		line int
	}
	var logical []logicalLine
	for i := 0; i < len(lines); i++ {
		t := lines[i]
		startLine := i + 1
		for strings.HasSuffix(t, "\\") && i+1 < len(lines) {
			t = t[:len(t)-1] + lines[i+1]
			i++
		}
		logical = append(logical, logicalLine{text: t, line: startLine})
	}

	for _, ll := range logical {
		trimmed := strings.TrimSpace(ll.text)
		if trimmed == "" {
			continue
		}
		if trimmed[0] == '#' {
			if err := p.directive(file, ll.line, trimmed, &stack); err != nil {
				return err
			}
			continue
		}
		if !active() {
			continue
		}
		toks, err := p.expandLine(file, ll.line, ll.text)
		if err != nil {
			return err
		}
		p.out = append(p.out, toks...)
	}
	return nil
}

// directive 处理一行预处理指令
func (p *Preprocessor) directive(file string, line int, s string, stack *[]condFrame) error {
	body := strings.TrimSpace(s[1:])
	name := body
	rest := ""
	if i := strings.IndexAny(body, " \t"); i >= 0 {
		name = body[:i]
		rest = strings.TrimSpace(body[i+1:])
	}
	curActive := len(*stack) == 0 || (*stack)[len(*stack)-1].active

	switch name {
	case "include":
		if !curActive {
			return nil
		}
		return p.include(file, line, rest)
	case "define":
		if !curActive {
			return nil
		}
		return p.define(file, line, rest)
	case "undef":
		if !curActive {
			return nil
		}
		delete(p.macros, strings.TrimSpace(rest))
	case "ifdef", "ifndef":
		cond := p.isDefined(strings.TrimSpace(rest))
		if name == "ifndef" {
			cond = !cond
		}
		parent := curActive
		*stack = append(*stack, condFrame{parentActive: parent, active: parent && cond, taken: cond})
	case "if":
		v, err := p.evalIf(rest)
		if err != nil {
			return fmt.Errorf("%s:%d: #if 求值失败: %v", file, line, err)
		}
		parent := curActive
		*stack = append(*stack, condFrame{parentActive: parent, active: parent && v, taken: v})
	case "elif":
		if len(*stack) == 0 {
			return fmt.Errorf("%s:%d: #elif 无匹配 #if", file, line)
		}
		f := &(*stack)[len(*stack)-1]
		v, err := p.evalIf(rest)
		if err != nil {
			return fmt.Errorf("%s:%d: #elif 求值失败: %v", file, line, err)
		}
		f.active = f.parentActive && !f.taken && v
		f.taken = f.taken || v
	case "else":
		if len(*stack) == 0 {
			return fmt.Errorf("%s:%d: #else 无匹配 #if", file, line)
		}
		f := &(*stack)[len(*stack)-1]
		f.active = f.parentActive && !f.taken
		f.taken = true
	case "endif":
		if len(*stack) == 0 {
			return fmt.Errorf("%s:%d: #endif 无匹配 #if", file, line)
		}
		*stack = (*stack)[:len(*stack)-1]
	case "pragma":
		if strings.TrimSpace(rest) == "once" {
			p.once[file] = true
		}
	case "error":
		if curActive {
			return fmt.Errorf("%s:%d: #error %s", file, line, rest)
		}
	case "line", "warning", "":
		// 忽略
	default:
		// 未知指令：宽松忽略（生成代码里不会出现）
	}
	return nil
}

func (p *Preprocessor) isDefined(name string) bool {
	_, ok := p.macros[name]
	return ok
}

func (p *Preprocessor) include(fromFile string, line int, spec string) error {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return fmt.Errorf("%s:%d: #include 缺少文件名", fromFile, line)
	}
	if spec[0] == '<' {
		end := strings.IndexByte(spec, '>')
		if end < 0 {
			return fmt.Errorf("%s:%d: #include 尖括号未闭合", fromFile, line)
		}
		name := spec[1:end]
		content, ok := p.builtin[name]
		if !ok {
			// 未知系统头：当作空（宽松），避免阻塞
			return nil
		}
		return p.ProcessFile("<内置>:"+name, content)
	}
	if spec[0] == '"' {
		end := strings.IndexByte(spec[1:], '"')
		if end < 0 {
			return fmt.Errorf("%s:%d: #include 引号未闭合", fromFile, line)
		}
		name := spec[1 : 1+end]
		// 1) 同目录
		dir := filepath.Dir(fromFile)
		cand := filepath.Join(dir, name)
		if data, err := os.ReadFile(cand); err == nil {
			return p.ProcessFile(cand, string(data))
		}
		// 2) includeDirs
		for _, d := range p.includeDirs {
			cand := filepath.Join(d, name)
			if data, err := os.ReadFile(cand); err == nil {
				return p.ProcessFile(cand, string(data))
			}
		}
		// 3) 内置头兜底
		if content, ok := p.builtin[name]; ok {
			return p.ProcessFile("<内置>:"+name, content)
		}
		return fmt.Errorf("%s:%d: 找不到头文件 %q", fromFile, line, name)
	}
	return fmt.Errorf("%s:%d: 非法 #include 形式 %q", fromFile, line, spec)
}

func (p *Preprocessor) define(file string, line int, rest string) error {
	i := 0
	for i < len(rest) && (isIdentCont(rest[i])) {
		i++
	}
	if i == 0 {
		return fmt.Errorf("%s:%d: #define 缺少宏名", file, line)
	}
	name := rest[:i]
	m := &Macro{Name: name}
	if i < len(rest) && rest[i] == '(' {
		// 函数宏
		depth := 1
		j := i + 1
		for j < len(rest) && depth > 0 {
			switch rest[j] {
			case '(':
				depth++
			case ')':
				depth--
			}
			j++
		}
		if depth != 0 {
			return fmt.Errorf("%s:%d: #define 参数表未闭合", file, line)
		}
		paramStr := rest[i+1 : j-1]
		m.IsFunc = true
		for _, ps := range strings.Split(paramStr, ",") {
			ps = strings.TrimSpace(ps)
			if ps != "" {
				m.Params = append(m.Params, ps)
			}
		}
		rest = strings.TrimSpace(rest[j:])
	} else {
		rest = strings.TrimSpace(rest[i:])
	}
	if rest != "" {
		toks, err := NewLexer(file, rest).Lex()
		if err != nil {
			return err
		}
		if len(toks) > 0 && toks[len(toks)-1].Kind == TEOF {
			toks = toks[:len(toks)-1]
		}
		m.Body = toks
	}
	p.macros[name] = m
	return nil
}

// evalIf 求值 #if 表达式（支持 defined()、整数、!、&&、||、比较）
func (p *Preprocessor) evalIf(expr string) (bool, error) {
	v, err := p.evalIfExpr(strings.TrimSpace(expr))
	if err != nil {
		return false, err
	}
	return v != 0, nil
}

func (p *Preprocessor) evalIfExpr(expr string) (int64, error) {
	toks, err := NewLexer("<#if>", expr).Lex()
	if err != nil {
		return 0, err
	}
	if len(toks) > 0 && toks[len(toks)-1].Kind == TEOF {
		toks = toks[:len(toks)-1]
	}
	pos := 0
	var parseOr func() (int64, error)
	var parseAnd func() (int64, error)
	var parseUnary func() (int64, error)
	var parseCmp func() (int64, error)
	var parseAtom func() (int64, error)

	parseAtom = func() (int64, error) {
		if pos >= len(toks) {
			return 0, fmt.Errorf("表达式意外结束")
		}
		t := toks[pos]
		if t.Is("(") {
			pos++
			v, err := parseOr()
			if err != nil {
				return 0, err
			}
			if pos >= len(toks) || !toks[pos].Is(")") {
				return 0, fmt.Errorf("缺少右括号")
			}
			pos++
			return v, nil
		}
		pos++
		switch t.Kind {
		case TInt:
			return t.IVal, nil
		case TIdent:
			if t.Text == "defined" {
				needParen := pos < len(toks) && toks[pos].Is("(")
				if needParen {
					pos++
				}
				if pos >= len(toks) {
					return 0, fmt.Errorf("defined 缺少标识符")
				}
				name := toks[pos].Text
				pos++
				if needParen {
					if pos >= len(toks) || !toks[pos].Is(")") {
						return 0, fmt.Errorf("defined( 未闭合")
					}
					pos++
				}
				if p.isDefined(name) {
					return 1, nil
				}
				return 0, nil
			}
			// 未定义的标识符按 0，已定义对象宏按宏体首个整数
			if m, ok := p.macros[t.Text]; ok && !m.IsFunc && len(m.Body) > 0 && m.Body[0].Kind == TInt {
				return m.Body[0].IVal, nil
			}
			return 0, nil
		case TChar:
			return t.IVal, nil
		}
		return 0, fmt.Errorf("无法识别的记号 %q", t.Text)
	}
	parseUnary = func() (int64, error) {
		if pos < len(toks) && toks[pos].Is("!") {
			pos++
			v, err := parseUnary()
			return b2i(v == 0), err
		}
		if pos < len(toks) && toks[pos].Is("-") {
			pos++
			v, err := parseUnary()
			return -v, err
		}
		return parseAtom()
	}
	parseCmp = func() (int64, error) {
		l, err := parseUnary()
		if err != nil {
			return 0, err
		}
		for pos < len(toks) {
			op := toks[pos].Text
			switch op {
			case "==", "!=", "<", ">", "<=", ">=":
				pos++
				r, err := parseUnary()
				if err != nil {
					return 0, err
				}
				switch op {
				case "==":
					l = b2i(l == r)
				case "!=":
					l = b2i(l != r)
				case "<":
					l = b2i(l < r)
				case ">":
					l = b2i(l > r)
				case "<=":
					l = b2i(l <= r)
				case ">=":
					l = b2i(l >= r)
				}
			default:
				return l, nil
			}
		}
		return l, nil
	}
	parseAnd = func() (int64, error) {
		l, err := parseCmp()
		if err != nil {
			return 0, err
		}
		for pos < len(toks) && toks[pos].Is("&&") {
			pos++
			r, err := parseCmp()
			if err != nil {
				return 0, err
			}
			l = b2i(l != 0 && r != 0)
		}
		return l, nil
	}
	parseOr = func() (int64, error) {
		l, err := parseAnd()
		if err != nil {
			return 0, err
		}
		for pos < len(toks) && toks[pos].Is("||") {
			pos++
			r, err := parseAnd()
			if err != nil {
				return 0, err
			}
			l = b2i(l != 0 || r != 0)
		}
		return l, nil
	}
	return parseOr()
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// expandLine 对一行源码做词法 + 宏展开
func (p *Preprocessor) expandLine(file string, line int, text string) ([]Token, error) {
	toks, err := NewLexer(file, text).Lex()
	if err != nil {
		return nil, err
	}
	if len(toks) > 0 && toks[len(toks)-1].Kind == TEOF {
		toks = toks[:len(toks)-1]
	}
	out := make([]Token, 0, len(toks))
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		t.Line = line
		t.File = file
		if t.Kind != TIdent {
			out = append(out, t)
			continue
		}
		m, ok := p.macros[t.Text]
		if !ok {
			out = append(out, t)
			continue
		}
		if m.IsFunc {
			// 函数宏：尝试收集实参
			if i+1 >= len(toks) || !toks[i+1].Is("(") {
				out = append(out, t)
				continue
			}
			depth := 0
			j := i + 1
			var args [][]Token
			var cur []Token
			for ; j < len(toks); j++ {
				tk := toks[j]
				if tk.Is("(") {
					depth++
					if depth == 1 {
						continue
					}
				}
				if tk.Is(")") {
					depth--
					if depth == 0 {
						args = append(args, cur)
						break
					}
				}
				if tk.Is(",") && depth == 1 {
					args = append(args, cur)
					cur = nil
					continue
				}
				cur = append(cur, tk)
			}
			if j >= len(toks) {
				out = append(out, t)
				continue
			}
			expanded, err := p.expandMacroBody(m, args)
			if err != nil {
				return nil, err
			}
			out = append(out, expanded...)
			i = j
			continue
		}
		// 对象宏
		expanded, err := p.expandMacroBody(m, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, expanded...)
	}
	return out, nil
}

func (p *Preprocessor) expandMacroBody(m *Macro, args [][]Token) ([]Token, error) {
	if p.depth > 64 {
		return nil, fmt.Errorf("宏展开深度超限:%s", m.Name)
	}
	if !m.IsFunc {
		return m.Body, nil
	}
	var out []Token
	for _, t := range m.Body {
		replaced := false
		for idx, pn := range m.Params {
			if t.Kind == TIdent && t.Text == pn {
				if idx < len(args) {
					out = append(out, args[idx]...)
				}
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, t)
		}
	}
	return out, nil
}

// stripComments 去掉注释（保留字符串/字符字面量内容与换行，行号不变）
func stripComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	i := 0
	for i < len(src) {
		c := src[i]
		if c == '"' || c == '\'' {
			quote := c
			b.WriteByte(c)
			i++
			for i < len(src) {
				b.WriteByte(src[i])
				if src[i] == '\\' && i+1 < len(src) {
					i++
					b.WriteByte(src[i])
					i++
					continue
				}
				if src[i] == quote {
					i++
					break
				}
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			for i < len(src) {
				if src[i] == '*' && i+1 < len(src) && src[i+1] == '/' {
					i += 2
					break
				}
				if src[i] == '\n' {
					b.WriteByte('\n')
				}
				i++
			}
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// Tokens 返回已累积的 token 流（末位补 EOF）
func (p *Preprocessor) Tokens() []Token {
	out := make([]Token, len(p.out))
	copy(out, p.out)
	if len(out) == 0 || out[len(out)-1].Kind != TEOF {
		out = append(out, Token{Kind: TEOF})
	}
	return out
}
