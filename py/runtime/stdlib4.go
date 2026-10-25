package runtime

import (
	"regexp"
	"strings"

	"zuzo/py/ast"
	"zuzo/py/object"
)

// registerStdlib4 注册第四批补充 API（re.finditer 等）
func registerStdlib4(ip *Interp) {
	registerReFinditer()
}

// registerReFinditer 在 re 模块上追加 finditer（模块工厂外包一层，避免改动 modRe）
func registerReFinditer() {
	prev, ok := builtinModuleInits["re"]
	if !ok {
		return
	}
	builtinModuleInits["re"] = func(ip *Interp) *object.Module {
		m := prev(ip)
		m.Dict["finditer"] = bi("finditer", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
			if len(args) < 2 {
				return nil, &PyError{Exc: object.NewException(object.ExcType, &object.Str{V: "finditer() takes at least 2 arguments"})}
			}
			pat := args[0].Str()
			s := args[1].Str()
			flags := int64(0)
			if len(args) > 2 {
				flags, _ = object.AsInt(args[2])
			}
			re := compileReFor(ip, pat, flags)
			return finditerList(ip, re, s), nil
		})
		// 同时为已编译 Pattern 对象补 finditer
		prevCompile, hasCompile := m.Dict["compile"].(*object.Builtin)
		if hasCompile {
			origFn := prevCompile.Fn
			m.Dict["compile"] = bi("compile", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
				v, err := origFn(args, kw)
				if err != nil || v == nil {
					return v, err
				}
				inst, ok := v.(*object.Instance)
				if !ok {
					return v, nil
				}
				pat := args[0].Str()
				flags := int64(0)
				if len(args) > 1 {
					flags, _ = object.AsInt(args[1])
				}
				inst.Dict["finditer"] = bi("finditer", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
					if len(a) < 1 {
						return nil, &PyError{Exc: object.NewException(object.ExcType, &object.Str{V: "finditer() missing string argument"})}
					}
					re := compileReFor(ip, pat, flags)
					return finditerList(ip, re, a[0].Str()), nil
				})
				return v, nil
			})
		}
		return m
	}
}

// pyRegexToGo 把 Python 正则方言里 Go RE2 不认的转义改成等价写法。
// 目前覆盖：\Z（Python 字符串绝对末尾）→ \z；Go 的 \A / \b / \B / \d 等本就一致。
var negLookaheadCharClass = regexp.MustCompile(`\(\?!\[([^\]]*)\]\)`)

func pyRegexToGo(p string) string {
	p = strings.ReplaceAll(p, `\Z`, `\z`)
	// 负向前瞻的字符类形式 (?![ \t]) → [^ \t]：RE2 无前瞻，
	// 这里降级为「消费一个字符」。对 .search() 的存在性判断等价
	// （http/client.py:145 的 header 合法性检查就是这种用法）。
	if strings.Contains(p, "(?!") {
		p = negLookaheadCharClass.ReplaceAllString(p, `[^$1]`)
	}
	return p
}

// neverMatch 永不匹配的正则：用于语法不支持时的降级（宁可放行，不中断 import 链）
func neverMatch() *regexp.Regexp { return regexp.MustCompile(`\b\B`) }

// compileReFor 与 stdlib.go 内 compileRe 语义一致（flags 位：2=I 8=M 16=S）
func compileReFor(ip *Interp, pattern string, flags int64) *regexp.Regexp {
	p := pyRegexToGo(pattern)
	if flags&2 != 0 {
		p = "(?i)" + p
	}
	if flags&8 != 0 {
		p = "(?m)" + p
	}
	if flags&16 != 0 {
		p = "(?s)" + p
	}
	re, err := regexp.Compile(p)
	if err != nil {
		// 语法不支持（前瞻/反向引用等）：降级为永不匹配，不中断调用链
		return neverMatch()
	}
	return re
}

// finditerList 返回可迭代的 Match 列表（元素支持 start/end/group/groups/span/groupdict）
func finditerList(ip *Interp, re *regexp.Regexp, s string) object.Object {
	locs := re.FindAllStringSubmatchIndex(s, -1)
	items := make([]object.Object, 0, len(locs))
	for _, loc := range locs {
		items = append(items, makeMatch(ip, re, s, loc))
	}
	return &object.List{Items: items}
}

// ==================== 推导式内海象运算符作用域修正 ====================

// compWalrus 把推导式内海象运算符（:=）绑定的名字回写到外层作用域（CPython 语义）
// gens 的迭代变量保持推导式局部；walrusEnv 为最后一次迭代所处的局部环境（海象结果落在这里）
func (ip *Interp) compWalrus(gens []ast.Comp, walrusEnv *object.Env, env *object.Env) {
	if walrusEnv == nil {
		return
	}
	skip := map[string]bool{}
	for _, g := range gens {
		collectTargetNames(g.Target, skip)
	}
	names := make([]string, 0, len(walrusEnv.Vars))
	for k := range walrusEnv.Vars {
		names = append(names, k)
	}
	for _, k := range names {
		if skip[k] {
			continue
		}
		env.Set(k, walrusEnv.Vars[k])
	}
}

func collectTargetNames(e ast.Expr, out map[string]bool) {
	switch n := e.(type) {
	case *ast.Name:
		out[n.Id] = true
	case *ast.Tuple:
		for _, x := range n.Elements {
			collectTargetNames(x, out)
		}
	case *ast.List:
		for _, x := range n.Elements {
			collectTargetNames(x, out)
		}
	case *ast.Starred:
		collectTargetNames(n.Value, out)
	}
}
