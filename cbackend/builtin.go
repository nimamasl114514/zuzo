package cbackend

import (
	"fmt"
	"strings"

	"zuzo/ast"
)

// genBuiltinCall 处理内置函数。返回 (C 表达式, 是否命中, error)。
// 语句型内置（打印/推入）会直接 emit 语句，表达式位置返回 z_null()。
func (g *Gen) genBuiltinCall(name string, n *ast.CallExpression) (string, bool, error) {
	args := n.Arguments
	if len(n.NamedArgs) > 0 {
		return "", true, fmt.Errorf("%s(): 具名参数暂不支持（计划第22轮）", name)
	}

	// 求值全部参数（保持从左到右副作用顺序）
	vals := make([]string, 0, len(args))
	for _, a := range args {
		v, err := g.genExpr(a)
		if err != nil {
			return "", true, err
		}
		vals = append(vals, v)
	}
	need := func(min, max int) error {
		if len(vals) < min || (max >= 0 && len(vals) > max) {
			return fmt.Errorf("%s() 参数数量不匹配: 得到 %d 个", name, len(vals))
		}
		return nil
	}

	switch name {
	case "打印":
		g.line("z_print(%d, (ZVal[]){%s});", len(vals), strings.Join(vals, ", "))
		return "z_null()", true, nil

	case "长度":
		if err := need(1, 1); err != nil {
			return "", true, err
		}
		return "z_len(" + vals[0] + ")", true, nil

	case "推入":
		if err := need(2, 2); err != nil {
			return "", true, err
		}
		g.line("z_array_push(%s, %s);", vals[0], vals[1])
		return "z_null()", true, nil

	case "连接":
		if err := need(1, 2); err != nil {
			return "", true, err
		}
		sep := "z_str(\"\")"
		if len(vals) == 2 {
			sep = vals[1]
		}
		return fmt.Sprintf("z_join(%s, %s)", vals[0], sep), true, nil

	case "字符串":
		if err := need(1, 1); err != nil {
			return "", true, err
		}
		return "z_to_str(" + vals[0] + ")", true, nil

	case "整数":
		if err := need(1, 1); err != nil {
			return "", true, err
		}
		return "z_to_int(" + vals[0] + ")", true, nil

	case "浮点数":
		if err := need(1, 1); err != nil {
			return "", true, err
		}
		return "z_to_float(" + vals[0] + ")", true, nil

	case "类型":
		if err := need(1, 1); err != nil {
			return "", true, err
		}
		return "z_type_of(" + vals[0] + ")", true, nil

	case "包含":
		if err := need(2, 2); err != nil {
			return "", true, err
		}
		return fmt.Sprintf("z_contains(%s, %s)", vals[0], vals[1]), true, nil
	}
	return "", false, nil
}

// textBuiltins 文本命名空间 → C 运行时函数映射
var textBuiltins = map[string]struct {
	Fn  string
	Min int
	Max int
}{
	"大写":    {"zs_upper", 1, 1},
	"小写":    {"zs_lower", 1, 1},
	"去空白":   {"zs_trim", 1, 1},
	"替换":    {"zs_replace", 3, 3},
	"分割":    {"zs_split", 2, 2},
	"重复":    {"zs_repeat", 2, 2},
	"查找":    {"zs_find", 2, 2},
	"包含":    {"zs_contains", 2, 2},
	"前缀":    {"zs_starts", 2, 2},
	"后缀":    {"zs_ends", 2, 2},
	"比较":    {"zs_compare", 2, 2},
	"子串":    {"zs_substr", 3, 3},
	"按行分割":  {"zs_lines", 1, 1},
	"连接":    {"zs_join_arr", 2, 2},
}

// genTextBuiltin 处理 `文本.成员(...)` 形式的调用
func (g *Gen) genTextBuiltin(name string, n *ast.CallExpression) (string, bool, error) {
	spec, ok := textBuiltins[name]
	if !ok {
		return "", false, nil
	}
	if len(n.Arguments) < spec.Min || len(n.Arguments) > spec.Max {
		return "", true, fmt.Errorf("文本.%s() 参数数量不匹配: 得到 %d 个", name, len(n.Arguments))
	}
	var vals []string
	for _, a := range n.Arguments {
		v, err := g.genExpr(a)
		if err != nil {
			return "", true, err
		}
		vals = append(vals, v)
	}
	return fmt.Sprintf("%s(%s)", spec.Fn, strings.Join(vals, ", ")), true, nil
}
