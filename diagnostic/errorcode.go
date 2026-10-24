package diagnostic

// ============================================================
// 标准错误码目录
//
// 编码规则：
//   E000-E099: 语法错误（解析器）
//   E100-E199: 类型错误（类型推断器）
//   E200-E299: 运行时错误（求值器）
//   E300-E399: 模块/导入错误
// ============================================================

// ErrorCode 错误码定义
type ErrorCode struct {
	Code        string // 错误码，如 "E001"
	Title       string // 简短标题
	Description string // 详细描述
}

// String 返回错误码字符串
func (ec ErrorCode) String() string { return ec.Code }

// 语法错误码 (E000-E099)
var (
	ErrExpectedToken = ErrorCode{
		Code: "E001", Title: "期望词法单元",
		Description: "解析器期望某个词法单元，但实际得到的是另一个。通常由遗漏符号或多余符号引起。",
	}
	ErrNoPrefixParseFn = ErrorCode{
		Code: "E002", Title: "无法解析",
		Description: "当前词法单元没有对应的前缀解析函数，可能出现在表达式开头。",
	}
	ErrInvalidInteger = ErrorCode{
		Code: "E003", Title: "无效整数",
		Description: "无法将当前词法单元解析为整数。",
	}
	ErrInvalidFloat = ErrorCode{
		Code: "E004", Title: "无效浮点数",
		Description: "无法将当前词法单元解析为浮点数。",
	}
	ErrInvalidAssignTarget = ErrorCode{
		Code: "E005", Title: "赋值目标非法",
		Description: "赋值语句的左侧必须是标识符（变量名）或索引表达式。",
	}
	ErrInvalidImport = ErrorCode{
		Code: "E006", Title: "导入格式错误",
		Description: "导入语句的格式不正确。",
	}
	ErrUnclosedString = ErrorCode{
		Code: "E007", Title: "字符串未闭合",
		Description: "字符串字面量缺少闭合引号。",
	}
	ErrBareReturn = ErrorCode{
		Code:        "E008",
		Title:       "返回语句缺少返回值",
		Description: "'返回' 后面未跟随表达式。请使用 '返回 空' 表示返回空值，或提供返回值表达式。",
	}
	ErrUnexpectedToken = ErrorCode{
		Code:        "E009",
		Title:       "非预期的词法单元",
		Description: "该位置出现了不应出现的标记，通常由上一个语句未正确结束或缺少表达式引起。",
	}
)

// 类型错误码 (E100-E199)
var (
	ErrTypeMismatch = ErrorCode{
		Code: "E101", Title: "类型不匹配",
		Description: "操作符两侧的类型不一致，或期望的类型与实际类型不符。",
	}
	ErrUndefinedVar = ErrorCode{
		Code: "E102", Title: "未定义变量",
		Description: "引用了未在当前作用域中声明的变量。",
	}
	ErrArgCount = ErrorCode{
		Code: "E103", Title: "参数数量不匹配",
		Description: "函数调用时传入的参数数量与函数定义不符。",
	}
	ErrNotCallable = ErrorCode{
		Code: "E104", Title: "不可调用",
		Description: "尝试调用一个非函数类型的对象。",
	}
	ErrBranchMismatch = ErrorCode{
		Code: "E105", Title: "分支类型不一致",
		Description: "条件表达式的 True 分支和 False 分支返回的类型不一致。",
	}
	ErrArrayMixedType = ErrorCode{
		Code: "E106", Title: "数组元素类型不一致",
		Description: "数组字面量中包含了不同类型的元素。",
	}
)

// 运行时错误码 (E200-E299)
var (
	ErrUndefinedIdentifier = ErrorCode{
		Code: "E201", Title: "未定义标识符",
		Description: "运行时引用了未定义的变量或函数。",
	}
	ErrDivisionByZero = ErrorCode{
		Code: "E202", Title: "除零错误",
		Description: "尝试进行除以零的运算。",
	}
	ErrModByZero = ErrorCode{
		Code: "E203", Title: "取模零错误",
		Description: "尝试进行以零为模数的取模运算。",
	}
	ErrUnsupportedOperator = ErrorCode{
		Code: "E204", Title: "不支持的运算符",
		Description: "当前运算符不支持给定的操作数类型。",
	}
	ErrArgCountRuntime = ErrorCode{
		Code: "E205", Title: "参数数量不匹配",
		Description: "函数调用时传入的参数数量与函数定义不符。",
	}
	ErrNotCallableRuntime = ErrorCode{
		Code: "E206", Title: "不可调用的对象",
		Description: "尝试调用一个非函数类型的对象。",
	}
	ErrIndexOutOfBounds = ErrorCode{
		Code: "E207", Title: "索引越界",
		Description: "数组索引超出有效范围。",
	}
	ErrInvalidIndexType = ErrorCode{
		Code: "E208", Title: "索引类型错误",
		Description: "数组索引必须是整数。",
	}
	ErrHashKeyNotFound = ErrorCode{
		Code: "E209", Title: "哈希键不存在",
		Description: "尝试访问哈希表中不存在的键。",
	}
	ErrIntegerOverflow = ErrorCode{
		Code: "E210", Title: "整数溢出",
		Description: "整数运算结果超出 int64 可表示范围（如 MinInt64 / -1 或 MinInt64 % -1）。",
	}
)

// 模块/导入错误码 (E300-E399)
var (
	ErrFileRead = ErrorCode{
		Code: "E301", Title: "文件读取失败",
		Description: "无法读取导入的文件。",
	}
	ErrCircularImport = ErrorCode{
		Code: "E302", Title: "循环导入",
		Description: "检测到模块之间的循环导入依赖。",
	}
	ErrImportSyntax = ErrorCode{
		Code: "E303", Title: "导入文件语法错误",
		Description: "导入的源文件存在语法错误。",
	}
	ErrModuleNotInit = ErrorCode{
		Code: "E304", Title: "模块系统未初始化",
		Description: "模块管理器尚未初始化，无法执行导入操作。",
	}
	ErrImportNotModule = ErrorCode{
		Code: "E305", Title: "导入结果不是模块",
		Description: "导入的文件没有返回模块对象。",
	}
)