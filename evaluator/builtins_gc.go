package evaluator

// builtins_gc.go — 语言级 GC 观测内置（第33轮 T33-b；第36轮 T35-c 补可达对象统计）
//
// 口径（六字段）：
//   live        —— 环境链上的变量数（当前根环境 store + 父链递归，去重防环）
//   total       —— 进程启动以来累计分配的堆对象数（runtime.MemStats.Mallocs）
//   collections —— 已完成的 GC 轮次（runtime.MemStats.NumGC）
//   objects     —— 当前存活堆对象数（runtime.MemStats.HeapObjects）
//   reachable   —— 从根环境可达的语言级堆对象数（对象图遍历，去重防环；第36轮新增）
//   reach_bytes —— 上述可达对象的估算字节数（按类型粗估，非精确 malloc 口径；第36轮新增）
//
// 说明：reachable 与 objects 口径不同——objects 是 Go 堆全量（含 AST / 诊断结构 / 运行时缓存），
// reachable 只数 zuzo 语言层从根环境可达的对象。判断「语言级对象图是否泄漏」看 reachable；
// 判断「Go 进程堆是否受控」看 objects。ReadMemStats 会短暂 STW，属于诊断型内置的预期成本。

import (
	"runtime"

	"zuzo/object"
)

// gcRootEnv 保存最近一次 registerBuiltins 绑定的根环境，供 GC统计() 遍历变量链。
var gcRootEnv *object.Environment

// builtinGCStats GC统计() → 哈希{live, total, collections, objects, reachable, reach_bytes}
func builtinGCStats(args ...object.Object) object.Object {
	if len(args) != 0 {
		return newError("GC统计() 期望 0 个参数，得到 %d 个", len(args))
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	reachObjs, reachBytes := countReachable(gcRootEnv)

	h := &object.Hash{}
	h.SetKey(&object.String{Value: "live"}, &object.Integer{Value: int64(countLiveVars(gcRootEnv))})
	h.SetKey(&object.String{Value: "total"}, &object.Integer{Value: int64(ms.Mallocs)})
	h.SetKey(&object.String{Value: "collections"}, &object.Integer{Value: int64(ms.NumGC)})
	h.SetKey(&object.String{Value: "objects"}, &object.Integer{Value: int64(ms.HeapObjects)})
	h.SetKey(&object.String{Value: "reachable"}, &object.Integer{Value: int64(reachObjs)})
	h.SetKey(&object.String{Value: "reach_bytes"}, &object.Integer{Value: reachBytes})
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

// reachWalker 从根环境沿对象图遍历，统计语言级可达对象数与估算字节。
// 所有容器按引用递归（数组元素 / 哈希键值 / 实例属性 / 函数与模块的环境），
// 用 seen（对象身份）与 envs（环境身份）去重，天然防闭包环。
type reachWalker struct {
	seen  map[object.Object]bool
	envs  map[*object.Environment]bool
	objs  int
	bytes int64
}

// countReachable 返回从 root 可达的语言级对象数与估算字节数。
func countReachable(root *object.Environment) (int, int64) {
	w := &reachWalker{
		seen: make(map[object.Object]bool, 128),
		envs: make(map[*object.Environment]bool, 8),
	}
	w.env(root)
	return w.objs, w.bytes
}

// env 遍历一个环境（含其外层链）：每个变量值进入对象图。
func (w *reachWalker) env(e *object.Environment) {
	if e == nil || w.envs[e] {
		return
	}
	w.envs[e] = true
	for _, v := range e.GetAll() {
		w.obj(v)
	}
	w.env(e.Outer())
}

// obj 统计单个对象并递归其子对象。估算口径按类型粗估（见各 case），
// 不含 malloc 头开销，也不含 AST 节点（AST 是编译期结构，不是运行时值）。
func (w *reachWalker) obj(o object.Object) {
	if o == nil || w.seen[o] {
		return
	}
	w.seen[o] = true
	w.objs++
	switch v := o.(type) {
	case *object.Integer, *object.Float, *object.Boolean, *object.Null:
		w.bytes += 8
	case *object.String:
		w.bytes += int64(16 + len(v.Value))
	case *object.Array:
		w.bytes += int64(24 + 16*len(v.Elements))
		for _, el := range v.Elements {
			w.obj(el)
		}
	case *object.Hash:
		w.bytes += int64(32 + 32*len(v.Pairs))
		for _, p := range v.Pairs {
			w.obj(p.Key)
			w.obj(p.Value)
		}
	case *object.Function:
		w.bytes += 64
		w.env(v.Env)
	case *object.Builtin:
		w.bytes += 48
	case *object.Module:
		w.bytes += 48
		w.env(v.Env)
	case *object.Instance:
		w.bytes += int64(48 + 32*len(v.Props))
		for _, p := range v.Props {
			w.obj(p)
		}
	case *object.Define:
		w.bytes += int64(32 + 24*len(v.Fields))
		for _, f := range v.Fields {
			w.obj(f.DefaultVal)
		}
	case *object.ReturnValue:
		w.bytes += 16
		w.obj(v.Value)
	case *object.Error:
		w.bytes += int64(32 + len(v.Message))
	default:
		w.bytes += 16
	}
}
