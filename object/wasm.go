package object

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// WASM 对象类型常量
const WASM_OBJ = "WASM模块"
const WASM_FUNC_OBJ = "WASM函数"

// WasmModule WASM 模块对象：包装 wazero 的运行时实例
type WasmModule struct {
	Runtime wazero.Runtime  // WASM 运行时
	Module  api.Module      // 已实例化的模块
	Ctx     context.Context // 上下文
	Cancel  context.CancelFunc
	Name    string // 模块名
}

func (w *WasmModule) Type() ObjectType { return WASM_OBJ }
func (w *WasmModule) Inspect() string {
	return fmt.Sprintf("WASM模块[%s] (导出: %d 个)", w.Name, len(w.ExportedFunctions()))
}

// ExportedFunctions 返回导出函数列表
func (w *WasmModule) ExportedFunctions() map[string]api.FunctionDefinition {
	if w.Module == nil {
		return nil
	}
	return w.Module.ExportedFunctionDefinitions()
}

// Close 关闭模块，释放资源
func (w *WasmModule) Close() {
	if w.Cancel != nil {
		w.Cancel()
	}
	if w.Module != nil {
		_ = w.Module.Close(w.Ctx)
	}
	if w.Runtime != nil {
		_ = w.Runtime.Close(w.Ctx)
	}
}

// WasmFunc WASM 函数对象：延迟绑定的 WASM 函数引用
type WasmFunc struct {
	Module  *WasmModule
	Name    string
	FuncDef api.FunctionDefinition
}

func (f *WasmFunc) Type() ObjectType { return WASM_FUNC_OBJ }
func (f *WasmFunc) Inspect() string {
	if f.FuncDef != nil {
		params := f.FuncDef.ParamTypes()
		results := f.FuncDef.ResultTypes()
		return fmt.Sprintf("WASM函数 %s(%d参数→%d返回)", f.Name, len(params), len(results))
	}
	return fmt.Sprintf("WASM函数 %s", f.Name)
}
