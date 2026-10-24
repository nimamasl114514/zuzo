package mix

// zuzo_sap.go — 混合模式下 zuzo 块的块级 SAP 编译（第6轮性能优化）
//
// 背景：混合执行器为了让跨块变量共享，此前统一关闭 SAP（Tree-Walk），
// 导致 zuzo 块里的大循环比单语言慢 10 倍以上
// （第6轮实测：1e6 累加 单语言 SAP 24.3ms vs 混合块 252ms）。
//
// 方案：块级 SAP —— 每个 zuzo 块单独做类型推断 + SAP 编译 + 独立 Environment，
// 块前只把本块引用的名字从全局环境拉进槽位，块后只把本块顶层定义写回全局。
// 这样跨块语义仍是「按名共享」，而块内热路径恢复槽位/原生循环。
//
// 启用条件（保守，避免为小片段付编译成本 / 破坏闭包语义）：
//   - 块内存在循环（for/while，含嵌套）
//   - 且块内没有函数字面量（函数会捕获 Environment 指针，临时环境会改变闭包可见性）

import (
	"fmt"
	"io"
	"strings"

	"zuzo/ast"
	"zuzo/evaluator"
	"zuzo/infer"
	"zuzo/object"
)

// scanSAPNeeds 递归扫描 AST：是否有循环、是否有函数字面量
func scanSAPNeeds(node ast.Node) (hasLoop, hasFunc bool) {
	if node == nil {
		return false, false
	}
	switch n := node.(type) {
	case *ast.Program:
		for _, s := range n.Statements {
			l, f := scanSAPNeeds(s)
			hasLoop = hasLoop || l
			hasFunc = hasFunc || f
		}
	case *ast.ForStatement:
		hasLoop = true
		l, f := scanSAPNeeds(n.Body)
		hasLoop = hasLoop || l
		hasFunc = hasFunc || f
	case *ast.WhileStatement:
		hasLoop = true
		l, f := scanSAPNeeds(n.Body)
		hasLoop = hasLoop || l
		hasFunc = hasFunc || f
	case *ast.BlockStatement:
		for _, s := range n.Statements {
			l, f := scanSAPNeeds(s)
			hasLoop = hasLoop || l
			hasFunc = hasFunc || f
		}
	case *ast.ExpressionStatement:
		l, f := scanSAPNeeds(n.Expression)
		hasLoop = hasLoop || l
		hasFunc = hasFunc || f
	case *ast.IfExpression:
		if n.Condition != nil {
			l, f := scanSAPNeeds(n.Condition)
			hasLoop = hasLoop || l
			hasFunc = hasFunc || f
		}
		l1, f1 := scanSAPNeeds(n.Consequence)
		hasLoop = hasLoop || l1
		hasFunc = hasFunc || f1
		l2, f2 := scanSAPNeeds(n.Alternative)
		hasLoop = hasLoop || l2
		hasFunc = hasFunc || f2
	case *ast.FunctionLiteral:
		hasFunc = true
	}
	return hasLoop, hasFunc
}

// blockWantsSAP 判断该块是否值得走块级 SAP
// 先用字符串预筛（O(源码长度)）再做 AST 扫描，避免对无循环的大块付 O(节点数) 扫描成本
func blockWantsSAP(program *ast.Program, src string) bool {
	if !strings.Contains(src, "对于") && !strings.Contains(src, "当") &&
		!strings.Contains(src, "for ") && !strings.Contains(src, "while ") {
		return false
	}
	hasLoop, hasFunc := scanSAPNeeds(program)
	return hasLoop && !hasFunc
}

// runZuzoBlockSAP 用块级 SAP 执行一个 zuzo 块
// 返回 (退出码, 是否已处理)。无法编译时返回 handled=false 交由 Tree-Walk 原路径。
func runZuzoBlockSAP(b *Bridge, blk Block, program *ast.Program, refs, defs map[string]bool, errw io.Writer) (int, bool) {
	inf := infer.New()
	inf.Infer(program)
	slotMap := inf.GetSlotMap()
	if slotMap == nil {
		return 0, false
	}

	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)

	env := evaluator.NewSAPEnvironment(len(slotMap), slotMap)

	// 块前：只拉取本块引用的名字（惰性，避免全量深拷贝）
	for name := range refs {
		if v, ok := b.Env.Get(name); ok {
			env.Set(name, v)
		}
	}

	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	code := 0
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(errw, "zuzo 运行时错误（块起始第 %d 行）：%v\n", blk.StartLine, r)
				code = 1
			}
		}()
		result := evaluator.Eval(program, env)
		if errObjVal, ok := result.(*object.Error); ok {
			fmt.Fprintln(errw, errObjVal.Inspect())
			code = 1
		}
	}()

	if code == 0 {
		// 块后：只把本块顶层定义写回全局环境（供 Python 块与后续 zuzo 块使用）
		for name := range defs {
			if v, ok := env.Get(name); ok {
				b.Env.Set(name, v)
			}
		}
		b.AfterZuzo(defs)
	}
	return code, true
}