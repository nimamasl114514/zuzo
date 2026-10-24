package evaluator

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"zuzo/ast"
	"zuzo/object"
)

// shadow.go — 影子模式 (Shadow Execution)
//
// 核心思想：同时执行 SAP 优化路径和传统 Tree-Walk 路径，
// 对比两者的返回值和副作用（打印输出），任何不一致立刻 Panic。
//
// 这是防御编译器优化 Bug 的终极武器：
// 优化器再怎么"自作聪明"，只要结果和原生态执行不一致，瞬间暴露。
//
// 用法：
//   zuzo --shadow 文件.hy          启用影子模式（严格对比）
//   zuzo --shadow-lenient 文件.hy  影子模式（宽松对比，忽略时间戳/并发交错）
//   zuzo --no-sap 文件.hy          关闭 SAP 优化，纯 Tree-Walk 执行

// shadowMode 影子模式全局开关
var shadowMode bool

// SetShadowMode 启用/关闭影子模式
func SetShadowMode(enabled bool) {
	shadowMode = enabled
}

// IsShadowMode 返回影子模式是否启用
func IsShadowMode() bool {
	return shadowMode
}

// ShadowResult 影子模式执行结果：返回值 + 副作用（打印输出）
type ShadowResult struct {
	ReturnValue object.Object
	Stdout      string // 捕获的打印输出
}

// RunShadow 双轨执行：SAP 路径 + 传统路径，返回两者的结果
//
// program: AST 根节点
// sapCompiler: SAP 编译器（已 CompileProgram）
// slotMap: 类型推断的槽位映射
//
// 返回：sapResult, naiveResult, error
func RunShadow(program *ast.Program, sapComp *SAPCompiler, slotMap map[string]int) (*ShadowResult, *ShadowResult, error) {
	// === 轨道 1：SAP 优化路径 ===
	SetSAPCompiler(sapComp)
	sapEnv := NewSAPEnvironment(len(slotMap), slotMap)
	var sapBuf bytes.Buffer
	installShadowPrint(&sapBuf, sapEnv)
	sapResult := Eval(program, sapEnv)
	sapShadow := &ShadowResult{
		ReturnValue: sapResult,
		Stdout:      sapBuf.String(),
	}

	// === 轨道 2：传统 Tree-Walk 路径 ===
	SetSAPCompiler(nil) // 关闭 SAP，纯 Tree-Walk
	naiveEnv := NewEnvironment()
	var naiveBuf bytes.Buffer
	installShadowPrint(&naiveBuf, naiveEnv)
	naiveResult := Eval(program, naiveEnv)
	naiveShadow := &ShadowResult{
		ReturnValue: naiveResult,
		Stdout:      naiveBuf.String(),
	}

	// 恢复 SAP 编译器
	SetSAPCompiler(sapComp)

	return sapShadow, naiveShadow, nil
}

// AssertShadow 对比双轨执行结果（严格模式），不一致则返回错误描述
// 严格模式：逐行精确对比，任何差异都报错
func AssertShadow(sap, naive *ShadowResult) error {
	return assertShadowImpl(sap, naive, false)
}

// AssertShadowLenient 宽松对比：过滤时间戳、排序后对比（处理并发交错）
// 适用于包含并发或时间相关代码的脚本
func AssertShadowLenient(sap, naive *ShadowResult) error {
	return assertShadowImpl(sap, naive, true)
}

// assertShadowImpl 对比实现核心
func assertShadowImpl(sap, naive *ShadowResult, lenient bool) error {
	// 1. 对比返回值
	if !objectsEqual(sap.ReturnValue, naive.ReturnValue) {
		return fmt.Errorf(
			"影子模式断言失败 [返回值不一致]\n"+
				"  SAP  优化路径: %s\n"+
				"  传统 Tree-Walk: %s",
			inspectSafe(sap.ReturnValue),
			inspectSafe(naive.ReturnValue),
		)
	}

	// 2. 对比副作用（打印输出）
	sapOut := sap.Stdout
	naiveOut := naive.Stdout

	if lenient {
		sapOut = normalizeForLenient(sapOut)
		naiveOut = normalizeForLenient(naiveOut)
	}

	if sapOut != naiveOut {
		return fmt.Errorf(
			"影子模式断言失败 [打印输出不一致]\n"+
				"=== SAP 优化路径输出 ===\n%s\n"+
				"=== 传统 Tree-Walk 输出 ===\n%s\n"+
				"=== 差异 ===\n%s",
			sap.Stdout,
			naive.Stdout,
			diffStrings(sapOut, naiveOut),
		)
	}

	return nil
}

// installShadowPrint 在环境中注入影子打印函数
// 将 打印 的输出重定向到指定 buffer，而非 stdout
func installShadowPrint(buf *bytes.Buffer, env *object.Environment) {
	shadowPrint := func(args ...object.Object) object.Object {
		var parts []string
		for _, arg := range args {
			parts = append(parts, arg.Inspect())
		}
		fmt.Fprintln(buf, strings.Join(parts, " "))
		return NULL
	}
	env.Set("打印", &object.Builtin{Fn: shadowPrint})
}

// inspectSafe 安全 Inspect（处理 nil）
func inspectSafe(obj object.Object) string {
	if obj == nil {
		return "<nil>"
	}
	return obj.Inspect()
}

// diffStrings 简单的字符串差异对比（逐行）
func diffStrings(a, b string) string {
	aLines := strings.Split(a, "\n")
	bLines := strings.Split(b, "\n")
	maxLen := len(aLines)
	if len(bLines) > maxLen {
		maxLen = len(bLines)
	}

	var diffs []string
	for i := 0; i < maxLen; i++ {
		var aLine, bLine string
		if i < len(aLines) {
			aLine = aLines[i]
		}
		if i < len(bLines) {
			bLine = bLines[i]
		}
		if aLine != bLine {
			diffs = append(diffs, fmt.Sprintf("  第 %d 行:\n    SAP:   %q\n    原生: %q", i+1, aLine, bLine))
		}
	}
	if len(diffs) == 0 {
		return "（逐行对比未发现差异，可能行尾不同）"
	}
	return strings.Join(diffs, "\n")
}

// normalizeForLenient 宽松模式标准化：
//  1. 将所有数字替换为占位符 <N>（过滤时间戳/耗时等非确定性数值）
//  2. 对所有行排序后重新拼接（处理 goroutine 交错导致的行序差异）
//
// 这样只保留"语义结构"的对比，忽略"数值"和"时序"差异。
// 如果 SAP 把 500000500000 算成了 500000500001，数字不同但仍会被过滤！
// 所以宽松模式需要配合返回值断言（上面第 1 步）一起使用才安全。
func normalizeForLenient(s string) string {
	// 1. 数字 → <N>
	numRe := regexp.MustCompile(`\d+`)
	normalized := numRe.ReplaceAllString(s, "<N>")

	// 2. 按行排序（处理并发交错）
	lines := strings.Split(normalized, "\n")
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// DefaultStdout 返回默认 stdout（供非影子模式使用）
func DefaultStdout() io.Writer {
	return os.Stdout
}
