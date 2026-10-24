// === NVIDIA Build 半自动注册机 (zuzo 版) ===
// 基于 nvidia_register.js 翻译
// - tmaily 纯 HTTP API 取号/收信（不开邮箱页）
// - 浏览器只跑 NVIDIA（hCaptcha 手动）
// - 验证码自动填 + 取 API key
// 输出: email|apikey (keys.txt 追加，stdout 同步)
//
// 用法:
//   zuzo nvidia_register.hy          # 注册 1 个
//   zuzo nvidia_register.hy 5        # 注册 5 个

// --- 常量 ---
变量 TMAILY = "https://tmaily.com"
变量 BUILD = "https://build.nvidia.com"
变量 OUT = "keys.txt"
变量 UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"
变量 CAPTCHA超时 = 300000
变量 邮件超时 = 180000

// --- 全局状态 ---
变量 sid = ""

// --- 辅助函数 ---
函数 日志(消息) {
    打印(f"[{当前时间()}] {消息}")
}

函数 随机名() {
    返回 f"dev-{随机整数(100000, 999999)}"
}

函数 随机密码() {
    变量 p = 环境变量("NV_PASS")
    如果 p != "" {
        返回 p
    }
    返回 f"Nv#Ai{随机整数(100000, 999999)}9aA!"
}

函数 构建请求头() {
    如果 sid != "" {
        返回 {"Accept": "*/*", "Referer": TMAILY + "/", "Origin": TMAILY, "User-Agent": UA, "Cookie": f"TMaily_sid={sid}"}
    }
    返回 {"Accept": "*/*", "Referer": TMAILY + "/", "Origin": TMAILY, "User-Agent": UA}
}

// --- TMaily HTTP API ---

函数 tmaily请求(路径, 方法, 请求体) {
    变量 响应 = http请求(方法, TMAILY + 路径, 请求体, 构建请求头())
    如果 类型(响应) == "错误" {
        返回 响应
    }
    // 解析 Set-Cookie 提取 sid
    变量 响应头 = 响应["响应头"]
    如果 类型(响应头) == "哈希" {
        变量 sc = 字符串(响应头["Set-Cookie"])
        如果 正则验证(sc, "TMaily_sid=") {
            变量 m = 正则匹配(sc, "TMaily_sid=([^;]+)")
            如果 类型(m) == "数组" 并且 长度(m) >= 2 {
                sid = 字符串(m[1])
            }
        }
    }
    返回 响应
}

函数 获取域名() {
    变量 响应 = tmaily请求("/domains", "GET", "")
    如果 类型(响应) == "错误" {
        返回 "hqpdf.com"
    }
    变量 body = json解析(字符串(响应["响应体"]))
    如果 类型(body) == "哈希" {
        变量 domains = body["domains"]
        如果 类型(domains) == "数组" 并且 长度(domains) > 0 {
            返回 字符串(domains[0])
        }
    }
    返回 "hqpdf.com"
}

函数 创建邮箱(域名) {
    变量 路径 = f"/generate?force=true&domain={url编码(域名)}"
    变量 响应 = tmaily请求(路径, "GET", "")
    如果 类型(响应) == "错误" {
        返回 ""
    }
    变量 body = json解析(字符串(响应["响应体"]))
    如果 类型(body) == "哈希" {
        返回 字符串(body["address"])
    }
    返回 ""
}

函数 获取邮件(地址) {
    变量 响应 = tmaily请求(f"/emails?address={url编码(地址)}", "GET", "")
    如果 类型(响应) == "错误" {
        返回 []
    }
    变量 body = json解析(字符串(响应["响应体"]))
    如果 类型(body) == "数组" {
        返回 body
    }
    返回 []
}

函数 提取验证码(文本) {
    变量 m = 正则匹配(文本, "Your verification code is:\\s*(\\d{3})[-\\s]?(\\d{3})")
    如果 类型(m) == "数组" 并且 长度(m) >= 3 {
        返回 m[1] + m[2]
    }
    m = 正则匹配(文本, "\\b(\\d{3})[-\\s](\\d{3})\\b")
    如果 类型(m) == "数组" 并且 长度(m) >= 3 {
        返回 m[1] + m[2]
    }
    m = 正则匹配(文本, "\\b(\\d{6})\\b")
    如果 类型(m) == "数组" 并且 长度(m) >= 2 {
        返回 m[1]
    }
    返回 ""
}

函数 等待邮件验证码(地址, 超时ms) {
    变量 开始 = 毫秒时间戳()
    当 毫秒时间戳() - 开始 < 超时ms {
        变量 邮件列表 = 获取邮件(地址)
        对于 i = 0; i < 长度(邮件列表); i = i + 1 {
            变量 邮件 = 邮件列表[i]
            变量 内容 = f"{邮件["subject"]} {邮件["text"]} {邮件["html"]}"
            变量 code = 提取验证码(内容)
            如果 code != "" {
                返回 code
            }
        }
        睡眠(800)
    }
    返回 ""
}

// --- 浏览器自动化辅助 ---

// 勾选注册页所有协议/保持登录 checkbox
函数 勾选协议(页面) {
    执行脚本(页面, "(function(){var cbs=document.querySelectorAll('input[type=checkbox]');for(var i=0;i<cbs.length;i++){if(!cbs[i].disabled&&!cbs[i].checked){var b=cbs[i].getBoundingClientRect();if(b.width>=1&&b.height>=1)cbs[i].click();}}return 'ok';})()")
}

// 等待 hCaptcha 完成（手动），检测 token + 按钮状态，连续 3 次就绪后点击注册
函数 等待验证码并注册(页面) {
    变量 开始 = 毫秒时间戳()
    变量 就绪次数 = 0

    当 毫秒时间戳() - 开始 < CAPTCHA超时 {
        变量 href = 当前网址(页面)
        如果 类型(href) != "字符串" {
            睡眠(700)
            跳过
        }
        // 已离开注册页 = 成功
        如果 正则验证(href, "profile-complete|verify") {
            返回 "完成"
        }
        如果 正则验证(href, "create-account") == 假 并且 正则验证(href, "login.nvgs") == 假 {
            返回 "完成"
        }

        勾选协议(页面)

        // 通过 JS 检测 hCaptcha token 和按钮状态
        变量 状态json = 执行脚本(页面, "(function(){var btn=document.querySelector('#register_button, button[type=submit]');if(!btn){var bs=document.querySelectorAll('button, [role=button]');for(var i=0;i<bs.length;i++){var t=bs[i].textContent||'';if(t.indexOf('Create account')>=0||t.indexOf('创建账户')>=0){btn=bs[i];break;}}}var token='';var ta=document.querySelector('textarea[name=h-captcha-response]');if(ta&&ta.value)token=ta.value;if(!token){var tas=document.querySelectorAll('textarea');for(var i=0;i<tas.length;i++){if(tas[i].value&&tas[i].value.length>20){token=tas[i].value;break;}}}var en=btn&&!btn.disabled&&btn.getAttribute('aria-disabled')!=='true';var sc=location.href.indexOf('create-account')>=0;return JSON.stringify({enabled:en,tokenLen:token.length,stillCreate:sc});})()")

        如果 类型(状态json) == "字符串" {
            变量 状态 = json解析(状态json)
            如果 类型(状态) == "哈希" {
                如果 状态["stillCreate"] == 假 {
                    返回 "完成"
                }

                变量 tokenOk = 状态["tokenLen"] >= 50
                变量 btnOk = 状态["enabled"] == 真

                如果 tokenOk 并且 btnOk {
                    就绪次数 = 就绪次数 + 1
                } 否则 {
                    就绪次数 = 0
                }

                如果 就绪次数 >= 3 {
                    日志("确认 hCaptcha 有效，点击创建账户")
                    点击按钮(页面, "Create account|创建账户")
                    睡眠(25000)

                    变量 url2 = 当前网址(页面)
                    如果 类型(url2) == "字符串" 并且 正则验证(url2, "create-account") == 假 {
                        返回 "完成"
                    }
                    日志("仍在注册页，重置等待")
                    就绪次数 = 0
                    睡眠(2000)
                }
            }
        }
        睡眠(700)
    }
    返回 "超时"
}

// 清空 OTP 输入框
函数 清空OTP(页面) {
    执行脚本(页面, "(function(){var sel='input[type=number], input[type=tel], input[maxlength], input[autocomplete=one-time-code], input[name=code], input[name=otp], input[name=verificationCode], input[name=pin], input[pattern], input[inputmode=numeric]';var ins=document.querySelectorAll(sel);var setter=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value');for(var i=0;i<ins.length;i++){ins[i].focus();if(setter&&setter.set)setter.set.call(ins[i],'');else ins[i].value='';ins[i].dispatchEvent(new Event('input',{bubbles:true}));ins[i].dispatchEvent(new Event('change',{bubbles:true}));}return 'ok';})()")
}

// 检测 OTP 错误提示（验证码无效）
函数 OTP错误可见(页面) {
    变量 结果 = 执行脚本(页面, "(function(){if(document.querySelector('.nv-notice.notice-alert'))return '1';var t=(document.body&&document.body.innerText||'').replace(/\\s+/g,'');if(/验证码无效|PinCodeInvalid|invalid(?:verification)?code|code(?:is)?invalid|incorrectcode/i.test(t))return '1';return '0';})()")
    如果 类型(结果) == "字符串" 并且 结果 == "1" {
        返回 真
    }
    返回 假
}

// 检测 OTP 输入框是否还在（还在 = 没提交成功）
// 广泛匹配多种 OTP 输入框
函数 OTP输入框还在(页面) {
    变量 结果 = 执行脚本(页面, "(function(){var sel='input[type=number], input[type=tel], input[maxlength], input[autocomplete=one-time-code], input[name=code], input[name=otp], input[name=verificationCode], input[name=pin], input[pattern], input[inputmode=numeric]';var ins=document.querySelectorAll(sel);for(var i=0;i<ins.length;i++){if(ins[i].offsetParent!==null)return '1';}return '0';})()")
    如果 类型(结果) == "字符串" 并且 结果 == "1" {
        返回 真
    }
    返回 假
}

// 点击"请求新验证码"重发
函数 重发验证码(页面) {
    // 点击 #request_new_code_link 或 #request_new_code，或文本匹配的重发链接
    变量 结果 = 执行脚本(页面, "(function(){var el=document.querySelector('#request_new_code_link, #request_new_code');if(el){el.click();return '1';}var as=document.querySelectorAll('a');for(var i=0;i<as.length;i++){var t=(as[i].textContent||'').trim();if(t.indexOf('请求新验证码')>=0||t.indexOf('重新请求')>=0||/resend/i.test(t)){as[i].click();return '1';}}return '0';})()")
    如果 类型(结果) == "字符串" 并且 结果 == "1" {
        日志("已点击重发验证码")
        睡眠(800)
        清空OTP(页面)
        返回 真
    }
    日志("未找到重发验证码入口")
    返回 假
}

// 填写 OTP 验证码并等待结果
// 返回 "成功" / "无效" / "超时"
函数 填写OTP并等待(页面, 验证码) {
    // 第 0 步：诊断 — 打印页面上所有 input 的信息
    变量 诊断 = 执行脚本(页面, "(function(){var ins=document.querySelectorAll('input');var info=[];for(var i=0;i<ins.length;i++){info.push(i+':type='+ins[i].type+',name='+(ins[i].name||'')+',id='+(ins[i].id||'')+',maxlen='+(ins[i].maxLength||-1)+',ac='+(ins[i].autocomplete||'')+',vis='+(ins[i].offsetParent!==null));}return info.join(' | ');})()")
    日志(f"OTP页面inputs: {诊断}")

    // 第 1 步：尝试策略 A — 广泛选择器 + 逐字符填入
    变量 jsA = "(function(){var d='" + 验证码 + "';var sel='input[type=number], input[type=tel], input[maxlength], input[autocomplete=one-time-code], input[name=code], input[name=otp], input[name=verificationCode], input[name=pin], input[pattern], input[inputmode=numeric]';var ins=document.querySelectorAll(sel);var vis=[];for(var i=0;i<ins.length;i++){if(ins[i].offsetParent===null||ins[i].type==='hidden')continue;vis.push(ins[i]);}var setter=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value');if(vis.length>=6){for(var i=0;i<6;i++){vis[i].focus();vis[i].select&&vis[i].select();if(setter&&setter.set)setter.set.call(vis[i],d[i]);else vis[i].value=d[i];vis[i].dispatchEvent(new Event('input',{bubbles:true}));vis[i].dispatchEvent(new Event('change',{bubbles:true}));vis[i].dispatchEvent(new KeyboardEvent('keyup',{bubbles:true}));}return 'A_multi:'+vis.length;}else if(vis.length>0){vis[0].focus();vis[0].select&&vis[0].select();if(setter&&setter.set)setter.set.call(vis[0],d);else vis[0].value=d;vis[0].dispatchEvent(new Event('input',{bubbles:true}));vis[0].dispatchEvent(new Event('change',{bubbles:true}));vis[0].dispatchEvent(new KeyboardEvent('keyup',{bubbles:true}));return 'A_single:'+vis.length;}return 'A_none';})()"
    变量 结果A = 执行脚本(页面, jsA)
    日志(f"策略A: {结果A}")

    // 第 2 步：如果策略 A 没找到，尝试策略 B — 所有可见 input
    如果 类型(结果A) == "字符串" 并且 包含(结果A, "none") {
        变量 jsB = "(function(){var d='" + 验证码 + "';var ins=document.querySelectorAll('input');var vis=[];for(var i=0;i<ins.length;i++){if(ins[i].type==='hidden'||ins[i].type==='submit'||ins[i].type==='button'||ins[i].type==='checkbox'||ins[i].type==='password'||ins[i].type==='email')continue;vis.push(ins[i]);}var setter=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value');if(vis.length>=6){for(var i=0;i<6;i++){vis[i].focus();if(setter&&setter.set)setter.set.call(vis[i],d[i]);else vis[i].value=d[i];vis[i].dispatchEvent(new Event('input',{bubbles:true}));vis[i].dispatchEvent(new Event('change',{bubbles:true}));}return 'B_multi:'+vis.length;}else if(vis.length>0){vis[0].focus();if(setter&&setter.set)setter.set.call(vis[0],d);else vis[0].value=d;vis[0].dispatchEvent(new Event('input',{bubbles:true}));vis[0].dispatchEvent(new Event('change',{bubbles:true}));return 'B_single:'+vis.length;}return 'B_none';})()"
        变量 结果B = 执行脚本(页面, jsB)
        日志(f"策略B: {结果B}")
    }

    // 第 3 步：短暂等待 DOM 更新
    睡眠(300)

    // 第 4 步：点击提交按钮（排除"重发验证码"等干扰按钮）
    变量 jsClick = "(function(){var names=['Continue','继续','Verify','验证','Submit','提交','Next','下一步'];var exclude=['请求新验证码','重新请求','resend','重发','request_new'];var els=document.querySelectorAll('button, a, [role=button], input[type=submit], input[type=button]');for(var i=0;i<els.length;i++){var t=(els[i].textContent||els[i].value||'').trim();var tl=t.toLowerCase();var skip=false;for(var k=0;k<exclude.length;k++){if(tl.indexOf(exclude[k].toLowerCase())>=0){skip=true;break;}}if(skip)continue;for(var j=0;j<names.length;j++){if(t.indexOf(names[j])>=0&&!els[i].disabled){els[i].click();return 'clicked:'+t;}}}return 'no_btn';})()"
    变量 点击结果 = 执行脚本(页面, jsClick)
    日志(f"点击: {点击结果}")

    // 第 5 步：如果执行脚本报错（页面导航中），说明提交成功
    如果 类型(点击结果) == "错误" {
        变量 错误信息 = 字符串(点击结果)
        如果 包含(错误信息, "导航中") 或者 包含(错误信息, "context canceled") {
            日志("页面导航中，OTP 可能已成功")
            返回 "成功"
        }
    }

    // 第 6 步：等待结果，最多 30 秒
    变量 开始 = 毫秒时间戳()
    当 毫秒时间戳() - 开始 < 30000 {
        如果 OTP错误可见(页面) {
            返回 "无效"
        }
        变量 href = 当前网址(页面)
        如果 类型(href) == "字符串" {
            如果 正则验证(href, "build.nvidia.com|cloudaccounts|consent") {
                返回 "成功"
            }
            如果 正则验证(href, "profile-complete|verify") == 假 并且 OTP输入框还在(页面) == 假 {
                返回 "成功"
            }
        }
        如果 OTP输入框还在(页面) == 假 {
            返回 "成功"
        }
        睡眠(300)
    }
    // 超时仍停在 OTP
    如果 OTP错误可见(页面) {
        返回 "无效"
    }
    如果 OTP输入框还在(页面) {
        返回 "超时"
    }
    返回 "成功"
}

// 通过浏览器 fetch 调用 NGC API 获取 API Key（共享浏览器 cookie）
函数 提取API密钥(页面) {
    变量 开始 = 毫秒时间戳()
    变量 js = "(async()=>{try{var r=await fetch('https://api.ngc.nvidia.com/user-context',{headers:{accept:'application/json'},credentials:'include'});if(!r.ok)return JSON.stringify({error:'uc_'+r.status});var uc=await r.json();var org=uc.orgName;if(!org)return JSON.stringify({error:'no_org'});var kr=await fetch('https://api.ngc.nvidia.com/v3/orgs/'+org+'/keys/type/AI_PLAYGROUNDS_KEY',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({expiryDate:'2126-04-08T07:00:00Z',name:'dev',type:'AI_PLAYGROUNDS_KEY',policies:[{product:'nv-cloud-functions',scopes:['invoke_function'],resources:[{id:'*',type:'account-functions'}]}]}),credentials:'include'});var kb=await kr.json();var ak=kb.apiKey?kb.apiKey.value:(kb.result&&kb.result.apiKey?kb.result.apiKey.value:'');if(!ak)return JSON.stringify({error:'no_key'});return JSON.stringify({orgName:org,email:uc.name||uc.email||org,apiKey:ak});}catch(e){return JSON.stringify({error:e.message});}})()"

    当 毫秒时间戳() - 开始 < 90000 {
        变量 结果json = 执行脚本(页面, js)
        如果 类型(结果json) == "字符串" {
            变量 结果 = json解析(结果json)
            如果 类型(结果) == "哈希" 并且 类型(结果["apiKey"]) == "字符串" {
                日志("获取 API Key 成功")
                返回 结果["apiKey"]
            }
            如果 类型(结果) == "哈希" 并且 类型(结果["error"]) == "字符串" {
                日志(f"API 调用: {结果["error"]}")
            }
        }
        睡眠(2000)
    }
    返回 ""
}

// --- 主注册流程 ---

函数 注册一个(浏览器) {
    变量 页面 = 打开页面(浏览器)
    变量 密码 = 随机密码()

    // 1. tmaily 纯 API 取号
    日志("正在获取临时邮箱...")
    变量 域名 = 获取域名()
    变量 邮箱 = 创建邮箱(域名)
    如果 邮箱 == "" {
        日志("获取邮箱失败")
        关闭页面(页面)
        返回 空
    }
    日志(f"邮箱: {邮箱} 密码: {密码}")

    // 2. build 登录弹窗
    日志("正在打开 NVIDIA Build...")
    导航(页面, BUILD + "/?modal=signin")

    // 3. 等待邮箱输入框出现（多选择器一行搞定）
    日志("等待邮箱输入框...")
    变量 邮箱就绪 = 等待可见(页面, "input[name=email]|#email|input[type=email]", 30000)
    如果 邮箱就绪 == 假 {
        日志("邮箱输入框等待超时")
        截图(页面, "debug_email.png")
        关闭页面(页面)
        返回 空
    }

    // 用 填写() 设置邮箱（原生 setter + 多选择器）
    填写(页面, "input[name=email]|#email|input[type=email]", 邮箱)
    日志("填邮箱完成")
    睡眠(500)

    // 点击 Next 按钮
    变量 点Next结果 = 点击按钮(页面, "Next|下一步|Continue|继续")
    日志(f"点Next: {点Next结果}")

    // 4. 等待跳转到 nvgs 登录/注册
    日志("等待跳转到登录/注册页...")
    变量 跳转成功 = 等待跳转(页面, "login.nvgs.nvidia|login.nvidia|accounts.nvgs", 60000)

    如果 跳转成功 == 假 {
        日志("跳转超时")
        截图(页面, "debug_novgs.png")
        关闭页面(页面)
        返回 空
    }

    // 4b. 如果在 identifier 页面，需要先填邮箱点 Next 跳到密码页
    变量 curUrl1 = 当前网址(页面)
    日志(f"登录页URL: {curUrl1}")
    如果 类型(curUrl1) == "字符串" 并且 正则验证(curUrl1, "/identifier") {
        日志("identifier 页，填邮箱点 Next...")
        填写(页面, "input[name=email]|#email|input[type=email]", 邮箱)
        睡眠(500)
        点击按钮(页面, "Next|下一步|Continue|继续")
        日志("已点 Next，等待密码页...")
    }

    // 等待密码框可见（多选择器一行搞定）
    日志("等待密码框...")
    变量 密码框就绪 = 等待可见(页面, "#registration_password|#password|input[type=password]", 30000)
    如果 密码框就绪 == 假 {
        日志("密码框等待超时")
        截图(页面, "debug_password.png")
        关闭页面(页面)
        返回 空
    }
    睡眠(1000)

    // 5. 判断注册还是登录（URL + 密码框双重检测）
    变量 url = 当前网址(页面)
    日志(f"当前URL: {url}")
    变量 是注册 = 假
    如果 类型(url) == "字符串" {
        是注册 = 正则验证(url, "create-account")
    }
    // 也检查页面上是否有密码框（URL 可能还没变）
    如果 元素存在(页面, "#registration_password|#password") {
        是注册 = 真
    }
    日志(f"是注册页: {是注册}")

    如果 是注册 {
        日志("注册页，填密码")
        填写(页面, "#registration_password|#password|input[type=password]", 密码)
        日志("填密码完成")
        // 确认密码框（如果有）
        如果 元素存在(页面, "#registration_passwordConfirm|#passwordConfirm|input[name=passwordConfirm]") {
            填写(页面, "#registration_passwordConfirm|#passwordConfirm|input[name=passwordConfirm]", 密码)
        }
        勾选协议(页面)

        // 6. 等待 hCaptcha + 注册
        日志(">>> 请完成 hCaptcha；验证码/后续自动")
        变量 注册结果 = 等待验证码并注册(页面)
        日志(f"注册结果: {注册结果}")
    } 否则 {
        日志("登录页，填密码")
        填写(页面, "#password|input[type=password]", 密码)
        睡眠(500)
        变量 登录结果 = 点击按钮(页面, "登录|Sign in|Log in|Continue|继续")
        日志(f"点登录: {登录结果}")
    }

    // 7. 等待跳转到 profile-complete / verify / email 页面（最多 90 秒）
    日志("等待注册/登录完成跳转...")
    变量 跳转完成 = 等待跳转(页面, "profile-complete|verify|email|build.nvidia.com|consent|cloudaccounts", 90000)
    如果 跳转完成 == 假 {
        变量 最终url2 = 当前网址(页面)
        日志(f"等待跳转超时，当前: {最终url2}")
        截图(页面, "debug_stuck.png")
    }

    // 7b. 检查是否出现 OTP 输入框
    睡眠(2000)
    变量 otp存在 = OTP输入框还在(页面)

    如果 otp存在 {
        变量 otp成功 = 假
        变量 已用验证码 = []
        对于 尝试 = 1; 尝试 <= 3 并且 otp成功 == 假; 尝试 = 尝试 + 1 {
            日志(f"等待验证码邮件... ({尝试}/3)")

            // 第 2 次开始先点重发
            如果 尝试 > 1 {
                重发验证码(页面)
            }

            变量 code = 等待邮件验证码(邮箱, 邮件超时)
            如果 code != "" {
                日志(f"验证码: {code}")
                变量 otp结果 = 填写OTP并等待(页面, code)
                日志(f"OTP 结果: {otp结果}")
                如果 otp结果 == "成功" {
                    otp成功 = 真
                } 否则 {
                    日志("验证码无效或超时，准备重试")
                    睡眠(1000)
                }
            } 否则 {
                日志("未收到验证码邮件")
            }
        }
        如果 otp成功 == 假 {
            日志("邮箱验证码失败")
            截图(页面, "debug_otp_fail.png")
            关闭页面(页面)
            返回 空
        }
    }

    // 8. 后续跳转：consent / cloud account / build
    变量 t1 = 毫秒时间戳()
    变量 完成 = 假
    当 毫秒时间戳() - t1 < 180000 并且 完成 == 假 {
        变量 href = 当前网址(页面)
        如果 类型(href) != "字符串" {
            睡眠(2000)
            跳过
        }
        日志(f"当前: {href}")

        如果 正则验证(href, "build.nvidia.com") 并且 正则验证(href, "modal=") == 假 {
            完成 = 真
        } 否则 如果 正则验证(href, "consent|static-login.nvidia") {
            日志("提交隐私同意")
            点击按钮(页面, "Submit|Agree|同意|提交")
            睡眠(2000)
        } 否则 如果 正则验证(href, "cloudaccounts.nvidia") {
            日志("创建 NVIDIA Cloud Account")
            变量 名 = 随机名()
            执行脚本(页面, "(function(){var inp=document.querySelector('input[type=text]');if(inp){inp.focus();inp.value='" + 名 + "';inp.dispatchEvent(new Event('input',{bubbles:true}));}var bs=document.querySelectorAll('button');for(var i=0;i<bs.length;i++){var t=bs[i].textContent||'';if(t.indexOf('Create')>=0||t.indexOf('创建')>=0){bs[i].click();break;}}return 'ok';})()")
            睡眠(5000)
        } 否则 如果 正则验证(href, "profile-complete") {
            点击按钮(页面, "Continue|继续|Submit|提交|Next")
            睡眠(2000)
        } 否则 如果 正则验证(href, "create-account") {
            勾选协议(页面)
            等待验证码并注册(页面)
        } 否则 {
            睡眠(1500)
        }
    }

    // 9. 确保在 build
    变量 最终url = 当前网址(页面)
    如果 类型(最终url) == "字符串" 并且 正则验证(最终url, "build.nvidia.com") == 假 {
        导航(页面, BUILD + "/")
        睡眠(2000)
    }

    // 10. 提取 API Key
    日志("正在获取 API Key...")
    变量 apiKey = 提取API密钥(页面)
    如果 apiKey == "" {
        日志("获取 API Key 失败")
        关闭页面(页面)
        返回 空
    }

    // 11. 保存结果
    变量 行 = f"{邮箱}|{apiKey}"
    追加文件(OUT, 行 + "\n")
    打印(行)
    日志("成功!")

    关闭页面(页面)
}

// --- 入口 ---
日志("=== NVIDIA Build 半自动注册机 (zuzo 版) ===")
变量 数量 = 1
如果 运行参数(2) != "" {
    数量 = 整数(运行参数(2))
}
日志(f"注册数量: {数量}（仅 hCaptcha 需人工）")

变量 浏览器 = 打开浏览器(假)

对于 i = 0; i < 数量; i = i + 1 {
    日志(f"======== {i + 1}/{数量} ========")
    注册一个(浏览器)
    如果 i + 1 < 数量 {
        睡眠(500)
    }
}

关闭浏览器(浏览器)
日志("完成")
