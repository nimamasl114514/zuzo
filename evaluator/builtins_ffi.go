package evaluator

// builtins_ffi.go - WebAssembly (WASM) FFI builtins
//
// 用法 (推荐):
//   m = 加载WASM("math.wasm")    // 加载 .wasm 文件
//   m.add(1, 2)                  // 直接调用导出函数，零样板
//   m.fibonacci(50)              // 任意导出函数均可点号调用
//   m.关闭()                     // 关闭模块
//   m.导出()                     // 列出导出函数
//   m.内存大小()                 // 查询内存大小
//   m.内存读取(0, 10)            // 读取内存
//   m.内存写入(0, "data")        // 写入内存
//
// 低级 API (兼容旧写法):
//   WASM调用(mod, fn, args...)   - 显式调用
//   WASM导出(mod)               - 列出导出
//   WASM内存读取(mod, off, len) - 读取内存
//   WASM内存写入(mod, off, data)- 写入内存
//   WASM内存大小(mod)           - 内存大小
//   WASM关闭(mod)               - 关闭模块

import (
	"context"
	"fmt"
	"os"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"zuzo/object"
)

var wasmRuntime wazero.Runtime

func getWasmRuntime() wazero.Runtime {
	if wasmRuntime != nil {
		return wasmRuntime
	}
	ctx := context.Background()
	cache, err := wazero.NewCompilationCacheWithDir(os.TempDir())
	if err != nil {
		wasmRuntime = wazero.NewRuntime(ctx)
		return wasmRuntime
	}
	wasmRuntime = wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithCompilationCache(cache))
	return wasmRuntime
}

// ==================== 加载WASM ====================

func builtinLoadWasm(args ...object.Object) object.Object {
	if len(args) < 1 || len(args) > 2 {
		return newError("加载WASM() 期望 1-2 个参数，得到 %d 个", len(args))
	}

	path, ok := args[0].(*object.String)
	if !ok {
		return newError("加载WASM() 第一个参数必须是字符串（文件路径），得到 %s", args[0].Type())
	}

	name := ""
	if len(args) == 2 {
		if n, ok := args[1].(*object.String); ok {
			name = n.Value
		}
	}

	wasmBytes, err := os.ReadFile(path.Value)
	if err != nil {
		return newError("加载WASM() 无法读取文件 %s: %v", path.Value, err)
	}

	if name == "" {
		name = path.Value
	}

	ctx, cancel := context.WithCancel(context.Background())
	rt := getWasmRuntime()

	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		cancel()
		return newError("加载WASM() 编译模块失败: %v", err)
	}

	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		cancel()
		return newError("加载WASM() 实例化失败: %v", err)
	}

	return &object.WasmModule{
		Runtime: rt,
		Module:  mod,
		Ctx:     ctx,
		Cancel:  cancel,
		Name:    name,
	}
}

// ==================== 核心调用逻辑 ====================

// callWasmFunction 核心调用逻辑 — 被 builtinWasmCall 和点号语法共用
func callWasmFunction(mod *object.WasmModule, fnName string, args []object.Object) object.Object {
	fn := mod.Module.ExportedFunction(fnName)
	if fn == nil {
		return newError("WASM 模块中不存在导出函数 %q", fnName)
	}

	funcDef := mod.Module.ExportedFunctionDefinitions()[fnName]
	return doWasmCall(mod, fn, funcDef, args)
}

// doWasmCall 执行实际的 WASM 函数调用
func doWasmCall(mod *object.WasmModule, fn api.Function, funcDef api.FunctionDefinition, args []object.Object) object.Object {
	paramTypes := []api.ValueType{}
	if funcDef != nil {
		paramTypes = funcDef.ParamTypes()
	}

	wasmArgs := make([]uint64, 0, len(args))
	for i, arg := range args {
		var wasmVal uint64
		expectedType := api.ValueTypeI64

		if i < len(paramTypes) {
			expectedType = paramTypes[i]
		}

		switch v := arg.(type) {
		case *object.Integer:
			switch expectedType {
			case api.ValueTypeI32:
				wasmVal = uint64(uint32(v.Value))
			case api.ValueTypeI64:
				wasmVal = uint64(v.Value)
			case api.ValueTypeF32:
				wasmVal = api.EncodeF32(float32(v.Value))
			case api.ValueTypeF64:
				wasmVal = api.EncodeF64(float64(v.Value))
			default:
				wasmVal = uint64(v.Value)
			}
		case *object.Float:
			switch expectedType {
			case api.ValueTypeF32:
				wasmVal = api.EncodeF32(float32(v.Value))
			case api.ValueTypeF64:
				wasmVal = api.EncodeF64(v.Value)
			case api.ValueTypeI32:
				wasmVal = uint64(uint32(int32(v.Value)))
			case api.ValueTypeI64:
				wasmVal = uint64(int64(v.Value))
			default:
				wasmVal = api.EncodeF64(v.Value)
			}
		case *object.Boolean:
			if v.Value {
				wasmVal = 1
			} else {
				wasmVal = 0
			}
		default:
			return newError("WASM 参数 %d 类型不支持: %s", i+1, arg.Type())
		}
		wasmArgs = append(wasmArgs, wasmVal)
	}

	results, err := fn.Call(mod.Ctx, wasmArgs...)
	if err != nil {
		return newError("WASM 调用失败: %v", err)
	}

	if len(results) == 0 {
		return NULL
	}
	if len(results) == 1 {
		return wasmValueToObject(results[0], funcDef)
	}

	elems := make([]object.Object, len(results))
	for i, val := range results {
		elems[i] = wasmValueToObject(val, funcDef)
	}
	return &object.Array{Elements: elems}
}

func wasmValueToObject(val uint64, funcDef api.FunctionDefinition) object.Object {
	if funcDef != nil && len(funcDef.ResultTypes()) > 0 {
		rt := funcDef.ResultTypes()[0]
		if rt == api.ValueTypeF32 {
			return &object.Float{Value: float64(api.DecodeF32(val))}
		}
		if rt == api.ValueTypeF64 {
			return &object.Float{Value: api.DecodeF64(val)}
		}
	}
	return &object.Integer{Value: int64(val)}
}

// wasmModuleBuiltin 为 WasmModule 的某个导出函数创建可直接调用的 Builtin
func wasmModuleBuiltin(mod *object.WasmModule, fnName string) *object.Builtin {
	return &object.Builtin{Fn: func(args ...object.Object) object.Object {
		return callWasmFunction(mod, fnName, args)
	}}
}

// ==================== 低级 API (兼容旧写法) ====================

// WASM调用
func builtinWasmCall(args ...object.Object) object.Object {
	if len(args) < 2 {
		return newError("WASM调用() 至少需要 2 个参数（模块, 函数名），得到 %d 个", len(args))
	}
	mod, ok := args[0].(*object.WasmModule)
	if !ok {
		return newError("WASM调用() 第一个参数必须是 WASM 模块，得到 %s", args[0].Type())
	}
	funcName, ok := args[1].(*object.String)
	if !ok {
		return newError("WASM调用() 第二个参数必须是字符串（函数名），得到 %s", args[1].Type())
	}
	return callWasmFunction(mod, funcName.Value, args[2:])
}

// WASM导出
func builtinWasmExports(args ...object.Object) object.Object {
	if len(args) == 1 {
		// 直接传模块: WASM导出(模块)
		if mod, ok := args[0].(*object.WasmModule); ok {
			return wasmExportList(mod)
		}
	}
	return newError("WASM导出() 期望 1 个 WASM 模块参数")
}

func wasmExportList(mod *object.WasmModule) object.Object {
	funcs := mod.ExportedFunctions()
	elems := make([]object.Object, 0, len(funcs))

	for name, def := range funcs {
		params := def.ParamTypes()
		results := def.ResultTypes()
		info := fmt.Sprintf("%s(%d参数->%d返回)", name, len(params), len(results))
		elems = append(elems, &object.String{Value: info})
	}

	return &object.Array{Elements: elems}
}

// WASM函数
func builtinWasmFunc(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("WASM函数() 期望 2 个参数，得到 %d 个", len(args))
	}

	mod, ok := args[0].(*object.WasmModule)
	if !ok {
		return newError("WASM函数() 第一个参数必须是 WASM 模块，得到 %s", args[0].Type())
	}

	funcName, ok := args[1].(*object.String)
	if !ok {
		return newError("WASM函数() 第二个参数必须是字符串，得到 %s", args[1].Type())
	}

	funcs := mod.ExportedFunctions()
	def, exists := funcs[funcName.Value]
	if !exists {
		return newError("WASM函数() 模块中不存在导出函数 %q", funcName.Value)
	}

	return &object.WasmFunc{
		Module:  mod,
		Name:    funcName.Value,
		FuncDef: def,
	}
}

// WASM内存读取
func builtinWasmMemRead(args ...object.Object) object.Object {
	if len(args) == 3 {
		// 低级 API: WASM内存读取(模块, 偏移, 长度)
		mod, ok := args[0].(*object.WasmModule)
		if !ok {
			return newError("WASM内存读取() 第一个参数必须是 WASM 模块，得到 %s", args[0].Type())
		}
		return wasmMemRead(mod, args[1], args[2])
	}
	return newError("WASM内存读取() 期望 3 个参数，得到 %d 个", len(args))
}

func wasmMemRead(mod *object.WasmModule, offsetArg, lengthArg object.Object) object.Object {
	offset, ok := offsetArg.(*object.Integer)
	if !ok {
		return newError("WASM内存读取() 偏移必须是整数，得到 %s", offsetArg.Type())
	}
	length, ok := lengthArg.(*object.Integer)
	if !ok {
		return newError("WASM内存读取() 长度必须是整数，得到 %s", lengthArg.Type())
	}

	mem := mod.Module.Memory()
	if mem == nil {
		return newError("WASM 模块没有导出内存")
	}

	buf, ok := mem.Read(uint32(offset.Value), uint32(length.Value))
	if !ok {
		return newError("WASM 内存范围越界: 偏移=%d 长度=%d", offset.Value, length.Value)
	}

	return &object.String{Value: string(buf)}
}

// WASM内存写入
func builtinWasmMemWrite(args ...object.Object) object.Object {
	if len(args) == 3 {
		mod, ok := args[0].(*object.WasmModule)
		if !ok {
			return newError("WASM内存写入() 第一个参数必须是 WASM 模块，得到 %s", args[0].Type())
		}
		return wasmMemWrite(mod, args[1], args[2])
	}
	return newError("WASM内存写入() 期望 3 个参数，得到 %d 个", len(args))
}

func wasmMemWrite(mod *object.WasmModule, offsetArg, dataArg object.Object) object.Object {
	offset, ok := offsetArg.(*object.Integer)
	if !ok {
		return newError("WASM内存写入() 偏移必须是整数，得到 %s", offsetArg.Type())
	}
	data, ok := dataArg.(*object.String)
	if !ok {
		return newError("WASM内存写入() 数据必须是字符串，得到 %s", dataArg.Type())
	}

	mem := mod.Module.Memory()
	if mem == nil {
		return newError("WASM 模块没有导出内存")
	}

	dataBytes := []byte(data.Value)
	ok = mem.Write(uint32(offset.Value), dataBytes)
	if !ok {
		return newError("WASM 内存范围越界: 偏移=%d 长度=%d", offset.Value, len(dataBytes))
	}

	return &object.Integer{Value: int64(len(dataBytes))}
}

// WASM内存大小
func builtinWasmMemSize(args ...object.Object) object.Object {
	if len(args) == 1 {
		if mod, ok := args[0].(*object.WasmModule); ok {
			return wasmMemSize(mod)
		}
	}
	return newError("WASM内存大小() 期望 1 个 WASM 模块参数")
}

func wasmMemSize(mod *object.WasmModule) object.Object {
	mem := mod.Module.Memory()
	if mem == nil {
		return newError("WASM 模块没有导出内存")
	}
	return &object.Integer{Value: int64(mem.Size())}
}

// WASM关闭
func builtinWasmClose(args ...object.Object) object.Object {
	if len(args) == 1 {
		if mod, ok := args[0].(*object.WasmModule); ok {
			mod.Close()
			return NULL
		}
	}
	return newError("WASM关闭() 期望 1 个 WASM 模块参数")
}
