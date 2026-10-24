package mix

// ==================== 惰性全局表同步 ====================
//
// 设计（v2，替换 v1 的块边界全量深拷贝）：
//
//	v1：每块结束把本侧全部变量深拷贝到对方 —— 成本 O(变量数 × 块数)，5000 变量 × 20 块 +221 ms。
//	v2：完全不预拷贝。每侧维护「写入版本号」，块前只针对本块真正引用的名字
//	    拉取对方最新值（带版本比对，未变则零动作），块后只登记本块定义名的版本。
//
// 语义与 v1 一致（后写入侧胜出），转换次数从「全量变量」降到「实际跨语言引用数」。

// BeforeZuzo zuzo 块执行前：按本块引用的名字从 Python 侧惰性拉取
func (b *Bridge) BeforeZuzo(refs map[string]bool) {
	for name := range refs {
		if b.injected[name] {
			continue
		}
		pv := b.pyVer[name]
		if pv == 0 || b.zzSeenPyVer[name] == pv {
			continue
		}
		v, ok := b.IP.Globals.Get(name)
		if !ok {
			continue
		}
		b.Env.Set(name, b.ToZuzo(v))
		b.zzSeenPyVer[name] = pv
	}
}

// AfterZuzo zuzo 块执行后：登记本块定义名的版本（不转换）
func (b *Bridge) AfterZuzo(defs map[string]bool) {
	for name := range defs {
		b.zzVer[name]++
	}
}

// BeforePy Python 块执行前：按本块引用的名字从 zuzo 侧惰性拉取
func (b *Bridge) BeforePy(refs map[string]bool) {
	for name := range refs {
		zv := b.zzVer[name]
		if zv == 0 || b.pySeenZzVer[name] == zv {
			continue
		}
		v, ok := b.Env.Get(name)
		if !ok {
			continue
		}
		b.IP.Globals.Set(name, b.ToPy(v))
		b.pySeenZzVer[name] = zv
	}
}

// AfterPy Python 块执行后：登记本块定义名的版本（不转换）
func (b *Bridge) AfterPy(defs map[string]bool) {
	for name := range defs {
		b.pyVer[name]++
	}
}