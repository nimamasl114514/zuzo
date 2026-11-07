package evaluator

// builtins_gc.go — 语言级 GC 观测内置（第33轮 T33-b）
//
// 背景：zuzo 有五层执行栈（Tree-Walk → SAP → 原生 int64 → Smi → De Bruijn），
// 但语言层没有任何 GC / 内存观测手段。长跑程序（如 100 万次循环）无法判断
// 对象是否被回收、堆是否线性增长。
//
// 口径（四字段，均可由 runtime 零侵入取得）：
//   live        —— 环境链上的变量数（当前根环境 store + 父链递归，去重防环）
//   total       —— 进程启动以来累计分配的堆对象数（runtime.MemStats.Mallocs）
//   collections —— 已完成的 GC 轮次（runtime.MemStats.NumGC）
//   objects     —— 当前存活堆对象数（runtime.MemStats.HeapObjects）
//
// 说明：store 是变量的唯一真源——SAP/Smi 路径的 Set/Assign 都会同步写回 store，
// 因此按 store 长度统计等价于「Object 槽位 ∪ Smi 槽位 ∪ 哈希表」的并集，且成本最低。
// ReadMemStats 会短暂 STW，属于诊断型内置的预期成本。

import (
	"runtime"

	"zuzo/object"
)

// gcRootEnv 保存最近一次 registerBuiltins 绑定的根环境，供 GC统计() 遍历变量链。
var gcRootEnv *object.Environment

// builtinGCStats GC统计() → 哈希{live, total, collections, objects}
func builtinGCStats(args ...object.Object) object.Object {
	if len(args) != 0 {
		return newError("GC统计() 期望 0 个参数，得到 %d 个", len(args))
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	h := &object.Hash{}
	h.SetKey(&object.String{Value: "live"}, &object.Integer{Value: int64(countLiveVars(gcRootEnv))})
	h.SetKey(&object.String{Value: "total"}, &object.Integer{Value: int64(ms.Mallocs)})
	h.SetKey(&object.String{Value: "collections"}, &object.Integer{Value: int64(ms.NumGC)})
	h.SetKey(&object.String{Value: "objects"}, &object.Integer{Value: int64(ms.HeapObjects)})
	return h
}

// countLiveVars 统计环境链上各作用域的变量数（含内置函数绑定）。
// 用 seen 集合防环：闭包环境理论上可能出现自引用。
func countLiveVars(env *object.Environment) int {
	if env == nil {
		return 0
	}
	total := 0
	seen := make(map[*object.Environment]bool, 8)
	for env != nil {
		if seen[env] {
			break
		}
		seen[env] = true
		total += len(env.GetAll())
		env = env.Outer()
	}
	return total
}
