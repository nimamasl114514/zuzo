package object

// pyobj.go — CPython 子进程桥的「远端对象句柄」
//
// 设计（第32轮 · 2026-11-01）：
//   - 桥的另一端是真实 CPython 进程，对象不能复制进 zuzo 堆，只能持引用（句柄）。
//   - 句柄只带三样东西：远端 ID、类型名、repr（截断），够 zuzo 侧打印/判定/传参。
//   - 可调用性显式标记，避免每次 py调 前多跑一次往返探测。
//   - 句柄不是值：跨语言容器传递仍是快照语义（与 mix 桥一致），释放走 py释放。

// PY_OBJ Python 对象句柄
const PY_OBJ = "Python对象"

// PyObj 指向 CPython 子进程里的一个对象
type PyObj struct {
	ID       int64  // 远端句柄 ID（子进程侧自增）
	Typ      string // Python 类型名，如 "module" / "numpy.ndarray"
	Repr     string // repr 前若干字符（打印用）
	Callable bool   // true = 可直接 py调
	Bridge   string // 所属桥标识（当前恒为 "pyext"，留作多桥扩展）
	Session  string // 子进程会话标识（重启后失配 → 旧句柄作废，防 ABA）
}

func (p *PyObj) Type() ObjectType { return PY_OBJ }

func (p *PyObj) Inspect() string {
	if p.Repr != "" {
		return p.Repr
	}
	return "<Python " + p.Typ + " #" + itoa(p.ID) + ">"
}

// itoa 避免为一行格式化引入额外依赖
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
