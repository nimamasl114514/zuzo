package object

// context.go — De Bruijn Index + 扁平帧栈（Flat Frame Stack）
//
// 核心思想：用连续数组替代链表式 Environment，用 (Depth, Slot) 二维坐标
// 替代变量名查找，实现 O(1) 绝对坐标寻址。
//
// De Bruijn Index:
//   Depth = 0 → 当前作用域
//   Depth = 1 → 外层作用域（函数闭包）
//   Depth = 2 → 更外层
//
// 扁平帧栈:
//   frames 是一个连续的 []Frame 数组，每个 Frame 包含 slots []Value。
//   函数调用时 PushFrame，返回时 PopFrame。
//   变量访问通过 c.frames[c.sp-depth].slots[slot] 完成，一次数组索引运算。
//
// 优势:
//   1. O(1) 变量查找：从 O(N) Map Hash + 链表遍历降维成 1 次数组索引
//   2. CPU Cache 友好：连续内存布局让 CPU 预取器完美工作
//   3. 消灭 syncSlot：Context 是唯一存储，无需 map+slot 双写
//   4. 零接口开销：Value 是 8 字节标量，全寄存器运算

// DeBruijnIndex 标识符的绝对二维坐标
type DeBruijnIndex struct {
	Depth int // 0 = 当前帧，1 = 外层帧
	Slot  int // 帧内槽位索引
}

// Frame 单个激活记录（一个作用域的变量集合）
type Frame struct {
	slots []Value // Smi 标记值，8 字节连续存储
}

// Context 扁平帧栈——替代链表式 Environment
type Context struct {
	frames []Frame // 连续数组，CPU 缓存友好
	sp     int     // Stack Pointer（当前帧索引）
}

// NewContext 创建带初始帧的 Context
// initialSlotCount 为根帧的槽位数量
func NewContext(initialSlotCount int) *Context {
	ctx := &Context{
		frames: make([]Frame, 1, 16), // 预分配 16 帧的增长空间
		sp:     0,
	}
	ctx.frames[0] = Frame{slots: make([]Value, initialSlotCount)}
	return ctx
}

// PushFrame 压入新帧（函数调用/新作用域）
// slotCount 为新帧所需的槽位数
func (c *Context) PushFrame(slotCount int) {
	c.sp++
	if c.sp >= len(c.frames) {
		// 扩容 frames 数组
		newFrames := make([]Frame, c.sp+1, c.sp+16)
		copy(newFrames, c.frames)
		c.frames = newFrames
	}
	// 复用或创建 slots
	if cap(c.frames[c.sp].slots) >= slotCount {
		c.frames[c.sp].slots = c.frames[c.sp].slots[:slotCount]
		// 清零
		for i := range c.frames[c.sp].slots {
			c.frames[c.sp].slots[i] = 0
		}
	} else {
		c.frames[c.sp].slots = make([]Value, slotCount)
	}
}

// PopFrame 弹出当前帧（函数返回/作用域退出）
func (c *Context) PopFrame() {
	if c.sp > 0 {
		c.sp--
	}
}

// Get 按 De Bruijn 索引获取值——O(1) 数组索引，无 Map 查找，无链表遍历
//
//go:inline
func (c *Context) Get(depth, slot int) Value {
	return c.frames[c.sp-depth].slots[slot]
}

// Set 按 De Bruijn 索引设置值
//
//go:inline
func (c *Context) Set(depth, slot int, val Value) {
	c.frames[c.sp-depth].slots[slot] = val
}

// GetSmi 便捷方法：获取 Smi 值并直接解码为 int64
//
//go:inline
func (c *Context) GetSmi(depth, slot int) int64 {
	return c.frames[c.sp-depth].slots[slot].AsSmi()
}

// SetSmi 便捷方法：将 int64 编码为 Smi 并存储
//
//go:inline
func (c *Context) SetSmi(depth, slot int, val int64) {
	c.frames[c.sp-depth].slots[slot] = Smi(val)
}

// CurrentFrame 返回当前帧的 slots 切片（用于循环退出时批量同步）
func (c *Context) CurrentFrame() []Value {
	return c.frames[c.sp].slots
}

// FrameCount 返回帧栈深度
func (c *Context) FrameCount() int {
	return c.sp + 1
}

// Depth 返回当前栈深度
func (c *Context) Depth() int {
	return c.sp
}

// GetObject 从 Context 槽位获取值并转换为 Object
// 用于循环退出后将 Context 值同步回 Environment
func (c *Context) GetObject(depth, slot int) Object {
	v := c.Get(depth, slot)
	if v.IsSmi() {
		return v.AsObject()
	}
	return &Null{}
}

// SetObject 将 Object 转换为 Value 并存入 Context 槽位
func (c *Context) SetObject(depth, slot int, obj Object) {
	if smiVal, ok := ObjectAsSmi(obj); ok {
		c.Set(depth, slot, smiVal)
	} else {
		c.Set(depth, slot, 0) // 非 Integer 存零值
	}
}
