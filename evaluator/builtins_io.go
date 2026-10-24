package evaluator

import (
	"bufio"
	"os"

	"zuzo/object"
)

// ==================== 文件 I/O 内置函数（封装 Go os/bufio/io 包） ====================

// 读文件(路径) → 字符串 — 读取整个文件内容
func builtinReadFile(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("读文件() 期望 1 个参数，得到 %d 个", len(args))
	}
	path, ok := args[0].(*object.String)
	if !ok {
		return newError("读文件() 参数必须是字符串（文件路径），得到 %s", args[0].Type())
	}

	data, err := os.ReadFile(path.Value)
	if err != nil {
		return newError("读文件() 失败: %s", err.Error())
	}
	return &object.String{Value: string(data)}
}

// 写文件(路径, 内容) → 空 — 将字符串写入文件（覆盖）
func builtinWriteFile(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("写文件() 期望 2 个参数，得到 %d 个", len(args))
	}
	path, ok := args[0].(*object.String)
	if !ok {
		return newError("写文件() 第一个参数必须是字符串（文件路径），得到 %s", args[0].Type())
	}
	content, ok := args[1].(*object.String)
	if !ok {
		return newError("写文件() 第二个参数必须是字符串（内容），得到 %s", args[1].Type())
	}

	err := os.WriteFile(path.Value, []byte(content.Value), 0644)
	if err != nil {
		return newError("写文件() 失败: %s", err.Error())
	}
	return NULL
}

// 追加文件(路径, 内容) → 空 — 追加写入文件
func builtinAppendFile(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("追加文件() 期望 2 个参数，得到 %d 个", len(args))
	}
	path, ok := args[0].(*object.String)
	if !ok {
		return newError("追加文件() 第一个参数必须是字符串（文件路径），得到 %s", args[0].Type())
	}
	content, ok := args[1].(*object.String)
	if !ok {
		return newError("追加文件() 第二个参数必须是字符串（内容），得到 %s", args[1].Type())
	}

	f, err := os.OpenFile(path.Value, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return newError("追加文件() 打开失败: %s", err.Error())
	}
	defer f.Close()

	if _, err := f.WriteString(content.Value); err != nil {
		return newError("追加文件() 写入失败: %s", err.Error())
	}
	return NULL
}

// 读行() → 字符串 — 从标准输入读取一行（阻塞）
func builtinReadLine(args ...object.Object) object.Object {
	if len(args) != 0 {
		return newError("读行() 期望 0 个参数，得到 %d 个", len(args))
	}

	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return &object.String{Value: scanner.Text()}
	}
	if err := scanner.Err(); err != nil {
		return newError("读行() 失败: %s", err.Error())
	}
	return NULL
}

// 文件存在(路径) → 布尔 — 检查文件是否存在
func builtinFileExists(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("文件存在() 期望 1 个参数，得到 %d 个", len(args))
	}
	path, ok := args[0].(*object.String)
	if !ok {
		return newError("文件存在() 参数必须是字符串（文件路径），得到 %s", args[0].Type())
	}

	_, err := os.Stat(path.Value)
	if err == nil {
		return TRUE
	}
	return FALSE
}

// 创建文件(路径) → 空 — 创建空文件（已存在则截断）
func builtinCreateFile(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("创建文件() 期望 1 个参数，得到 %d 个", len(args))
	}
	path, ok := args[0].(*object.String)
	if !ok {
		return newError("创建文件() 参数必须是字符串（文件路径），得到 %s", args[0].Type())
	}

	f, err := os.Create(path.Value)
	if err != nil {
		return newError("创建文件() 失败: %s", err.Error())
	}
	f.Close()
	return NULL
}

// 删除文件(路径) → 空 — 删除文件或空目录
func builtinDeleteFile(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("删除文件() 期望 1 个参数，得到 %d 个", len(args))
	}
	path, ok := args[0].(*object.String)
	if !ok {
		return newError("删除文件() 参数必须是字符串（文件路径），得到 %s", args[0].Type())
	}

	if err := os.Remove(path.Value); err != nil {
		return newError("删除文件() 失败: %s", err.Error())
	}
	return NULL
}

// 重命名(旧路径, 新路径) → 空 — 重命名或移动文件
func builtinRename(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("重命名() 期望 2 个参数，得到 %d 个", len(args))
	}
	oldPath, ok := args[0].(*object.String)
	if !ok {
		return newError("重命名() 第一个参数必须是字符串（旧路径），得到 %s", args[0].Type())
	}
	newPath, ok := args[1].(*object.String)
	if !ok {
		return newError("重命名() 第二个参数必须是字符串（新路径），得到 %s", args[1].Type())
	}

	if err := os.Rename(oldPath.Value, newPath.Value); err != nil {
		return newError("重命名() 失败: %s", err.Error())
	}
	return NULL
}

// 读目录(路径) → 数组 — 列出目录下的文件名
func builtinReadDir(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("读目录() 期望 1 个参数，得到 %d 个", len(args))
	}
	path, ok := args[0].(*object.String)
	if !ok {
		return newError("读目录() 参数必须是字符串（目录路径），得到 %s", args[0].Type())
	}

	entries, err := os.ReadDir(path.Value)
	if err != nil {
		return newError("读目录() 失败: %s", err.Error())
	}

	var elements []object.Object
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		elements = append(elements, &object.String{Value: name})
	}
	if elements == nil {
		elements = []object.Object{}
	}
	return &object.Array{Elements: elements}
}

// 环境变量(名) → 字符串 — 读取环境变量（不存在返回空字符串）
func builtinGetEnv(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("环境变量() 期望 1 个参数，得到 %d 个", len(args))
	}
	name, ok := args[0].(*object.String)
	if !ok {
		return newError("环境变量() 参数必须是字符串，得到 %s", args[0].Type())
	}
	return &object.String{Value: os.Getenv(name.Value)}
}

// 运行参数(序号) → 字符串 — 获取命令行参数（序号0=程序名，1=第一个参数）
// 不传参数则返回参数总数（整数）
func builtinCmdArgs(args ...object.Object) object.Object {
	if len(args) == 0 {
		return &object.Integer{Value: int64(len(os.Args))}
	}
	idx, ok := args[0].(*object.Integer)
	if !ok {
		return newError("运行参数() 参数必须是整数（序号），得到 %s", args[0].Type())
	}
	i := int(idx.Value)
	if i < 0 || i >= len(os.Args) {
		return &object.String{Value: ""}
	}
	return &object.String{Value: os.Args[i]}
}
