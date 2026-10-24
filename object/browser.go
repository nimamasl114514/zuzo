package object

import (
	"context"
	"fmt"
	"runtime"
	"sync"
)

// 浏览器自动化相关的对象类型
const (
	BROWSER_OBJ = "浏览器"
	PAGE_OBJ    = "页面"
)

// Browser 浏览器句柄：包装 chromedp 的 context
type Browser struct {
	allocCtx context.Context    // chromedp 分配上下文
	cancel   context.CancelFunc // 取消函数
	ctx      context.Context    // 浏览器上下文

	// 🌟 优化 1：并发安全与状态管理
	cancelOnce sync.Once
	closed     bool
	mu         sync.Mutex
}

// NewBrowser 创建浏览器句柄（构造函数，使字段可以从包外设置）
func NewBrowser(allocCtx, ctx context.Context, cancel context.CancelFunc) *Browser {
	b := &Browser{allocCtx: allocCtx, ctx: ctx, cancel: cancel}
	
	// 🌟 优化 2：设置 GC 终结器，防止用户忘记调用 Cancel 导致 chromedp 僵尸进程泄漏
	runtime.SetFinalizer(b, func(b *Browser) {
		b.Cancel()
	})
	
	return b
}

func (b *Browser) Type() ObjectType { return BROWSER_OBJ }

// Inspect 返回对象的字符串表示
// 🌟 优化 3：丰富输出内容，包含运行状态和指针地址，极大提升 REPL 调试体验
func (b *Browser) Inspect() string {
	if b == nil {
		return "浏览器(null)"
	}
	status := "运行中"
	if b.IsClosed() {
		status = "已关闭"
	}
	return fmt.Sprintf("浏览器(%s, %p)", status, b)
}

// String 实现 fmt.Stringer 接口，让 fmt.Println 自动调用 Inspect
func (b *Browser) String() string { return b.Inspect() }

// AllocCtx 返回浏览器分配上下文
func (b *Browser) AllocCtx() context.Context {
	if b == nil {
		return context.Background() // 🌟 优化 4：Nil 安全保护
	}
	return b.allocCtx
}

// Ctx 返回浏览器上下文
func (b *Browser) Ctx() context.Context {
	if b == nil {
		return context.Background()
	}
	return b.ctx
}

// Cancel 取消浏览器
func (b *Browser) Cancel() {
	if b == nil {
		return
	}
	// 🌟 优化 1：使用 sync.Once 保证并发安全，且无论调用多少次都不会 panic
	b.cancelOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		b.mu.Unlock()
		
		if b.cancel != nil {
			b.cancel()
		}
	})
}

// IsClosed 检查浏览器是否已关闭（新增辅助方法，不破坏原有 API）
func (b *Browser) IsClosed() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

// ============================================================================

// Page 页面句柄：包装一个浏览器标签页的 context
type Page struct {
	ctx     context.Context    // 页面上下文
	cancel  context.CancelFunc // 取消函数
	browser *Browser           // 所属浏览器

	// 🌟 优化 1：并发安全与状态管理
	cancelOnce sync.Once
	closed     bool
	mu         sync.Mutex
}

// NewPage 创建页面句柄（构造函数，使字段可以从包外设置）
func NewPage(ctx context.Context, cancel context.CancelFunc, browser *Browser) *Page {
	p := &Page{ctx: ctx, cancel: cancel, browser: browser}
	
	// 🌟 优化 2：设置 GC 终结器，防止页面标签泄漏
	runtime.SetFinalizer(p, func(p *Page) {
		p.Cancel()
	})
	
	return p
}

func (p *Page) Type() ObjectType { return PAGE_OBJ }

// Inspect 返回对象的字符串表示
func (p *Page) Inspect() string {
	if p == nil {
		return "页面(null)"
	}
	status := "运行中"
	if p.IsClosed() {
		status = "已关闭"
	}
	return fmt.Sprintf("页面(%s, %p)", status, p)
}

// String 实现 fmt.Stringer 接口
func (p *Page) String() string { return p.Inspect() }

// Ctx 返回页面上下文
func (p *Page) Ctx() context.Context {
	if p == nil {
		return context.Background()
	}
	return p.ctx
}

// Cancel 取消页面
func (p *Page) Cancel() {
	if p == nil {
		return
	}
	p.cancelOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
		
		if p.cancel != nil {
			p.cancel()
		}
	})
}

// IsClosed 检查页面是否已关闭
func (p *Page) IsClosed() bool {
	if p == nil {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// Browser 返回所属浏览器
func (p *Page) Browser() *Browser {
	if p == nil {
		return nil
	}
	return p.browser
}