package evaluator

import (
	"fmt"
	"os"

	"zuzo/ast"
	"zuzo/diagnostic"
)

// ============================================================
// 运行期诊断通道
// ============================================================
//
// 背景：数组越界、哈希缺键长期静默返回「空」，无效访问在编译/运行两路都
// 没有任何提示（第29轮 T29.1 基线实测）。直接改成硬错误会破坏既有程序语义，
// 因此分两档：
//   - 默认（非严格）：照旧返回 空，但向 stderr 打一条带 file:line:col 的警告
//   - --strict：返回错误对象，按运行期错误中止（与类型错误严格模式一致）
// 开关由 main 在解析参数后设置。

var (
	runtimeStrict bool
	runtimeFile   string
	// warningsEnabled 允许单独关掉（避免 golden 输出被警告污染）
	// 环境变量 ZUZO_RT_WARN=0/off/false 关闭；默认开。
	warningsEnabled = !isEnvOff(os.Getenv("ZUZO_RT_WARN"))
)

func isEnvOff(v string) bool {
	switch v {
	case "0", "off", "false", "OFF", "FALSE", "no":
		return true
	}
	return false
}

// SetRuntimeStrict 设置运行期严格模式（越界/缺键变为错误）
func SetRuntimeStrict(v bool) { runtimeStrict = v }

// RuntimeStrict 返回当前是否严格模式
func RuntimeStrict() bool { return runtimeStrict }

// SetRuntimeFile 设置运行期诊断使用的源文件名
func SetRuntimeFile(name string) { runtimeFile = name }

// SetRuntimeWarnings 开关运行期警告（默认开）
func SetRuntimeWarnings(v bool) { warningsEnabled = v }

// runtimeWarn 输出一条运行期警告（不中断执行）
func runtimeWarn(node ast.Node, code, msg string) {
	if !warningsEnabled {
		return
	}
	line, col := getNodeLine(node), getNodeColumn(node)
	loc := fmt.Sprintf("%d:%d", line, col)
	if runtimeFile != "" {
		loc = runtimeFile + ":" + loc
	}
	fmt.Fprintf(os.Stderr, "⚠ 运行期警告 %s [%s]: %s（本次按「空」继续）\n", loc, code, msg)
}

// diagnosticCode 供外部构造错误时取码
func diagnosticCode(ec diagnostic.ErrorCode) string { return ec.Code }

var _ = diagnostic.ErrIndexOutOfBounds
