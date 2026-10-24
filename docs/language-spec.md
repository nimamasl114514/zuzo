# zuzo 语言规范

本文是 zuzo 语言的**实现级规范**：所有条目均从源码直读，标注了对应的源文件与符号，
便于交叉核对。规范描述的是**当前实现行为**，不是理想设计。

- 词法与关键字：`token/token.go`、`lexer/lexer.go`
- 语法与优先级：`parser/parser.go`
- 类型系统：`infer/infer.go`、`infer/infer_builtins.go`
- 运行时语义：`evaluator/evaluator.go`、`evaluator/modern.go`
- 内置函数：`evaluator/evaluator.go:registerBuiltins` 与 `evaluator/builtins_*.go`
- 内嵌 Python：`py/runtime/builtins.go`、`py/runtime/stdlib*.go`

执行模型与性能分层见 [架构说明](architecture.md)。

---

## 1. 词法层

### 1.1 字符集与标识符

- 源文件按 UTF-8 读取；标识符允许 Unicode 字母与数字，因此中文变量名是一等公民（`总`、`计数`、`文件存在`）。
- 标识符判定入口为 `token.LookupIdent`：先查关键字表，命中则返回关键字类型，否则返回 `IDENT`。
- 空白与换行不产生语义（无自动分号插入）；语句边界由语法结构决定。
- 注释为行注释，以 `//` 起始至行尾。

### 1.2 字面量

| 字面量 | 词法类型 | 说明 |
|---|---|---|
| 整数 | `INT` | 十进制，运行时承载于 `int64`；越界走大整数路径 |
| 浮点数 | `FLOAT` | 十进制带小数点 |
| 字符串 | `STRING` | 双引号包裹 |
| f-字符串 | `FSTRING` | Python 风格插值：`f"你好 {名字}"` |
| 布尔 | `TRUE` / `FALSE` | 字面量写作 `真` / `假` |
| 空 | `NULL` | 字面量写作 `空` |

### 1.3 关键字（共 23 个，源自 `token/token.go`）

关键字是**中文原生**的，不接受英文别名。

| 关键字 | Token 常量 | 语义 |
|---|---|---|
| `变量` | `LET` | 变量声明 |
| `函数` | `FUNCTION` | 函数字面量 / 具名函数声明 |
| `如果` | `IF` | 条件表达式（可带 `否则`） |
| `否则` | `ELSE` | 条件分支；同时是中缀 fallback 运算符 |
| `当` | `WHILE` | 条件循环 |
| `对于` | `FOR` | C 风格循环；`在/到/满足` 组合为遍历循环 |
| `返回` | `RETURN` | 函数返回 |
| `真` | `TRUE` | 布尔真 |
| `假` | `FALSE` | 布尔假 |
| `空` | `NULL` | 空值 |
| `并且` | `AND` | 逻辑与（短路） |
| `或者` | `OR` | 逻辑或（短路） |
| `非` | `NOT` | 逻辑非（前缀） |
| `导入` | `IMPORT` | 模块导入 |
| `为` | `AS` | 导入别名 |
| `定义` | `DEFINE` | 数据建模（蓝图 + 实例） |
| `执行` | `TRY` | 错误处理块起始 |
| `遇到` | `CATCH` | 错误捕获分支 |
| `在` | `IN` | 遍历循环的集合/起点标记 |
| `到` | `TO` | 遍历循环的终点标记 |
| `满足` | `WHEN` | 遍历循环的过滤条件 |
| `跳过` | `SKIP` | continue |
| `跳出` | `BREAK` | break |

### 1.4 运算符与优先级

优先级常量定义于 `parser/parser.go`，数值越大结合越紧。解析器为 **Pratt 解析法**，
每个 token 的（前缀函数、中缀函数、优先级）集中在 `operatorDefs` 一张表里。

| 优先级 | 常量 | 运算符 |
|---|---|---|
| -1 | `_LOWEST` | 表达式入口 |
| 0 | `_ASSIGN` | `=` |
| 1 | `_FALLBACK` | `否则`（错误/空值回退） |
| 2 | `_OR` | `或者` |
| 3 | `_AND` | `并且` |
| 4 | `_EQUALS` | `==` `!=` |
| 5 | `_LESSGREATER` | `<` `>` `<=` `>=` |
| 6 | `_SUM` | `+` `-` |
| 7 | `_PRODUCT` | `*` `/` `%` |
| 8 | `_PREFIX` | `-` `!` `非`（前缀） |
| 9 | `_CALL` | `(` 函数调用 |
| 10 | `_INDEX` | `[` 索引；`.` 成员访问同优先级 |

`-` 与 `!`/`非` 是双角色 token：前缀位置走 `parsePrefixExpression`，中缀位置走 `parseInfixExpression`。

---

## 2. 语法产生式

下列 EBNF 直接对应 `parser/parser.go` 中的 `parseXxx` 函数与 `ast/ast.go` 的节点结构。

### 2.1 程序与语句

```ebnf
Program     = { Statement } ;
Statement   = LetStmt | ReturnStmt | WhileStmt | ForStmt | ForInStmt
            | ImportStmt | DefineStmt | TryCatchStmt | BreakStmt
            | ContinueStmt | BlockStmt | ExprStmt ;
Block       = "{" { Statement } "}" ;
ExprStmt    = Expression ;
```

### 2.2 声明与赋值

```ebnf
LetStmt     = "变量" IDENT "=" Expression ;
ReturnStmt  = "返回" [ Expression ] ;
AssignExpr  = LValue "=" Expression ;          (* 中缀，优先级 _ASSIGN *)
LValue      = IDENT | IndexExpr ;
```

赋值是**表达式**而非语句（`ast.AssignExpression`），因此可作为子表达式出现；
`=` 右结合由 Pratt 循环的 `_ASSIGN` 优先级保证。

### 2.3 条件与循环

```ebnf
IfExpr      = "如果" Expression Block [ "否则" ( IfExpr | Block ) ] ;
WhileStmt   = "当" Expression Block ;
ForStmt     = "对于" [Expression] ";" [Expression] ";" [Expression] Block ;
ForInStmt   = "对于" IDENT [ "," IDENT ] "在" Expression
              [ "到" Expression ] [ "满足" Expression ] Block ;
BreakStmt   = "跳出" ;
ContinueStmt= "跳过" ;
```

- `如果` 是**表达式**，有值；`否则 如果` 链式条件由 `parseIfExpression` 递归支持。
- `ForInStmt` 一个产生式覆盖三种形态：集合遍历（`在 数组`）、范围遍历（`在 0 到 10`）、
  带索引遍历（`在 arr, i` 形式通过 `parseForInRest` 解析索引变量）以及带过滤（`满足 条件`）。

### 2.4 函数与调用

```ebnf
FunctionLit = "函数" [ IDENT ] "(" [ ParamList ] ")" [ "->" TypeAnnot ] Block ;
ParamList   = Param { "," Param } ;
Param       = IDENT [ ":" TypeAnnot ] ;
CallExpr    = Expression "(" [ ArgList ] ")" ;
ArgList     = Arg { "," Arg } ;
Arg         = Expression | NamedArg ;
NamedArg    = IDENT ":" Expression ;
```

- 具名函数声明形如 `函数 名称(a, b) { ... }`，在 `ExpressionStatement` 求值时绑定到环境。
- 参数支持类型注解（`ast.Identifier.TypeAnnot`），注解文本进入 `infer` 作为约束来源。
- 调用支持**命名实参**（`ast.NamedArg`），求值器在 `evaluator.go` 中按名绑定。

### 2.5 数据建模与错误处理

```ebnf
DefineStmt  = "定义" IDENT "{" { FieldDef } "}" ;
FieldDef    = IDENT "=" Expression ;
TryCatchStmt= "执行" Block "遇到" IDENT Block ;
```

- `定义` 产生蓝图对象（`object.DEFINE_OBJ`），字段值为默认值表达式；
  调用蓝图得到实例（`object.INSTANCE_OBJ`）。
- `执行/遇到` 的捕获变量绑定错误对象；配合 `错误码` / `错误信息` / `错误行` 三个内置函数读取结构化错误。

### 2.6 导入

```ebnf
ImportStmt  = "导入" STRING [ "为" IDENT ] ;
```

- 无 `为`：在当前环境执行目标文件，其顶层定义直接可见。
- 有 `为`：在新环境执行，结果包装为模块对象（`object.MODULE_OBJ`），以别名为名绑定。
- 搜索顺序（`evaluator/module.go`）：相对路径 → `zpm_modules/` → `~/.zuzo/stdlib/` → `~/.zuzo/.cache/zpackages/`；
  支持 `.hy`、`.z` 包，具备循环导入检测与“同一文件只执行一次”的模块缓存。

### 2.7 表达式

```ebnf
Expression  = PrefixExpr | InfixExpr | AssignExpr | IfExpr | FunctionLit
            | CallExpr | IndexExpr | MemberExpr | ArrayLit | HashLit
            | IntegerLit | FloatLit | StringLit | FStringLit | BoolLit
            | NullLit | IDENT | "(" Expression ")" ;
PrefixExpr  = ( "-" | "!" | "非" ) Expression ;
InfixExpr   = Expression BinOp Expression ;
BinOp       = "+" | "-" | "*" | "/" | "%" | "==" | "!=" | "<" | ">" | "<=" | ">="
            | "并且" | "或者" | "否则" ;
ArrayLit    = "[" [ Expression { "," Expression } ] "]" ;
HashLit     = "{" [ Expression ":" Expression { "," Expression ":" Expression } ] "}" ;
IndexExpr   = Expression "[" Expression "]" ;
MemberExpr  = Expression "." IDENT ;           (* 语法糖：降为 IndexExpr + 字符串键 *)
```

`.成员` 在解析期被降级为字符串键索引（`parseMemberExpression` 直接构造 `IndexExpression`），
因此模块成员访问与哈希取值在运行时走同一条路径。

---

## 3. 类型系统

`infer` 包是**静态推断器**，不是强制类型检查器：它推断 AST 节点类型、分配变量槽位、
产出类型错误码，但默认不阻止执行（`--strict` 才阻止）。

### 3.1 类型集合（`infer/infer.go`）

| 类型 | 构造 | 出现形态 |
|---|---|---|
| 整数 | `TInt`（`TypePrimitive{"整数"}`） | `int64` |
| 浮点数 | `TFloat` | `float64` |
| 字符串 | `TStr` | `string` |
| 布尔 | `TBool` | `bool` |
| 空 | `TNull` | `object.NULL` |
| 任意 | `TAny` | 推断失败的兜底 |
| 哈希 | `THash` | 值类型为任意 |
| 数组 | `TypeArray{Elem}` | 元素类型参与统一 |
| 通道 | `TypeChannel{Elem}` | 并发原语 |
| 函数 | `TypeFunc{Params, Return, Variadic}` | 可变参数以 `Variadic` 标记 |

### 3.2 推断机制

1. **类型变量**：`TypeVar{ID, Resolved}` 表示未知类型，`infer.newTypeVar()` 分配。
2. **约束求解**：`addConstraint(a, b, node)` 记录 `a ≡ b`，统一（unify）时合并类型变量链。
3. **路径压缩**：`TypeVar.resolved()` 把链式查找压平为均摊 O(1)，并显式避免用 `!=` 比较
   含 slice 的类型（`TypeFunc` 不可比较），防止深递归栈溢出。
4. **槽位分配**：`Inferencer.SlotMap map[string]int` 为每个变量名分配扁平槽位序号，
   供 SAP 直接按整数下标存取，这是五层执行栈的性能前提。
5. **节点类型查询**：`GetNodeType(node)` 让 SAP 编译期拿到“这个 `+` 是整数加法”这类确定信息。

### 3.3 类型错误码

| 码 | 常量 | 含义 |
|---|---|---|
| `E101` | `ErrCodeTypeMismatch` | 类型不匹配 |
| `E102` | `ErrCodeUndefinedVar` | 未定义变量 |
| `E103` | `ErrCodeArgCount` | 参数数量不匹配 |
| `E104` | `ErrCodeNotCallable` | 不可调用 |
| `E105` | `ErrCodeBranchMismatch` | 分支类型不一致 |
| `E106` | `ErrCodeArrayMixed` | 数组元素类型不一致 |

错误以 `diagnostic.Report` 渲染，带源码行与因果链（根因 / 连带影响分级）。

### 3.4 运行时对象类型（`object/object.go`）

运行时类型名直接是中文：`整数` `浮点数` `字符串` `布尔` `空` `返回值` `错误` `函数`
`内置函数` `数组` `哈希` `模块` `实例` `定义` `跳出信号` `跳过信号`。

`跳出` / `跳过` 以**信号对象**（`BREAK_SIG` / `CONTINUE_SIG`）沿求值结果向上传播，
循环节点负责识别并消费，不会被误当成普通值。

### 3.5 值与表示

- `object.Value` 是 64 位无符号整数：最低位 `1` 表示 Smi（小整数直接内联，零堆分配），
  `0` 预留给堆句柄（池内索引 + 4 bit 类型标记）。编码为 `(n << 1) | 1`。
- 小整数 `-512..511` 走 `smiCache` 预分配对象池，避免重复堆分配。
- 浮点、字符串、数组、哈希、函数等仍以 Go 接口对象形式存放于 `Environment`，
  以保证 GC 可达性。

---

## 4. 内置函数清单（zuzo 侧，共 175 个）

下列清单由 `evaluator/evaluator.go` 的 `registerBuiltins` 注册表逐条提取，
签名与说明取自各 `evaluator/builtins_*.go` 的源码注释。**未在源码中出现的函数一律不存在。**

注册机制：`registerBuiltins` 构建 `map[string]object.BuiltinFunction`，
一次性注入全局 `object` 内置池（`object.InitBuiltinPool`），随后按名绑定到环境，
实现零分配复用。

### 通用
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `打印` | ... | 空 | 打印参数（空格分隔） | `builtinPrint` |
| `长度` | 字符串\|数组 | 整数 | 获取长度 | `builtinLen` |
| `类型` | 值 | 字符串 | 获取类型名称 | `builtinType` |
| `字符串` | 值 | 字符串 | 转换为字符串 | `builtinStr` |
| `整数` | 值 | 整数 | 转换为整数 | `builtinInt` |
| `浮点数` | 值 | 浮点数 | 转换为浮点数 | `builtinFloat` |

### 错误内省（与 执行/遇到 搭配使用）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `错误码` | 错误 | 字符串：错误码，如 "E202" |  | `builtinErrorCode` |
| `错误信息` | 错误 | 字符串：错误消息文本 |  | `builtinErrorMessage` |
| `错误行` | 错误 | 整数：错误所在行号（0 表示未知） |  | `builtinErrorLine` |

### 数组（builtins_array.go）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `推入` | 数组, 元素 | 数组 | 追加元素到末尾 | `builtinPush` |
| `弹出` | 数组 | 元素 | 弹出末尾元素 | `builtinPop` |
| `首项` | 数组 | 元素 | 获取第一个元素 | `builtinFirst` |
| `末项` | 数组 | 元素 | 获取最后一个元素 | `builtinLast` |
| `连接` | 数组, 分隔符 | 字符串 | 将数组元素用分隔符连接成字符串 | `builtinJoin` |
| `包含` | 集合, 元素 | 布尔 | 字符串包含或数组包含 | `builtinContains` |

### 字符串处理（builtins_strings.go）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `大写` | s | 字符串 | strings.ToUpper | `builtinToUpper` |
| `小写` | s | 字符串 | strings.ToLower | `builtinToLower` |
| `前缀` | s, prefix | 布尔 | strings.HasPrefix | `builtinHasPrefix` |
| `后缀` | s, suffix | 布尔 | strings.HasSuffix | `builtinHasSuffix` |
| `查找` | s, substr | 整数 | strings.Index（返回位置，-1 表示未找到） | `builtinIndex` |
| `替换` | s, old, new | 字符串 | strings.ReplaceAll | `builtinReplace` |
| `分割` | s, sep | 数组[字符串] | strings.Split | `builtinSplit` |
| `去空白` | s | 字符串 | strings.TrimSpace | `builtinTrimSpace` |
| `裁剪` | s, cutset | 字符串 | strings.Trim | `builtinTrim` |
| `重复` | s, n | 字符串 | strings.Repeat | `builtinRepeat` |
| `计数` | s, substr | 整数 | strings.Count | `builtinCount` |
| `分词` | s | 数组[字符串] | strings.Fields（按空白自动分词） | `builtinFields` |
| `取字符` | s, i | 字符串 | 取第 i 个 rune（以字符为单位，非字节） | `builtinCharAt` |
| `格式化` | — | — |  | `builtinFormat` |

### 哈希（键遍历；支持中文键）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `键` | — | — |  | `builtinKeys` |
| `取值` | — | — |  | `builtinHashGet` |

### 并发（builtins_concurrency.go）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `并发` | 函数 | 通道 | 启动 goroutine 执行函数，返回通道接收结果 | `builtinGo` |
| `通道` | — | 通道 | 创建无缓冲通道 | `builtinMakeChan` |
| `发送` | 通道, 值 | 空 | 向通道发送值（缓冲满时阻塞） | `builtinSend` |
| `接收` | 通道 | 值 | 从通道接收值（通道为空时阻塞） | `builtinRecv` |
| `关闭` | 通道 | 空 | 关闭通道 | `builtinClose` |
| `等待全部` | 通道... | 数组 | 等待多个通道全部完成，按顺序返回结果 | `builtinWaitAll` |
| `尝试接收` | 通道 | 值 | 非阻塞接收，通道为空返回空 | `builtinTryRecv` |
| `尝试执行` | 函数 | 值 | 错误 | 执行函数，捕获运行时错误，不抛出 | `builtinTryEval` |

### 文件 I/O（builtins_io.go）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `读文件` | 路径 | 字符串 | 读取整个文件内容 | `builtinReadFile` |
| `写文件` | 路径, 内容 | 空 | 将字符串写入文件（覆盖） | `builtinWriteFile` |
| `追加文件` | 路径, 内容 | 空 | 追加写入文件 | `builtinAppendFile` |
| `读行` | — | 字符串 | 从标准输入读取一行（阻塞） | `builtinReadLine` |
| `文件存在` | 路径 | 布尔 | 检查文件是否存在 | `builtinFileExists` |
| `创建文件` | 路径 | 空 | 创建空文件（已存在则截断） | `builtinCreateFile` |
| `删除文件` | 路径 | 空 | 删除文件或空目录 | `builtinDeleteFile` |
| `重命名` | 旧路径, 新路径 | 空 | 重命名或移动文件 | `builtinRename` |
| `读目录` | 路径 | 数组 | 列出目录下的文件名 | `builtinReadDir` |
| `环境变量` | 名 | 字符串 | 读取环境变量（不存在返回空字符串） | `builtinGetEnv` |
| `运行参数` | 序号 | 字符串 | 获取命令行参数（序号0=程序名，1=第一个参数） | `builtinCmdArgs` |

### 网络（builtins_net.go）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `http获取` | url | 字符串 | HTTP GET 请求，返回响应体 | `builtinHttpGet` |
| `http提交` | url, body | 字符串 | HTTP POST 请求，返回响应体 | `builtinHttpPost` |
| `http更新` | url, body | 字符串 | HTTP PUT 请求，返回响应体 | `builtinHttpPut` |
| `http补丁` | url, body | 字符串 | HTTP PATCH 请求，返回响应体 | `builtinHttpPatch` |
| `http删除` | url | 字符串 | HTTP DELETE 请求，返回响应体 | `builtinHttpDelete` |
| `http请求` | method, url, body, headers | 哈希 | 完整 HTTP 请求 | `builtinHttpRequest` |

### HTTP 服务端（builtins_httpserver.go）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `http服务` | — | — |  | `builtinHttpServe` |
| `http服务后台` | — | — |  | `builtinHttpServeBackground` |
| `http流请求` | — | — |  | `builtinHttpStreamRequest` |

### JSON（builtins_json.go）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `json解析` | s | 哈希|数组 | 将 JSON 字符串解析为 zuzo 值 | `builtinJsonParse` |
| `json序列化` | 值 | 字符串 | 将 zuzo 值序列化为 JSON 字符串 | `builtinJsonStringify` |
| `json格式化` | 值, 缩进 | 字符串 | 将 zuzo 值序列化为带缩进的 JSON 字符串 | `builtinJsonFormat` |

### 正则表达式（builtins_regex.go）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `正则匹配` | s, pattern | 数组|空 | 返回第一个匹配的全部捕获组；无匹配返回空 | `builtinRegexMatch` |
| `正则查找全部` | s, pattern | 数组 | 返回所有匹配（每个匹配是一个数组） | `builtinRegexFindAll` |
| `正则替换` | s, pattern, replacement | 字符串 | 替换所有匹配 | `builtinRegexReplace` |
| `正则分割` | s, pattern | 数组 | 按正则分割字符串 | `builtinRegexSplit` |
| `正则验证` | s, pattern | 布尔 | 检查字符串是否匹配正则 | `builtinRegexTest` |
| `正则提取` | s, pattern | 字符串 | 返回第一个匹配的字符串（无捕获组时返回完整匹配） | `builtinRegexExtract` |

### 数学（builtins_math.go）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `随机` | — | 浮点数 | 返回 [0.0, 1.0) 随机浮点数 | `builtinRandom` |
| `随机整数` | 最小, 最大 | 整数 | 返回 [min, max] 闭区间随机整数 | `builtinRandomInt` |
| `绝对值` | n | 整数|浮点数 | 返回绝对值 | `builtinAbs` |
| `最大值` | ... | 整数|浮点数 | 返回多个值中的最大值 | `builtinMax` |
| `最小值` | ... | 整数|浮点数 | 返回多个值中的最小值 | `builtinMin` |
| `向上取整` | n | 整数 | math.Ceil | `builtinCeil` |
| `向下取整` | n | 整数 | math.Floor | `builtinFloor` |
| `四舍五入` | n | 整数 | math.Round | `builtinRound` |
| `幂` | base, exp | 浮点数 | math.Pow | `builtinPow` |
| `平方根` | n | 浮点数 | math.Sqrt | `builtinSqrt` |
| `正弦` | n | 浮点数 | math.Sin（弧度） | `builtinSin` |
| `余弦` | n | 浮点数 | math.Cos（弧度） | `builtinCos` |
| `对数` | n, base | 浮点数 | math.Log | `builtinLog` |

### 时间（builtins_time.go）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `时间戳` | — | 整数 | 返回当前 Unix 时间戳（秒） | `builtinUnixTime` |
| `毫秒时间戳` | — | 整数 | 返回当前 Unix 毫秒时间戳 | `builtinUnixMilli` |
| `当前时间` | — | 字符串 | 返回 ISO 8601 格式的当前时间 | `builtinNow` |
| `格式化时间` | 时间戳, 布局 | 字符串 | 将时间戳按 Go 布局格式化 | `builtinFormatTime` |
| `解析时间` | 字符串, 布局 | 整数 | 将时间字符串按布局解析为时间戳 | `builtinParseTime` |
| `睡眠` | 毫秒 | 空 | 阻塞当前协程指定毫秒数 | `builtinSleep` |
| `计时` | 函数 | 整数 | 执行函数并返回耗时（毫秒） | `builtinTimer` |
| `计时秒` | 函数 | 浮点数 | 执行函数并返回耗时（秒，带小数） | `builtinTimerSec` |
| `时间差` | 开始戳, 结束戳 | 字符串 | 返回人类可读的时间差 | `builtinTimeDiff` |

### 浏览器自动化（builtins_browser.go）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `打开浏览器` | 无头, 浏览器路径 | 浏览器 | 启动浏览器 | `builtinOpenBrowser` |
| `关闭浏览器` | 浏览器 | 空 | 关闭浏览器 | `builtinCloseBrowser` |
| `打开页面` | 浏览器 | 页面 | 新建一个标签页 | `builtinNewPage` |
| `关闭页面` | 页面 | 空 | 关闭标签页 | `builtinClosePage` |
| `导航` | 页面, url | 空 | 导航到指定 URL（等待页面加载完成） | `builtinNavigate` |
| `当前网址` | 页面 | 字符串 | 获取当前页面 URL（带重试，防止导航时 context canceled） | `builtinPageURL` |
| `页面标题` | 页面 | 字符串 | 获取页面标题 | `builtinPageTitle` |
| `等待元素` | 页面, 选择器, 超时ms | 空 | 等待元素可见（对齐 Playwright waitFor({state:'visible'})） | `builtinWaitElement` |
| `等待可见` | 页面, 选择器, 超时ms | 布尔 | JS 轮询等待元素可见，返回布尔而非报错 | `builtinWaitVisible` |
| `点击` | 页面, 选择器 | 空 | 点击元素（先等可见再点击） | `builtinClickElement` |
| `点击按钮` | 页面, 文本 | 布尔 | 按文本匹配点击按钮（对齐 Playwright getByRole('button', {name})） | `builtinClickButton` |
| `填写` | 页面, 选择器, 值 | 空 | 用原生 setter 填写输入框（对齐 Playwright el.fill()） | `builtinFillInput` |
| `清空输入` | 页面, 选择器 | 空 | 用原生 setter 清空输入框（对齐 Playwright el.fill(”)） | `builtinClearInput` |
| `获取文本` | 页面, 选择器 | 字符串 | 获取元素的文本内容 | `builtinGetText` |
| `获取属性` | 页面, 选择器, 属性名 | 字符串 | 获取元素的属性值 | `builtinGetAttribute` |
| `获取HTML` | 页面, 选择器 | 字符串 | 获取元素的 HTML 内容 | `builtinGetHTML` |
| `获取全部文本` | 页面 | 字符串 | 获取整个页面的文本内容 | `builtinGetFullText` |
| `元素存在` | 页面, 选择器 | 布尔 | 检查元素是否存在且可见 | `builtinElementExists` |
| `元素数量` | 页面, 选择器 | 整数 | 返回匹配选择器的可见元素数量（对齐 Playwright locator.count()） | `builtinElementCount` |
| `执行脚本` | 页面, js代码 | 字符串 | 在页面中执行 JavaScript 并返回结果（支持 async/await） | `builtinEvalJS` |
| `等待跳转` | 页面, url包含, 超时ms | 布尔 | 等待页面跳转到包含指定字符串的 URL | `builtinWaitNavigation` |
| `截图` | 页面, 路径 | 空 | 对当前页面截图并保存到文件 | `builtinScreenshot` |
| `设置视口` | 页面, 宽, 高 | 空 | 设置页面视口大小 | `builtinSetViewport` |
| `等待` | 毫秒 | 空 | 在页面上下文中等待（用于轮询场景） | `builtinBrowserWait` |

### 编码/解码（builtins_encoding.go）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `base64编码` | s | 字符串 | standard Base64 编码 | `builtinBase64Encode` |
| `base64解码` | s | 字符串 | standard Base64 解码 | `builtinBase64Decode` |
| `base64url编码` | s | 字符串 | URL-safe Base64 编码 | `builtinBase64URLEncode` |
| `base64url解码` | s | 字符串 | URL-safe Base64 解码 | `builtinBase64URLDecode` |
| `hex编码` | s | 字符串 | 将字符串的 UTF-8 字节编码为十六进制 | `builtinHexEncode` |
| `hex解码` | s | 字符串 | 将十六进制字符串解码为原始字符串 | `builtinHexDecode` |
| `html转义` | s | 字符串 | HTML 实体转义（< > & ' "） | `builtinHTMLEscape` |
| `html反转义` | s | 字符串 | HTML 实体反转义 | `builtinHTMLUnescape` |
| `url编码` | s | 字符串 | URL 编码（url.QueryEscape，空格→+） | `builtinUrlEncode` |
| `url解码` | s | 字符串 | URL 解码（url.QueryUnescape） | `builtinUrlDecode` |
| `url路径编码` | s | 字符串 | URL Path 编码（url.PathEscape，空格→%20） | `builtinUrlPathEncode` |
| `url路径解码` | s | 字符串 | URL Path 解码（url.PathUnescape） | `builtinUrlPathDecode` |
| `md5` | s | 字符串 | 计算字符串的 MD5 哈希（十六进制） | `builtinMD5` |
| `sha1` | s | 字符串 | 计算字符串的 SHA1 哈希（十六进制） | `builtinSHA1` |
| `sha256` | s | 字符串 | 计算字符串的 SHA256 哈希（十六进制） | `builtinSHA256` |

### UTF-8 / Unicode / 转义（builtins_utf8.go）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `转义` | s | 字符串 | 将字符串中的特殊字符转义为可安全传输的形式 | `builtinEscape` |
| `反转义` | s | 字符串 | 将转义序列还原为实际字符 | `builtinUnescape` |
| `unicode转义` | s | 字符串 | 将非 ASCII 字符转为 \uXXXX 转义形式 | `builtinUnicodeEscape` |
| `unicode反转义` | s | 字符串 | 将 \uXXXX / \UXXXXXXXX 转义序列还原为实际字符 | `builtinUnicodeUnescape` |
| `码点转字符` | n | 字符串 | 将 Unicode 码点转为对应字符（类似 chr） | `builtinCodepointToChar` |
| `字符转码点` | s | 整数 | 将字符串的第一个字符转为 Unicode 码点（类似 ord） | `builtinCharToCodepoint` |
| `utf8验证` | s | 布尔 | 验证字符串是否为有效的 UTF-8 编码 | `builtinUTF8Valid` |
| `utf8修复` | s | 字符串 | 修复无效的 UTF-8 序列，用替换字符 (U+FFFD) 替换损坏字节 | `builtinUTF8Fix` |
| `utf8字节数` | s | 整数 | 返回字符串的 UTF-8 编码字节数（区别于 长度() 返回 rune 数） | `builtinUTF8ByteCount` |
| `转编码` | s, 编码名 | 字符串 | 将 UTF-8 字符串编码为指定编码（返回字节序列的字符串形式） | `builtinEncodeTo` |
| `从编码转换` | s, 编码名 | 字符串 | 将指定编码的字节序列解码为 UTF-8 字符串 | `builtinDecodeFrom` |
| `规范化` | s, 形式 | 字符串 | Unicode 规范化 | `builtinNormalize` |
| `全角转半角` | s | 字符串 | 将全角字符转换为半角字符 | `builtinFullToHalf` |
| `半角转全角` | s | 字符串 | 将半角字符转换为全角字符 | `builtinHalfToFull` |
| `去除不可见` | s | 字符串 | 去除控制字符和其他不可见字符 | `builtinRemoveInvisible` |
| `去除BOM` | s | 字符串 | 去除字符串开头的 UTF-8 BOM (U+FEFF) | `builtinRemoveBOM` |

### BDD 测试框架（builtins_bdd.go）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `测试套件` | 名称, 函数 | 空 |  | `builtinTestSuite` |
| `测试用例` | 名称, 函数 | 空 |  | `builtinTestCase` |
| `测试前` | 函数 | 空 |  | `builtinBeforeEach` |
| `测试后` | 函数 | 空 |  | `builtinAfterEach` |
| `测试跳过` | 原因 | 空 |  | `builtinTestSkip` |
| `断言相等` | 期望, 实际 | 空 |  | `builtinAssertEqual` |
| `断言不相等` | a, b | 空 |  | `builtinAssertNotEqual` |
| `断言为真` | 值 | 空 |  | `builtinAssertTrue` |
| `断言为假` | 值 | 空 |  | `builtinAssertFalse` |
| `断言为空` | 值 | 空 |  | `builtinAssertNull` |
| `断言非空` | 值 | 空 |  | `builtinAssertNotNull` |
| `断言包含` | 容器, 元素 | 空 |  | `builtinAssertContains` |
| `断言大于` | a, b | 空  （a > b） |  | `builtinAssertGreater` |
| `断言小于` | a, b | 空  （a < b） |  | `builtinAssertLess` |
| `断言大于等于` | a, b | 空  （a >= b） |  | `builtinAssertGreaterEqual` |
| `断言小于等于` | a, b | 空  （a <= b） |  | `builtinAssertLessEqual` |
| `断言错误` | 函数 | 空 |  | `builtinAssertError` |
| `断言无错误` | 函数 | 空 |  | `builtinAssertNoError` |
| `运行测试` | — | 布尔 |  | `builtinRunTests` |

### WASM FFI（builtins_ffi.go）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `加载WASM` | — | — |  | `builtinLoadWasm` |
| `WASM调用` | — | — |  | `builtinWasmCall` |
| `WASM导出` | — | — |  | `builtinWasmExports` |
| `WASM函数` | — | — |  | `builtinWasmFunc` |
| `WASM内存读取` | — | — |  | `builtinWasmMemRead` |
| `WASM内存写入` | — | — |  | `builtinWasmMemWrite` |
| `WASM内存大小` | — | — |  | `builtinWasmMemSize` |
| `WASM关闭` | — | — |  | `builtinWasmClose` |

### C 库 FFI（builtins_cffi.go）
| 函数 | 参数 | 返回 | 说明 | Go 实现 |
|---|---|---|---|---|
| `加载C库` | 路径或库名 | C库对象 |  | `builtinLoadCLib` |
| `C函数声明` | 库, 函数名, [参数类型...], 返回类型 | C函数对象 |  | `builtinCFuncDecl` |
| `C调用` | 函数对象, 参数... | 返回值 |  | `builtinCCall` |

---

## 5. 内嵌 Python 侧能力

zuzo 内嵌一个纯 Go 的 Python 运行时（`py/`），不依赖 CPython。
Python 侧有两套名字体系：**内置函数**（`py/runtime/builtins.go`）与**标准库模块**（`stdlib*.go`）。

### 5.1 Python 内置函数（62 个）

```
print len range int float str repr bool list tuple set frozenset dict bytes
abs round sum min max sorted reversed enumerate zip map filter any all
isinstance issubclass type hasattr getattr setattr delattr callable iter next
format chr ord hex bin oct divmod pow id hash super property staticmethod
classmethod slice open input dir globals locals vars exit quit eval exec
```

另有常量 `True` `False` `None` `NotImplemented` `Ellipsis` 与 `__name__`。

### 5.2 标准库模块（33 个，注册于 `builtinModuleInits`）

| 模块 | 导出的主要成员 |
|---|---|
| `math` | `pi` `e` `tau` `inf` `nan` `floor` `ceil` `trunc` `pow` `atan2` `hypot` `factorial` `gcd` `isnan` `isinf` `isfinite` `sqrt` `exp` `sin` `cos` `tan` `log` 等 |
| `random` | `random` `seed` `randint` `randrange` `uniform` `choice` `shuffle` `sample` |
| `time` | `time` `time_ns` `perf_counter` `sleep` `strftime` `localtime` |
| `json` | `dumps` `loads` `dump` |
| `re` | `match` `search` `findall` `sub` `split` `escape` `compile` + 标志 `I` `M` `S` `IGNORECASE` `MULTILINE` `DOTALL` |
| `os` | `getcwd` `listdir` `makedirs` `mkdir` `rmdir` `remove` `rename` `getenv` `environ` `sep` `linesep` `path` |
| `os.path` | `join` `exists` `isfile` `isdir` `basename` `dirname` `abspath` `split` `splitext` `isabs` `normpath` `relpath` `realpath` `expanduser` `getsize` |
| `sys` | `argv` `version` `platform` `maxsize` `exit` `path` `getrecursionlimit` `setrecursionlimit` `stdout` |
| `string` | `ascii_lowercase` `ascii_uppercase` `ascii_letters` `digits` `hexdigits` `octdigits` `punctuation` `whitespace` `printable` |
| `collections` | `Counter` `defaultdict` `deque` `namedtuple` `OrderedDict` |
| `itertools` | `chain` `product` `combinations` `permutations` `repeat` `count` `islice` `groupby` `zip_longest` |
| `functools` | `reduce` `partial` `wraps` `lru_cache` `cmp_to_key` |
| `copy` | `copy` `deepcopy` |
| `hashlib` | 构造器由 `ip.regMethod` 注册（md5 / sha1 / sha256 对象方法） |
| `base64` | `b64encode` `b64decode` `b64decode_str` |
| `datetime` | `datetime` `now` `date` `timedelta` |
| `typing` | 类型别名占位（`Any` 等，实现于 `modTyping`） |
| `dataclasses` | `dataclass` `field` |
| `asyncio` | `run` `sleep` `gather` `create_task` `wait_for` `iscoroutine` |
| `shutil` | `rmtree` `copy` `copyfile` `move` |
| `pathlib` | `Path` `PurePath` |
| `logging` | `DEBUG` `INFO` `WARNING` `ERROR` `CRITICAL` `basicConfig` `getLogger` `debug` `info` `warning` `error` |
| `argparse` | `ArgumentParser` |
| `unittest` | `TestCase` `main` |
| `threading` | `Thread` `Lock` `RLock` `current_thread` `active_count` |
| `socket` | `socket` `gethostname` + 常量 `AF_INET` `AF_INET6` `SOCK_STREAM` `SOCK_DGRAM` `SOL_SOCKET` `SO_REUSEADDR` |
| `urllib` / `urllib.parse` / `urllib.request` | `quote` `quote_plus` `unquote` `urlencode` / `urlopen` `urlretrieve` |
| `tempfile` | `gettempdir` `mkdtemp` `mkstemp` `NamedTemporaryFile` |
| `glob` | `glob` `escape` |
| `uuid` | `uuid1` `uuid4` |

Python 侧的整数计数循环另有原生路径（`py/runtime/native_loop.go`），
识别 `while i < N` + 整数累加窄模式后整段用 `int64` 执行，退出时一次性写回环境。

---

## 6. 语义要点

- **真值性**：`空` 与 `假` 为假，其余（含 `0`）为真；条件位置复用同一判定函数。
- **短路**：`并且` / `或者` 在左值已决定结果时不求值右值。
- **fallback 运算符**：`左 否则 右`——左值为错误对象或空时取右值，否则取左值。
  这是错误处理的轻量表达，等价于空值合并 + 错误逃逸。
- **错误传播**：任何子表达式返回 `ERROR_OBJ` 时，父表达式立即短路向上传播，
  直到 `执行/遇到` 或 `否则` 接管。
- **变量作用域**：函数体为独立环境，闭包捕获定义处环境；SAP 路径下作用域映射为
  De Bruijn 的 `(Depth, Slot)` 坐标。
- **语义恒定原则**：所有优化层必须与 Tree-Walk 逐字节等价，
  由 `--shadow` 双轨执行与差分测试强制约束。

---

## 7. 规范符合性验证

| 手段 | 命令 | 覆盖内容 |
|---|---|---|
| Python 语义对拍 | `./run_tests.sh` | 33 个用例与 CPython 逐字节比对 |
| 混合语言 golden | `./run_tests_mix.sh` | 15 个 `.hypy` 用例 |
| Go 单测 | `go test ./...` | 原生循环、SAP、混合切块 |
| 类型检查 | `./zuzo --check 文件.hy` | `infer` 全部错误码 |
| 影子执行 | `./zuzo --shadow 文件.hy` | SAP 与 Tree-Walk 结果、副作用一致性 |