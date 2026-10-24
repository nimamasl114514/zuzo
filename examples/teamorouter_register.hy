// === TeamoRouter 自动注册（zuzo 版）===
// =====================================================
// 移植自 teamorouter-register.js（Playwright → zuzo 内置 chromedp 函数）
// 流程: mail.tm 临时邮箱 → 邮箱验证码登录 → 控制台自动创建 API Key → 提取 sk- key
//
// 用法:
//   zuzo teamorouter_register.hy                            # 注册 1 个（有头）
//   zuzo teamorouter_register.hy --headless                 # 无头
//   zuzo teamorouter_register.hy --count 5 --parallel 3 --headless --keygate
//   zuzo teamorouter_register.hy --email xx@mail.tm --mail-password 密码
//
// 依赖: 系统安装 Chrome 或 Edge（打开浏览器自动探测路径）

// --- 常量 ---
变量 BASE_URL = "https://teamorouter.com"
变量 MAIL_TM_API = "https://api.mail.tm"
变量 邮件超时 = 120000
变量 轮询间隔 = 1500
变量 输出默认 = "teamorouter-keys.json"

// ==================== 工具函数 ====================

// 日志: 带时间戳输出
函数 日志(消息) {
    打印(f"[{当前时间()}] {消息}")
}

// 随机字符串: 生成长度 n 的字母数字串
函数 随机字符串(n) {
    变量 字符集 = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
    变量 结果 = ""
    对于 i = 0; i < n; i = i + 1 {
        结果 = 结果 + 取字符(字符集, 随机整数(0, 61))
    }
    返回 结果
}

// 全同字符: 判断字符串是否所有字符相同（用于排除占位验证码）
函数 全同字符(s) {
    如果 长度(s) <= 1 { 返回 真 }
    变量 首 = 取字符(s, 0)
    对于 i = 1; i < 长度(s); i = i + 1 {
        如果 取字符(s, i) != 首 { 返回 假 }
    }
    返回 真
}

// 请求带重试: HTTP 请求，失败重试 3 次
// 成功返回响应哈希 {状态码, 响应体, 响应头}；彻底失败返回 {"失败": 错误消息}
// 用 执行/遇到 捕获错误（zuzo 内置错误对象在求值链中会被当作致命错误，必须捕获）
函数 请求带重试(方法, url, 体, 头) {
    变量 尝试 = 0
    当 真 {
        变量 错误消息 = ""
        执行 {
            返回 http请求(方法, url, 体, 头)
        } 遇到 e {
            错误消息 = 错误信息(e)
        }
        如果 尝试 >= 2 { 返回 {"失败": 错误消息} }
        尝试 = 尝试 + 1
        日志(f"请求失败（第 {尝试} 次），重试: {url} - {错误消息}")
        睡眠(1500)
    }
    返回 {"失败": "未知错误"}
}

// ==================== 安全浏览器操作（执行/遇到 包裹，失败不中止） ====================

// 安全导航: 导航失败返回假
函数 安全导航(页面, url) {
    变量 ok = 假
    执行 {
        导航(页面, url)
        ok = 真
    } 遇到 e {
        日志("导航失败: " + 错误信息(e))
    }
    返回 ok
}

// 安全填写: 填写失败返回假
函数 安全填写(页面, 选择器, 值) {
    变量 ok = 假
    执行 {
        填写(页面, 选择器, 值)
        ok = 真
    } 遇到 e { }
    返回 ok
}

// 安全截图: 失败忽略（调试用）
函数 安全截图(页面, 路径) {
    执行 {
        截图(页面, 路径)
    } 遇到 e { }
    返回 空
}

// 安全关闭页面: 失败忽略
函数 安全关闭页面(页面) {
    执行 {
        关闭页面(页面)
    } 遇到 e { }
    返回 空
}

// 安全关闭浏览器: 失败忽略
函数 安全关闭浏览器(浏览器) {
    执行 {
        关闭浏览器(浏览器)
    } 遇到 e { }
    返回 空
}

// ==================== mail.tm 临时邮箱 ====================

// mail登录: 登录已有 mail.tm 账号，返回 token
函数 mail登录(地址, 密码) {
    变量 响应 = 请求带重试("POST", MAIL_TM_API + "/token",
        json序列化({"address": 地址, "password": 密码}),
        {"Content-Type": "application/json"})
    如果 类型(响应["失败"]) == "字符串" { 返回 "" }
    变量 data = json解析(字符串(响应["响应体"]))
    如果 类型(data) == "哈希" { 返回 字符串(data["token"]) }
    返回 ""
}

// mail创建账号: 创建临时邮箱，返回 {address, password, token}（失败返回空哈希）
// 注意: /domains 可能返回 {hydra:member:[...]} 或直接返回数组，两者都兼容
函数 mail创建账号() {
    变量 失败 = {"address": "", "password": "", "token": ""}
    变量 响应 = 请求带重试("GET", MAIL_TM_API + "/domains", "", {"Accept": "application/json"})
    如果 类型(响应["失败"]) == "字符串" { 返回 失败 }
    变量 data = json解析(字符串(响应["响应体"]))
    变量 列表 = []
    如果 类型(data) == "数组" {
        列表 = data
    } 否则 如果 类型(data) == "哈希" 并且 类型(data["hydra:member"]) == "数组" {
        列表 = data["hydra:member"]
    }
    变量 域名 = ""
    对于 i = 0; i < 长度(列表); i = i + 1 {
        如果 列表[i]["isActive"] == 真 {
            域名 = 字符串(列表[i]["domain"])
            跳过
        }
    }
    如果 域名 == "" { 返回 失败 }

    变量 地址 = f"tr{毫秒时间戳()}{随机整数(0, 999)}@{域名}"
    变量 密码 = "Tm" + 随机字符串(12) + "!"

    变量 响应2 = 请求带重试("POST", MAIL_TM_API + "/accounts",
        json序列化({"address": 地址, "password": 密码}),
        {"Content-Type": "application/json"})
    如果 类型(响应2["失败"]) == "字符串" { 返回 失败 }

    变量 token = mail登录(地址, 密码)
    如果 token == "" { 返回 失败 }
    返回 {"address": 地址, "password": 密码, "token": token}
}

// mail列邮件: 获取邮箱消息列表
函数 mail列邮件(token) {
    变量 响应 = 请求带重试("GET", MAIL_TM_API + "/messages", "",
        {"Accept": "application/json", "Authorization": "Bearer " + token})
    如果 类型(响应["失败"]) == "字符串" { 返回 [] }
    变量 data = json解析(字符串(响应["响应体"]))
    如果 类型(data) == "哈希" 并且 类型(data["hydra:member"]) == "数组" {
        返回 data["hydra:member"]
    }
    返回 []
}

// mail读邮件: 读取单封邮件全文
函数 mail读邮件(token, id) {
    变量 响应 = 请求带重试("GET", MAIL_TM_API + "/messages/" + id, "",
        {"Accept": "application/json", "Authorization": "Bearer " + token})
    如果 类型(响应["失败"]) == "字符串" { 返回 空 }
    返回 json解析(字符串(响应["响应体"]))
}

// 从主题提取验证码: 主题中直接给出 6 位数字时采用（加速路径）
函数 从主题提取验证码(主题) {
    变量 m = 正则匹配(主题, "\\b(\\d{6})\\b")
    如果 类型(m) != "数组" 或者 长度(m) < 2 { 返回 "" }
    返回 字符串(m[1])
}

// 从正文提取验证码: 从邮件正文（text/html）提取 6 位验证码
函数 从正文提取验证码(正文) {
    变量 清洗 = 正则替换(正文, "<[^>]+>", " ")
    变量 m = 正则匹配(清洗, "\\b(\\d{6})\\b")
    如果 类型(m) != "数组" 或者 长度(m) < 2 { 返回 "" }
    变量 code = 字符串(m[1])
    // 排除占位符: 全同数字或 123456
    如果 全同字符(code) { 返回 "" }
    如果 code == "123456" { 返回 "" }
    返回 code
}

// mail等待验证码: 轮询收件箱直到拿到验证码
函数 mail等待验证码(token, 超时ms) {
    变量 开始 = 毫秒时间戳()
    当 毫秒时间戳() - 开始 < 超时ms {
        变量 邮件列表 = mail列邮件(token)
        对于 i = 0; i < 长度(邮件列表); i = i + 1 {
            变量 邮件 = 邮件列表[i]
            变量 主题 = 字符串(邮件["subject"])
            变量 m = 正则匹配(主题, "(?i)verification code|验证码|code|teamo")
            如果 类型(m) == "数组" {
                // 主题直出验证码（不需要拉全文）
                变量 code = 从主题提取验证码(主题)
                如果 code != "" { 返回 code }
                // 拉全文提取
                变量 全文 = mail读邮件(token, 字符串(邮件["id"]))
                如果 类型(全文) == "哈希" {
                    变量 正文 = 字符串(全文["text"]) + " " + 字符串(全文["html"])
                    code = 从正文提取验证码(正文)
                    如果 code != "" { 返回 code }
                }
            }
        }
        睡眠(轮询间隔)
    }
    返回 ""
}

// ==================== 浏览器辅助 ====================

// 轮询点击: 反复尝试按文本点击按钮直到成功或超时
函数 轮询点击(页面, 文本, 超时ms) {
    变量 开始 = 毫秒时间戳()
    当 毫秒时间戳() - 开始 < 超时ms {
        如果 点击按钮(页面, 文本) { 返回 真 }
        睡眠(500)
    }
    返回 假
}

// 等待JS为真: 轮询执行 JS 直到返回 "1" 或超时（JS 报错视为未满足）
函数 等待JS为真(页面, js, 超时ms) {
    变量 开始 = 毫秒时间戳()
    当 毫秒时间戳() - 开始 < 超时ms {
        变量 结果 = ""
        执行 {
            结果 = 执行脚本(页面, js)
        } 遇到 e { }
        如果 结果 == "1" { 返回 真 }
        睡眠(500)
    }
    返回 假
}

// 填输入框按名称: 按 aria-label/placeholder/name/id/label 正则匹配输入框并填入
// （对齐原 JS 的 getByRole('textbox', {name}) 语义，含 <label> 关联计算）
函数 填输入框按名称(页面, 正则源码, 值) {
    变量 js = "(function(){var re=new RegExp('" + 正则源码 + "','i');var els=document.querySelectorAll('input,textarea');for(var i=0;i<els.length;i++){var el=els[i];if(el.type==='hidden'||el.type==='submit'||el.type==='button'||el.type==='checkbox'||el.type==='radio'||el.type==='password')continue;var lab='';if(el.id){var l=document.querySelector('label[for=\"'+el.id+'\"]');if(l)lab=l.innerText||'';}if(!lab&&el.closest&&el.closest('label'))lab=el.closest('label').innerText||'';var label=(el.getAttribute('aria-label')||'')+' '+(el.placeholder||'')+' '+(el.name||'')+' '+(el.id||'')+' '+lab;if(re.test(label)&&el.offsetParent!==null){el.focus();var setter=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value');if(setter&&setter.set)setter.set.call(el,'" + 值 + "');else el.value='" + 值 + "';el.dispatchEvent(new Event('input',{bubbles:true}));el.dispatchEvent(new Event('change',{bubbles:true}));return 'ok';}}return 'none';})()"
    变量 结果 = ""
    执行 {
        结果 = 执行脚本(页面, js)
    } 遇到 e { }
    如果 结果 == "ok" { 返回 真 }
    返回 假
}

// 提取文本skkey: 从文本中提取 sk- 开头的 API Key
函数 提取文本skkey(文本) {
    变量 keys = 正则查找全部(文本, "\\b(sk-[A-Za-z0-9_-]{20,80})\\b")
    如果 类型(keys) == "数组" 并且 长度(keys) > 0 {
        如果 类型(keys[0]) == "数组" 并且 长度(keys[0]) >= 2 {
            返回 字符串(keys[0][1])
        }
    }
    返回 ""
}

// 提取页面skkey: 从当前页面全文提取 sk- key
函数 提取页面skkey(页面) {
    变量 文本 = 获取全部文本(页面)
    如果 类型(文本) != "字符串" { 返回 "" }
    返回 提取文本skkey(文本)
}

// 等待skkey: 轮询页面直到出现 sk- key（创建弹窗场景）
函数 等待skkey(页面, 超时ms) {
    变量 开始 = 毫秒时间戳()
    当 毫秒时间戳() - 开始 < 超时ms {
        变量 k = 提取页面skkey(页面)
        如果 k != "" { 返回 k }
        睡眠(700)
    }
    返回 ""
}

// ==================== keygate 入池 ====================

// 加入keygate池: 把 key POST 回 keygate，并触发健康检查
函数 加入keygate池(keygateUrl, 主key, 邮箱) {
    变量 base = keygateUrl
    如果 后缀(base, "/") { base = 裁剪(base, "/") }

    变量 本地 = 分割(邮箱, "@")[0]
    变量 m = 正则匹配(本地, "([A-Za-z0-9]{6})$")
    变量 名称 = "reg-"
    如果 类型(m) == "数组" 并且 长度(m) >= 2 { 名称 = 名称 + m[1] } 否则 { 名称 = 名称 + 取字符(本地, 0) }

    变量 响应 = 请求带重试("POST", base + "/api/keys",
        json序列化({"name": 名称, "value": 主key}),
        {"Content-Type": "application/json"})
    如果 类型(响应["失败"]) == "字符串" {
        日志("加入 keygate 池失败: " + 响应["失败"])
        返回 空
    }
    变量 状态码 = 整数(响应["状态码"])
    如果 状态码 >= 200 并且 状态码 < 300 {
        变量 data = json解析(字符串(响应["响应体"]))
        如果 类型(data) == "哈希" 并且 类型(data["id"]) == "字符串" {
            请求带重试("POST", base + "/api/keys/" + data["id"] + "/check", "", {})
        }
        日志("已自动加入 keygate 池: " + 名称)
    } 否则 {
        日志(f"加入 keygate 池失败: HTTP {状态码}")
    }
}

// ==================== 注册主流程 ====================

// 注册一个: 注册单个账号并提取 key
// 账号: {address, password, token}
// 返回: {email, password, apiKeys, mainKey, status, error}
函数 注册一个(浏览器, 账号, keygateUrl) {
    变量 页面 = 打开页面(浏览器)
    变量 结果 = {"email": 账号["address"], "password": 账号["password"],
        "apiKeys": [], "mainKey": "", "status": "failed", "error": ""}
    变量 主key = ""

    // 1. 打开首页
    日志("打开网站...")
    变量 导航结果 = 导航(页面, BASE_URL + "/zh")
    如果 类型(导航结果) == "错误" {
        结果["error"] = "导航失败: " + 字符串(导航结果)
        关闭页面(页面)
        返回 结果
    }

    // 2. 关闭 cookie 弹窗（没有就忽略）
    点击按钮(页面, "接受|同意|允许|全部接受")

    // 3. 点击"开始使用"
    如果 轮询点击(页面, "开始使用", 15000) == 假 {
        结果["error"] = "找不到「开始使用」入口"
        截图(页面, "debug_start.png")
        关闭页面(页面)
        返回 结果
    }
    日志("已点击开始使用")

    // 4+5. 等待对话框出现并切换到邮箱登录
    // 注意: Teamo 的对话框不是 role=dialog 元素，用"改用邮箱登录"按钮做锚点
    如果 轮询点击(页面, "改用邮箱登录|邮箱登录", 15000) == 假 {
        结果["error"] = "未出现登录/注册对话框"
        截图(页面, "debug_dialog.png")
        关闭页面(页面)
        返回 结果
    }
    日志("登录/注册对话框已打开")

    // 6. 填邮箱（placeholder 可能是 name@example.com，正则含 example 兜底）
    如果 填输入框按名称(页面, "邮箱|email|e-?mail|example", 账号["address"]) == 假 {
        // 兜底: CSS 选择器
        变量 兜底 = 填写(页面, "input[type=email]|input[name=email]|input[type=text]", 账号["address"])
        如果 类型(兜底) == "错误" {
            结果["error"] = "找不到邮箱输入框"
            截图(页面, "debug_email.png")
            关闭页面(页面)
            返回 结果
        }
    }
    日志("邮箱已填入: " + 账号["address"])

    // 7. 点击继续/发送验证码
    如果 轮询点击(页面, "继续|获取验证码|发送验证码|Next|Send", 20000) == 假 {
        结果["error"] = "找不到继续按钮"
        截图(页面, "debug_continue.png")
        关闭页面(页面)
        返回 结果
    }

    // 8. 等待验证码输入框出现
    变量 JS验证码框 = "(function(){var els=document.querySelectorAll('input');for(var i=0;i<els.length;i++){var el=els[i];if(el.offsetParent===null||el.type==='hidden'||el.type==='submit'||el.type==='button'||el.type==='checkbox'||el.type==='radio'||el.type==='password')continue;var lab='';if(el.id){var l=document.querySelector('label[for=\"'+el.id+'\"]');if(l)lab=l.innerText||'';}if(!lab&&el.closest&&el.closest('label'))lab=el.closest('label').innerText||'';var label=(el.getAttribute('aria-label')||'')+' '+(el.placeholder||'')+' '+(el.name||'')+' '+(el.id||'')+' '+(el.autocomplete||'')+' '+lab;if(/验证码|code|otp|verification|一次性/i.test(label))return '1';}return '0';})()"
    如果 等待JS为真(页面, JS验证码框, 20000) == 假 {
        结果["error"] = "验证码输入框未出现"
        截图(页面, "debug_code_wait.png")
        关闭页面(页面)
        返回 结果
    }
    日志("验证码已发送")

    // 9. 等待验证码邮件
    日志("等待验证码邮件...")
    变量 code = mail等待验证码(账号["token"], 邮件超时)
    如果 code == "" {
        结果["error"] = "等待验证码邮件超时"
        截图(页面, "debug_mail_timeout.png")
        关闭页面(页面)
        返回 结果
    }
    日志("验证码: " + code)

    // 10. 填验证码（单输入框填法，失败则兜底分段 OTP 填法）
    如果 填输入框按名称(页面, "验证码|code|otp|verification", code) == 假 {
        变量 分段JS = "(function(){var d='" + code + "';var sel='input[type=number],input[type=tel],input[maxlength],input[autocomplete=one-time-code],input[name=code],input[name=otp],input[inputmode=numeric]';var ins=document.querySelectorAll(sel);var vis=[];for(var i=0;i<ins.length;i++){if(ins[i].offsetParent===null||ins[i].type==='hidden')continue;vis.push(ins[i]);}var setter=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value');if(vis.length>=6){for(var j=0;j<6;j++){vis[j].focus();if(setter&&setter.set)setter.set.call(vis[j],d[j]);else vis[j].value=d[j];vis[j].dispatchEvent(new Event('input',{bubbles:true}));vis[j].dispatchEvent(new Event('change',{bubbles:true}));}return 'ok';}if(vis.length>0){vis[0].focus();if(setter&&setter.set)setter.set.call(vis[0],d);else vis[0].value=d;vis[0].dispatchEvent(new Event('input',{bubbles:true}));vis[0].dispatchEvent(new Event('change',{bubbles:true}));return 'ok';}return 'none';})()"
        变量 分段结果 = 执行脚本(页面, 分段JS)
        如果 类型(分段结果) != "字符串" 或者 分段结果 != "ok" {
            结果["error"] = "找不到验证码输入框"
            截图(页面, "debug_code.png")
            关闭页面(页面)
            返回 结果
        }
    }

    // 11. 点击登录/提交
    轮询点击(页面, "登录|注册|login|sign.?in|Submit|确认|继续", 15000)

    // 12. 等待登录完成（"改用邮箱登录"按钮消失 = 对话框关闭；登录后常驻 /zh，URL 不变化）
    变量 登录成功 = 假
    变量 开始等待 = 毫秒时间戳()
    当 毫秒时间戳() - 开始等待 < 8000 并且 登录成功 == 假 {
        如果 点击按钮(页面, "改用邮箱登录") == 假 { 登录成功 = 真 }
        睡眠(500)
    }
    如果 登录成功 { 日志("登录成功") } 否则 { 日志("登录状态未确认，继续尝试...") }

    // 13. 进入控制台 API Keys 页
    导航(页面, BASE_URL + "/dashboard?tab=api-keys")
    睡眠(1500)

    // 14. 确定性流程: 创建 API Key
    如果 轮询点击(页面, "创建 Key", 15000) {
        睡眠(500)
        // 填 Key 名称
        如果 填输入框按名称(页面, "API Key 名称|名称", "auto-register-key") == 假 {
            变量 兜底填 = 填写(页面, "input[type=text]|input", "auto-register-key")
            如果 类型(兜底填) == "错误" { 日志("填写 Key 名称失败") }
        }
        // 点击对话框内的"创建 Key"
        变量 JS点创建 = "(function(){var els=document.querySelectorAll('[role=dialog],dialog');var dlg=null;for(var i=0;i<els.length;i++){if(els[i].offsetParent===null)continue;dlg=els[i];break;}if(!dlg){var bs=document.querySelectorAll('button,a,[role=button]');for(var j=0;j<bs.length;j++){var t=(bs[j].textContent||'').trim();if(/^创建 ?key$/i.test(t)&&!bs[j].disabled){bs[j].click();return '1';}}return '0';}var bs2=dlg.querySelectorAll('button,a,[role=button]');for(var j=0;j<bs2.length;j++){var t=(bs2[j].textContent||'').trim();if(/^创建 ?key$/i.test(t)&&!bs2[j].disabled){bs2[j].click();return '1';}}return '0';})()"
        执行脚本(页面, JS点创建)
        睡眠(500)

        // 15. 等待"API Key 已创建"弹窗中的 sk- key
        主key = 等待skkey(页面, 15000)
        如果 主key != "" {
            日志("从创建弹窗提取到 API Key")
            结果["apiKeys"] = [主key]
            结果["mainKey"] = 主key
            点击按钮(页面, "完成")
        }
    }

    // 16. 兜底: 遍历可能的 key 页面路径扫描
    如果 主key == "" {
        日志("自动创建失败，兜底扫描页面...")
        主key = 提取页面skkey(页面)
        如果 主key == "" {
            变量 路径表 = ["/dashboard", "/dashboard/api-keys", "/dashboard/keys",
                "/dashboard/settings", "/settings", "/api-keys", "/profile",
                "/user/api-keys", "/user/tokens", "/account/api-keys"]
            对于 i = 0; i < 长度(路径表); i = i + 1 {
                导航(页面, BASE_URL + 路径表[i])
                睡眠(1500)
                主key = 提取页面skkey(页面)
                如果 主key != "" {
                    日志("在 " + 路径表[i] + " 找到 API Key")
                    跳过
                }
            }
        }
        如果 主key != "" {
            结果["apiKeys"] = [主key]
            结果["mainKey"] = 主key
        }
    }

    // 17. 收尾
    如果 主key != "" {
        结果["status"] = "success"
        日志("主 API Key 提取成功")
    } 否则 {
        结果["error"] = "未提取到 API Key"
        截图(页面, "debug_no_key.png")
    }

    关闭页面(页面)

    // 18. keygate 入池
    如果 keygateUrl != "" 并且 主key != "" {
        加入keygate池(keygateUrl, 主key, 账号["address"])
    }

    返回 结果
}

// ==================== 批量注册 ====================

// 批量注册: 共享浏览器 + 小幅度并行（上限 5，防风控）
函数 批量注册(数量, 并行度, 输出文件, keygateUrl) {
    如果 并行度 > 5 { 并行度 = 5 }
    如果 并行度 > 数量 { 并行度 = 数量 }
    如果 并行度 < 1 { 并行度 = 1 }
    日志(f"🚀 批量注册 {数量} 个账号（并行度 {并行度}）...")

    变量 浏览器 = 打开浏览器(真)
    如果 类型(浏览器) == "错误" {
        日志("打开浏览器失败: " + 字符串(浏览器))
        返回 空
    }

    // 原子计数器（channel 串行化）
    变量 计数通道 = 通道(1)
    发送(计数通道, 1)
    变量 结果通道 = 通道(数量)

    变量 取号 = 函数() {
        变量 n = 接收(计数通道)
        发送(计数通道, n + 1)
        返回 n
    }

    变量 工作 = 函数() {
        当 真 {
            变量 i = 取号()
            如果 i > 数量 { 返回 空 }
            // 开工前随机错峰 0~2s，别同时注册
            睡眠(随机整数(0, 2000))
            变量 账号 = mail创建账号()
            变量 结果 = {"email": "", "password": "", "apiKeys": [], "mainKey": "",
                "status": "failed", "error": "", "index": i}
            如果 类型(账号) == "哈希" 并且 账号["address"] != "" {
                变量 r = 注册一个(浏览器, 账号, keygateUrl)
                结果["email"] = r["email"]
                结果["password"] = r["password"]
                结果["apiKeys"] = r["apiKeys"]
                结果["mainKey"] = r["mainKey"]
                结果["status"] = r["status"]
                结果["error"] = r["error"]
            } 否则 {
                结果["error"] = "临时邮箱创建失败"
            }
            发送(结果通道, 结果)
        }
    }

    对于 w = 0; w < 并行度; w = w + 1 {
        并发(工作)
    }

    // 收集结果
    变量 结果数组 = []
    变量 成功数 = 0
    对于 n = 0; n < 数量; n = n + 1 {
        变量 结果 = 接收(结果通道)
        推入(结果数组, 结果)
        如果 结果["status"] == "success" { 成功数 = 成功数 + 1 }
        日志(f"账号 {结果["index"]}/{数量}: {结果["status"]} {结果["email"]}")
        如果 类型(结果["mainKey"]) == "字符串" 并且 结果["mainKey"] != "" {
            日志("  KEY: " + 结果["mainKey"])
        }
        如果 输出文件 != "" {
            追加文件(输出文件, f"账号 {结果["index"]}: {结果["status"]} | {结果["email"]} | {结果["mainKey"]}\n")
        }
    }

    关闭浏览器(浏览器)

    变量 汇总 = {"generatedAt": 当前时间(), "total": 数量, "succeeded": 成功数,
        "failed": 数量 - 成功数, "accounts": 结果数组}
    如果 输出文件 != "" {
        写文件(输出文件, json序列化(汇总))
    }
    日志(f"批量完成: 成功 {成功数} / {数量}")
}

// ==================== 入口 ====================

// 主流程: 解析命令行参数并执行
函数 主流程() {
    变量 i = 2
    变量 headless = 假
    变量 数量 = 1
    变量 并行度 = 3
    变量 输出文件 = 输出默认
    变量 邮箱 = ""
    变量 密码 = ""
    变量 keygateUrl = ""

    // 运行参数(0)=程序名，运行参数(1)=脚本名，运行参数(i) 越界返回 ""
    当 i < 128 {
        变量 参数 = 运行参数(i)
        如果 参数 == "--headless" {
            headless = 真
        } 否则 如果 参数 == "--count" {
            如果 i + 1 < 128 { 数量 = 整数(运行参数(i + 1)); i = i + 1 }
        } 否则 如果 参数 == "--parallel" {
            如果 i + 1 < 128 { 并行度 = 整数(运行参数(i + 1)); i = i + 1 }
        } 否则 如果 参数 == "--output" {
            如果 i + 1 < 128 { 输出文件 = 运行参数(i + 1); i = i + 1 }
        } 否则 如果 参数 == "--email" {
            如果 i + 1 < 128 { 邮箱 = 运行参数(i + 1); i = i + 1 }
        } 否则 如果 参数 == "--mail-password" {
            如果 i + 1 < 128 { 密码 = 运行参数(i + 1); i = i + 1 }
        } 否则 如果 参数 == "--keygate" {
            keygateUrl = "http://127.0.0.1:8080"
            如果 i + 1 < 128 并且 前缀(运行参数(i + 1), "--") == 假 {
                keygateUrl = 运行参数(i + 1)
                i = i + 1
            }
        }
        i = i + 1
    }

    如果 数量 > 1 {
        批量注册(数量, 并行度, 输出文件, keygateUrl)
        返回 空
    }

    // ---- 单账号 ----
    变量 账号 = {"address": "", "password": "", "token": ""}
    如果 邮箱 != "" {
        // 复用已有 mail.tm 邮箱
        如果 密码 == "" {
            日志("--email 需要配合 --mail-password（该 mail.tm 邮箱的密码）才能自动读取验证码")
            返回 空
        }
        变量 token = mail登录(邮箱, 密码)
        如果 token == "" {
            日志("邮箱登录失败: " + 邮箱)
            返回 空
        }
        账号 = {"address": 邮箱, "password": 密码, "token": token}
        日志("使用已有邮箱: " + 邮箱)
    } 否则 {
        账号 = mail创建账号()
        如果 账号["address"] == "" {
            日志("临时邮箱创建失败")
            返回 空
        }
        日志("临时邮箱: " + 账号["address"])
        日志("邮箱密码: " + 账号["password"])
    }

    变量 浏览器 = 打开浏览器(headless)
    如果 类型(浏览器) == "错误" {
        日志("打开浏览器失败: " + 字符串(浏览器))
        返回 空
    }

    变量 结果 = 注册一个(浏览器, 账号, keygateUrl)
    关闭浏览器(浏览器)

    如果 输出文件 != "" {
        写文件(输出文件, json序列化(结果))
        日志("结果已保存到: " + 输出文件)
    }
    如果 结果["status"] == "success" {
        日志("✅ 注册成功")
        日志("   KEY: " + 结果["mainKey"])
    } 否则 {
        日志("❌ 注册失败: " + 结果["error"])
    }
    返回 空
}

// 直接运行时才执行主流程（导入用于测试时不执行）
变量 当前脚本 = 运行参数(1)
如果 当前脚本 == "" 或者 后缀(当前脚本, "teamorouter_register.hy") {
    主流程()
}