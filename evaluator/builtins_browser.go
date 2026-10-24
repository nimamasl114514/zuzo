package evaluator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"zuzo/object"
)

// ==================== 浏览器自动化内置函数（封装 chromedp） ====================
//
// 设计原则：对齐 Playwright 的行为语义
//   - 填写/清空 → 用原生 setter（React/Vue/Angular 兼容），等价 Playwright 的 el.fill()
//   - 等待元素 → 等到可见（offsetParent !== null），等价 Playwright 的 waitFor({state:'visible'})
//   - 执行脚本 → 自动 awaitPromise，等价 Playwright 的 page.evaluate(async()=>{...})
//   - 当前网址 → 带重试，防止页面导航时 context canceled
//   - 点击按钮 → 按文本匹配，等价 Playwright 的 getByRole('button', {name: /regex/})

// 打开浏览器(无头, 浏览器路径) → 浏览器 — 启动浏览器
// 无头参数为布尔，true=无头模式，false=有头模式（默认有头）
// 浏览器路径为可选字符串，指定浏览器可执行文件路径（默认自动检测）
func builtinOpenBrowser(args ...object.Object) object.Object {
	headless := false
	if len(args) >= 1 {
		if b, ok := args[0].(*object.Boolean); ok {
			headless = b.Value
		}
	}

	// 可选：用户指定浏览器路径
	browserPath := ""
	if len(args) >= 2 {
		if s, ok := args[1].(*object.String); ok {
			browserPath = s.Value
		}
	}

	// 自动检测浏览器路径
	if browserPath == "" {
		browserPath = findBrowserPath()
		if browserPath == "" {
			return newError("打开浏览器() 未找到浏览器，请安装 Chrome 或 Edge，或手动指定路径")
		}
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserPath),
		chromedp.Flag("headless", headless),
		chromedp.Flag("disable-gpu", false),
		chromedp.Flag("start-maximized", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)

	// 启动浏览器
	err := chromedp.Run(browserCtx)
	if err != nil {
		allocCancel()
		return newError("打开浏览器() 启动失败: %s", err.Error())
	}

	return object.NewBrowser(allocCtx, browserCtx, func() { browserCancel(); allocCancel() })
}

// findBrowserPath 自动检测系统上可用的浏览器路径
// 优先级：Chrome > Edge
func findBrowserPath() string {
	// 尝试从 PATH 中查找
	for _, name := range []string{"chrome", "google-chrome", "chromium", "msedge"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}

	// Windows 常见安装路径
	if runtime.GOOS == "windows" {
		candidates := []string{}
		// Chrome
		candidates = append(candidates,
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		)
		// Chrome (用户安装)
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			candidates = append(candidates,
				filepath.Join(localAppData, `Google\Chrome\Application\chrome.exe`),
			)
		}
		// Edge
		candidates = append(candidates,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		)

		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}

	// macOS
	if runtime.GOOS == "darwin" {
		candidates := []string{
			`/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`,
			`/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge`,
			`/Applications/Chromium.app/Contents/MacOS/Chromium`,
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}

	// Linux
	if runtime.GOOS == "linux" {
		candidates := []string{
			`/usr/bin/google-chrome`,
			`/usr/bin/google-chrome-stable`,
			`/usr/bin/chromium`,
			`/usr/bin/chromium-browser`,
			`/usr/bin/microsoft-edge`,
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}

	return ""
}

// 关闭浏览器(浏览器) → 空 — 关闭浏览器
func builtinCloseBrowser(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("关闭浏览器() 期望 1 个参数，得到 %d 个", len(args))
	}
	b, ok := args[0].(*object.Browser)
	if !ok {
		return newError("关闭浏览器() 参数必须是浏览器，得到 %s", args[0].Type())
	}
	b.Cancel()
	return NULL
}

// 打开页面(浏览器) → 页面 — 新建一个标签页
func builtinNewPage(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("打开页面() 期望 1 个参数，得到 %d 个", len(args))
	}
	b, ok := args[0].(*object.Browser)
	if !ok {
		return newError("打开页面() 参数必须是浏览器，得到 %s", args[0].Type())
	}

	ctx, cancel := context.WithCancel(b.Ctx())
	return object.NewPage(ctx, cancel, b)
}

// 关闭页面(页面) → 空 — 关闭标签页
func builtinClosePage(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("关闭页面() 期望 1 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("关闭页面() 参数必须是页面，得到 %s", args[0].Type())
	}
	p.Cancel()
	return NULL
}

// 导航(页面, url) → 空 — 导航到指定 URL（等待页面加载完成）
func builtinNavigate(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("导航() 期望 2 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("导航() 第一个参数必须是页面，得到 %s", args[0].Type())
	}
	urlStr, ok := args[1].(*object.String)
	if !ok {
		return newError("导航() 第二个参数必须是字符串（URL），得到 %s", args[1].Type())
	}

	// 等待页面加载完成（waitUntil:domcontentloaded 等价）
	ctx, cancel := context.WithTimeout(p.Ctx(), 60*time.Second)
	defer cancel()

	err := chromedp.Run(ctx,
		chromedp.Navigate(urlStr.Value),
		chromedp.WaitReady("body", chromedp.ByQuery),
	)
	if err != nil {
		return newError("导航() 失败: %s", err.Error())
	}
	return NULL
}

// 当前网址(页面) → 字符串 — 获取当前页面 URL（带重试，防止导航时 context canceled）
func builtinPageURL(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("当前网址() 期望 1 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("当前网址() 参数必须是页面，得到 %s", args[0].Type())
	}

	var urlStr string
	var err error
	for i := 0; i < 5; i++ {
		err = chromedp.Run(p.Ctx(), chromedp.Location(&urlStr))
		if err == nil {
			return &object.String{Value: urlStr}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return newError("当前网址() 失败: %s", err.Error())
}

// 页面标题(页面) → 字符串 — 获取页面标题
func builtinPageTitle(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("页面标题() 期望 1 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("页面标题() 参数必须是页面，得到 %s", args[0].Type())
	}

	var title string
	err := chromedp.Run(p.Ctx(), chromedp.Title(&title))
	if err != nil {
		return newError("页面标题() 失败: %s", err.Error())
	}
	return &object.String{Value: title}
}

// 等待元素(页面, 选择器, 超时ms) → 空 — 等待元素可见（对齐 Playwright waitFor({state:'visible'})）
// 用 chromedp.WaitVisible + 可见性 JS 检测双重保障
func builtinWaitElement(args ...object.Object) object.Object {
	if len(args) < 2 || len(args) > 3 {
		return newError("等待元素() 期望 2 或 3 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("等待元素() 第一个参数必须是页面，得到 %s", args[0].Type())
	}
	selector, ok := args[1].(*object.String)
	if !ok {
		return newError("等待元素() 第二个参数必须是字符串（选择器），得到 %s", args[1].Type())
	}
	sel := normalizeSelector(selector.Value)
	timeoutSec := 30.0
	if len(args) == 3 {
		if ms, ok := args[2].(*object.Integer); ok {
			timeoutSec = float64(ms.Value) / 1000.0
		}
	}

	ctx, cancel := context.WithTimeout(p.Ctx(), time.Duration(timeoutSec*float64(time.Second)))
	defer cancel()

	// WaitVisible 等待元素可见（display !== none && visibility !== hidden && offsetParent !== null）
	err := chromedp.Run(ctx, chromedp.WaitVisible(sel, chromedp.ByQuery))
	if err != nil {
		return newError("等待元素() 超时或失败: %s", err.Error())
	}
	return NULL
}

// 等待可见(页面, 选择器, 超时ms) → 布尔 — JS 轮询等待元素可见，返回布尔而非报错
// 选择器支持 | 分隔多选择器（如 "#a|#b|input[type=text]"）
func builtinWaitVisible(args ...object.Object) object.Object {
	if len(args) < 2 || len(args) > 3 {
		return newError("等待可见() 期望 2 或 3 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("等待可见() 第一个参数必须是页面，得到 %s", args[0].Type())
	}
	selector, ok := args[1].(*object.String)
	if !ok {
		return newError("等待可见() 第二个参数必须是字符串（选择器），得到 %s", args[1].Type())
	}
	sel := normalizeSelector(selector.Value)
	timeoutMs := int64(30000)
	if len(args) == 3 {
		if ms, ok := args[2].(*object.Integer); ok {
			timeoutMs = ms.Value
		}
	}

	// JS 检测脚本：检查元素存在且可见（CSS 逗号选择器 querySelector 原生支持）
	jsCheck := fmt.Sprintf(
		"(function(){var el=document.querySelector('%s');if(el&&el.offsetParent!==null)return '1';return '0';})()",
		escapeJSString(sel),
	)

	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for time.Now().Before(deadline) {
		var result string
		err := chromedp.Run(p.Ctx(),
			chromedp.Evaluate(jsCheck, &result, evalWithAwaitPromise),
		)
		if err == nil && result == "1" {
			return TRUE
		}
		time.Sleep(500 * time.Millisecond)
	}
	return FALSE
}

// 点击(页面, 选择器) → 空 — 点击元素（先等可见再点击）
// 选择器支持 | 分隔多选择器
func builtinClickElement(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("点击() 期望 2 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("点击() 第一个参数必须是页面，得到 %s", args[0].Type())
	}
	selector, ok := args[1].(*object.String)
	if !ok {
		return newError("点击() 第二个参数必须是字符串（选择器），得到 %s", args[1].Type())
	}
	sel := normalizeSelector(selector.Value)

	// 先等可见，再点击（对齐 Playwright 的 click 行为）
	err := chromedp.Run(p.Ctx(),
		chromedp.WaitVisible(sel, chromedp.ByQuery),
		chromedp.Click(sel),
	)
	if err != nil {
		return newError("点击() 失败: %s", err.Error())
	}
	return NULL
}

// 点击按钮(页面, 文本) → 布尔 — 按文本匹配点击按钮（对齐 Playwright getByRole('button', {name})）
// 支持 | 分隔多个文本，匹配任意一个即可。例如 "登录|Sign in|Continue"
func builtinClickButton(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("点击按钮() 期望 2 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("点击按钮() 第一个参数必须是页面，得到 %s", args[0].Type())
	}
	text, ok := args[1].(*object.String)
	if !ok {
		return newError("点击按钮() 第二个参数必须是字符串（按钮文本），得到 %s", args[1].Type())
	}

	// 构建文本匹配数组
	patterns := strings.Split(text.Value, "|")
	var jsPatterns []string
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p != "" {
			jsPatterns = append(jsPatterns, fmt.Sprintf("%q", p))
		}
	}
	if len(jsPatterns) == 0 {
		return FALSE
	}

	jsCode := fmt.Sprintf(
		"(function(){var names=[%s];var els=document.querySelectorAll('button, a, [role=button], input[type=submit], input[type=button]');for(var i=0;i<els.length;i++){var t=(els[i].textContent||els[i].value||'').trim();for(var j=0;j<names.length;j++){if(t.indexOf(names[j])>=0&&!els[i].disabled){els[i].click();return '1';}}}return '0';})()",
		strings.Join(jsPatterns, ","),
	)

	var result string
	err := chromedp.Run(p.Ctx(),
		chromedp.Evaluate(jsCode, &result, evalWithAwaitPromise),
	)
	if err != nil {
		return FALSE
	}
	return &object.Boolean{Value: result == "1"}
}

// 填写(页面, 选择器, 值) → 空 — 用原生 setter 填写输入框（对齐 Playwright el.fill()）
// 选择器支持 | 分隔多选择器
func builtinFillInput(args ...object.Object) object.Object {
	if len(args) != 3 {
		return newError("填写() 期望 3 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("填写() 第一个参数必须是页面，得到 %s", args[0].Type())
	}
	selector, ok := args[1].(*object.String)
	if !ok {
		return newError("填写() 第二个参数必须是字符串（选择器），得到 %s", args[1].Type())
	}
	value, ok := args[2].(*object.String)
	if !ok {
		return newError("填写() 第三个参数必须是字符串（值），得到 %s", args[2].Type())
	}
	sel := normalizeSelector(selector.Value)

	// 等元素可见
	err := chromedp.Run(p.Ctx(),
		chromedp.WaitVisible(sel, chromedp.ByQuery),
	)
	if err != nil {
		return newError("填写() 元素不可见: %s", err.Error())
	}

	// 用 JS 原生 setter 设置值（React/Vue 兼容）
	jsCode := fmt.Sprintf(
		"(function(){var el=document.querySelector('%s');if(!el)return 'no_input';el.focus();el.click();var setter=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value');if(setter&&setter.set)setter.set.call(el,%q);else el.value=%q;el.dispatchEvent(new Event('input',{bubbles:true}));el.dispatchEvent(new Event('change',{bubbles:true}));return 'ok';})()",
		escapeJSString(sel),
		value.Value,
		value.Value,
	)

	var result string
	err = chromedp.Run(p.Ctx(),
		chromedp.Evaluate(jsCode, &result, evalWithAwaitPromise),
	)
	if err != nil {
		return newError("填写() 失败: %s", err.Error())
	}
	if result != "ok" {
		return newError("填写() 元素未找到: %s", result)
	}
	return NULL
}

// 获取文本(页面, 选择器) → 字符串 — 获取元素的文本内容
func builtinGetText(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("获取文本() 期望 2 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("获取文本() 第一个参数必须是页面，得到 %s", args[0].Type())
	}
	selector, ok := args[1].(*object.String)
	if !ok {
		return newError("获取文本() 第二个参数必须是字符串（选择器），得到 %s", args[1].Type())
	}
	sel := normalizeSelector(selector.Value)

	var text string
	err := chromedp.Run(p.Ctx(),
		chromedp.WaitVisible(sel, chromedp.ByQuery),
		chromedp.Text(sel, &text),
	)
	if err != nil {
		return newError("获取文本() 失败: %s", err.Error())
	}
	return &object.String{Value: text}
}

// 获取属性(页面, 选择器, 属性名) → 字符串 — 获取元素的属性值
func builtinGetAttribute(args ...object.Object) object.Object {
	if len(args) != 3 {
		return newError("获取属性() 期望 3 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("获取属性() 第一个参数必须是页面，得到 %s", args[0].Type())
	}
	selector, ok := args[1].(*object.String)
	if !ok {
		return newError("获取属性() 第二个参数必须是字符串（选择器），得到 %s", args[1].Type())
	}
	attrName, ok := args[2].(*object.String)
	if !ok {
		return newError("获取属性() 第三个参数必须是字符串（属性名），得到 %s", args[2].Type())
	}
	sel := normalizeSelector(selector.Value)

	var value string
	err := chromedp.Run(p.Ctx(),
		chromedp.WaitVisible(sel, chromedp.ByQuery),
		chromedp.AttributeValue(sel, attrName.Value, &value, nil),
	)
	if err != nil {
		return newError("获取属性() 失败: %s", err.Error())
	}
	return &object.String{Value: value}
}

// evalWithAwaitPromise 让 chromedp.Evaluate 等待 Promise resolve
func evalWithAwaitPromise(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
	p.AwaitPromise = true
	return p
}

// 执行脚本(页面, js代码) → 字符串 — 在页面中执行 JavaScript 并返回结果（支持 async/await）
// 自动 awaitPromise + 重试 3 次，防止页面导航时 context canceled
func builtinEvalJS(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("执行脚本() 期望 2 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("执行脚本() 第一个参数必须是页面，得到 %s", args[0].Type())
	}
	jsCode, ok := args[1].(*object.String)
	if !ok {
		return newError("执行脚本() 第二个参数必须是字符串（JS 代码），得到 %s", args[1].Type())
	}

	var result string
	var err error
	// 重试 3 次，防止页面导航时 context canceled
	// 重试间隔 150ms（原 500ms 过慢，导致 OTP 填写等时序敏感场景超时）
	for i := 0; i < 3; i++ {
		err = chromedp.Run(p.Ctx(),
			chromedp.Evaluate(jsCode.Value, &result, evalWithAwaitPromise),
		)
		if err == nil {
			return &object.String{Value: result}
		}
		// context canceled 通常意味着页面正在跳转（OTP 提交成功等），
		// 不应继续重试，直接返回错误让上层判断
		if strings.Contains(err.Error(), "context canceled") {
			return newError("执行脚本() 页面导航中: %s", err.Error())
		}
		// execution context destroyed — 页面正在重载/跳转，JS 上下文已失效
		if strings.Contains(err.Error(), "Execution context was destroyed") || strings.Contains(err.Error(), "execution context destroyed") {
			return newError("执行脚本() 页面重载中: %s", err.Error())
		}
		// 找不到目标元素 — 页面 DOM 尚未就绪或选择器错误
		if strings.Contains(err.Error(), "Node with given id") && strings.Contains(err.Error(), "not found") {
			return newError("执行脚本() 元素不存在: %s", err.Error())
		}
		// 连接已断开 — 浏览器已关闭或 CDP 连接丢失
		if strings.Contains(err.Error(), "websocket") && (strings.Contains(err.Error(), "closed") || strings.Contains(err.Error(), "EOF")) {
			return newError("执行脚本() 浏览器连接断开: %s", err.Error())
		}
		time.Sleep(150 * time.Millisecond)
	}
	return newError("执行脚本() 失败: %s", err.Error())
}

// 等待跳转(页面, url包含, 超时ms) → 布尔 — 等待页面跳转到包含指定字符串的 URL
// 对齐 Playwright 的 waitForURL(callback)，支持 | 分隔多个匹配模式
func builtinWaitNavigation(args ...object.Object) object.Object {
	if len(args) < 2 || len(args) > 3 {
		return newError("等待跳转() 期望 2 或 3 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("等待跳转() 第一个参数必须是页面，得到 %s", args[0].Type())
	}
	urlPart, ok := args[1].(*object.String)
	if !ok {
		return newError("等待跳转() 第二个参数必须是字符串（URL 片段），得到 %s", args[1].Type())
	}
	timeoutSec := 60.0
	if len(args) == 3 {
		if ms, ok := args[2].(*object.Integer); ok {
			timeoutSec = float64(ms.Value) / 1000.0
		}
	}

	// 支持 | 分隔多个匹配模式
	patterns := strings.Split(urlPart.Value, "|")

	deadline := time.Now().Add(time.Duration(timeoutSec * float64(time.Second)))
	for time.Now().Before(deadline) {
		var curURL string
		err := chromedp.Run(p.Ctx(), chromedp.Location(&curURL))
		if err == nil {
			for _, pat := range patterns {
				if containsSubstring(curURL, strings.TrimSpace(pat)) {
					return TRUE
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return FALSE
}

// 截图(页面, 路径) → 空 — 对当前页面截图并保存到文件
func builtinScreenshot(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("截图() 期望 2 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("截图() 第一个参数必须是页面，得到 %s", args[0].Type())
	}
	path, ok := args[1].(*object.String)
	if !ok {
		return newError("截图() 第二个参数必须是字符串（文件路径），得到 %s", args[1].Type())
	}

	var buf []byte
	err := chromedp.Run(p.Ctx(), chromedp.FullScreenshot(&buf, 100))
	if err != nil {
		return newError("截图() 失败: %s", err.Error())
	}

	err = writeFileBytes(path.Value, buf)
	if err != nil {
		return newError("截图() 保存失败: %s", err.Error())
	}
	return NULL
}

// 设置视口(页面, 宽, 高) → 空 — 设置页面视口大小
func builtinSetViewport(args ...object.Object) object.Object {
	if len(args) != 3 {
		return newError("设置视口() 期望 3 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("设置视口() 第一个参数必须是页面，得到 %s", args[0].Type())
	}
	w, ok := args[1].(*object.Integer)
	if !ok {
		return newError("设置视口() 第二个参数必须是整数（宽度），得到 %s", args[1].Type())
	}
	h, ok := args[2].(*object.Integer)
	if !ok {
		return newError("设置视口() 第三个参数必须是整数（高度），得到 %s", args[2].Type())
	}

	err := chromedp.Run(p.Ctx(),
		chromedp.EmulateViewport(int64(w.Value), int64(h.Value)),
	)
	if err != nil {
		return newError("设置视口() 失败: %s", err.Error())
	}
	return NULL
}

// 等待(毫秒) → 空 — 在页面上下文中等待（用于轮询场景）
func builtinBrowserWait(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("等待() 期望 1 个参数，得到 %d 个", len(args))
	}
	ms, ok := args[0].(*object.Integer)
	if !ok {
		return newError("等待() 参数必须是整数（毫秒），得到 %s", args[0].Type())
	}
	time.Sleep(time.Duration(ms.Value) * time.Millisecond)
	return NULL
}

// 获取全部文本(页面) → 字符串 — 获取整个页面的文本内容
func builtinGetFullText(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("获取全部文本() 期望 1 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("获取全部文本() 参数必须是页面，得到 %s", args[0].Type())
	}

	var text string
	err := chromedp.Run(p.Ctx(), chromedp.Text("body", &text, chromedp.ByQuery))
	if err != nil {
		return newError("获取全部文本() 失败: %s", err.Error())
	}
	return &object.String{Value: text}
}

// 清空输入(页面, 选择器) → 空 — 用原生 setter 清空输入框（对齐 Playwright el.fill('')）
// 选择器支持 | 分隔多选择器
func builtinClearInput(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("清空输入() 期望 2 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("清空输入() 第一个参数必须是页面，得到 %s", args[0].Type())
	}
	selector, ok := args[1].(*object.String)
	if !ok {
		return newError("清空输入() 第二个参数必须是字符串（选择器），得到 %s", args[1].Type())
	}
	sel := normalizeSelector(selector.Value)

	// 用 JS 原生 setter 设空值
	jsCode := fmt.Sprintf(
		"(function(){var el=document.querySelector('%s');if(!el)return 'no_input';el.focus();var setter=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value');if(setter&&setter.set)setter.set.call(el,'');else el.value='';el.dispatchEvent(new Event('input',{bubbles:true}));el.dispatchEvent(new Event('change',{bubbles:true}));return 'ok';})()",
		escapeJSString(sel),
	)

	var result string
	err := chromedp.Run(p.Ctx(),
		chromedp.Evaluate(jsCode, &result, evalWithAwaitPromise),
	)
	if err != nil {
		return newError("清空输入() 失败: %s", err.Error())
	}
	return NULL
}

// 获取HTML(页面, 选择器) → 字符串 — 获取元素的 HTML 内容
func builtinGetHTML(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("获取HTML() 期望 2 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("获取HTML() 第一个参数必须是页面，得到 %s", args[0].Type())
	}
	selector, ok := args[1].(*object.String)
	if !ok {
		return newError("获取HTML() 第二个参数必须是字符串（选择器），得到 %s", args[1].Type())
	}
	sel := normalizeSelector(selector.Value)

	var html string
	err := chromedp.Run(p.Ctx(),
		chromedp.WaitReady(sel, chromedp.ByQuery),
		chromedp.OuterHTML(sel, &html),
	)
	if err != nil {
		return newError("获取HTML() 失败: %s", err.Error())
	}
	return &object.String{Value: html}
}

// 元素存在(页面, 选择器) → 布尔 — 检查元素是否存在且可见
// 选择器支持 | 分隔多选择器
func builtinElementExists(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("元素存在() 期望 2 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("元素存在() 第一个参数必须是页面，得到 %s", args[0].Type())
	}
	selector, ok := args[1].(*object.String)
	if !ok {
		return newError("元素存在() 第二个参数必须是字符串（选择器），得到 %s", args[1].Type())
	}
	sel := normalizeSelector(selector.Value)

	jsCode := fmt.Sprintf(
		"(function(){var el=document.querySelector('%s');if(el&&el.offsetParent!==null)return '1';return '0';})()",
		escapeJSString(sel),
	)

	var result string
	err := chromedp.Run(p.Ctx(),
		chromedp.Evaluate(jsCode, &result, evalWithAwaitPromise),
	)
	if err != nil {
		return FALSE
	}
	return &object.Boolean{Value: result == "1"}
}

// 元素数量(页面, 选择器) → 整数 — 返回匹配选择器的可见元素数量（对齐 Playwright locator.count()）
// 选择器支持 | 分隔多选择器
func builtinElementCount(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("元素数量() 期望 2 个参数，得到 %d 个", len(args))
	}
	p, ok := args[0].(*object.Page)
	if !ok {
		return newError("元素数量() 第一个参数必须是页面，得到 %s", args[0].Type())
	}
	selector, ok := args[1].(*object.String)
	if !ok {
		return newError("元素数量() 第二个参数必须是字符串（选择器），得到 %s", args[1].Type())
	}
	sel := normalizeSelector(selector.Value)

	jsCode := fmt.Sprintf(
		"(function(){var els=document.querySelectorAll('%s');var n=0;for(var i=0;i<els.length;i++){if(els[i].offsetParent!==null)n++;}return String(n);})()",
		escapeJSString(sel),
	)

	var result string
	err := chromedp.Run(p.Ctx(),
		chromedp.Evaluate(jsCode, &result, evalWithAwaitPromise),
	)
	if err != nil {
		return &object.Integer{Value: 0}
	}
	n := int64(0)
	fmt.Sscanf(result, "%d", &n)
	return &object.Integer{Value: n}
}

// --- 辅助函数 ---

// normalizeSelector 将 zuzo 风格的多选择器（用 | 分隔）转为 CSS 逗号语法
// 例: "#a|#b|input[type=text]" → "#a, #b, input[type=text]"
// CSS querySelector/querySelectorAll 原生支持逗号分隔，会返回第一个匹配的元素
func normalizeSelector(s string) string {
	if !strings.Contains(s, "|") {
		return s
	}
	parts := strings.Split(s, "|")
	var trimmed []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			trimmed = append(trimmed, p)
		}
	}
	return strings.Join(trimmed, ", ")
}

// escapeJSString 转义 JS 字符串中的特殊字符（单引号、反斜杠）
func escapeJSString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	return s
}

// containsSubstring 检查字符串是否包含子串
func containsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}

// writeFileBytes 将字节写入文件
func writeFileBytes(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}
