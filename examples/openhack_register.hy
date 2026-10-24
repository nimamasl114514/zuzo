// === OpenHack 半自动注册机 (zuzo 版) ===
// 基于 nvidia_register.hy 改编
// - tmaily 纯 HTTP API 取号/收信（不开邮箱页）
// - 浏览器只跑 OpenHack（WorkOS 认证，bot 检测手动）
// - 注册流程: 邮箱 → Email sign-in code → 验证码 → 完成
// - 邮件验证码自动填 + 取 API key
// 输出: email|apikey (openhack_keys.txt 追加，stdout 同步)
//
// 用法:
//   zuzo openhack_register.hy          # 注册 1 个
//   zuzo openhack_register.hy 5        # 注册 5 个

// --- 常量 ---
变量 TMAILY = "https://tmaily.com"
变量 APP = "https://app.openhack.com"
变量 AUTH = "https://auth.openhack.com"
变量 OUT = "openhack_keys.txt"
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
    变量 p = 环境变量("OH_PASS")
    如果 p != "" {
        返回 p
    }
    返回 f"Oh#Sec{随机整数(100000, 999999)}9aA!"
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
    // WorkOS 验证码通常是 6 位数字
    变量 m = 正则匹配(文本, "verification code[:\\s]*(\\d{6})")
    如果 类型(m) == "数组" 并且 长度(m) >= 2 {
        返回 m[1]
    }
    m = 正则匹配(文本, "code[:\\s]*(\\d{6})")
    如果 类型(m) == "数组" 并且 长度(m) >= 2 {
        返回 m[1]
    }
    m = 正则匹配(文本, "\\b(\\d{6})\\b")
    如果 类型(m) == "数组" 并且 长度(m) >= 2 {
        返回 m[1]
    }
    m = 正则匹配(文本, "\\b(\\d{3})[-\\s]?(\\d{3})\\b")
    如果 类型(m) == "数组" 并且 长度(m) >= 3 {
        返回 m[1] + m[2]
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

// 检测并处理 WorkOS bot 验证
函数 等待Bot验证通过(页面) {
    变量 开始 = 毫秒时间戳()
    当 毫秒时间戳() - 开始 < CAPTCHA超时 {
        变量 href = 当前网址(页面)
        如果 类型(href) != "字符串" {
            睡眠(700)
            跳过
        }

        // 检测是否还在认证页
        变量 在认证页 = 正则验证(href, "auth.openhack.com")

        如果 在认证页 == 假 {
            返回 "完成"
        }

        // 检测 bot 验证错误提示
        变量 bot错误 = 执行脚本(页面, "(function(){var t=document.body.innerText||'';if(/can't verify|not human|captcha|robot/i.test(t))return '1';return '0';})()")
        如果 类型(bot错误) == "字符串" 并且 bot错误 == "1" {
            日志(">>> 检测到 bot 验证，请在浏览器中手动完成")
        }

        // 检测是否有验证码输入框（说明已进入验证码步骤）
        变量 有验证码框 = 执行脚本(页面, "(function(){var ins=document.querySelectorAll('input[type=number], input[type=tel], input[maxlength], input[autocomplete=one-time-code], input[name=code], input[name=otp], input[inputmode=numeric]');for(var i=0;i<ins.length;i++){if(ins[i].offsetParent!==null)return '1';}return '0';})()")
        如果 类型(有验证码框) == "字符串" 并且 有验证码框 == "1" {
            返回 "验证码步骤"
        }

        // 检测是否已到达 app 页面
        如果 正则验证(href, "app.openhack.com") 并且 正则验证(href, "auth.openhack.com") == 假 {
            返回 "完成"
        }

        睡眠(1000)
    }
    返回 "超时"
}

// 填写验证码并提交
函数 填写验证码并提交(页面, 验证码) {
    日志(f"填写验证码: {验证码}")

    // 诊断
    变量 诊断 = 执行脚本(页面, "(function(){var ins=document.querySelectorAll('input');var info=[];for(var i=0;i<ins.length;i++){info.push(i+':type='+ins[i].type+',name='+(ins[i].name||'')+',id='+(ins[i].id||'')+',maxlen='+(ins[i].maxLength||-1)+',ac='+(ins[i].autocomplete||'')+',vis='+(ins[i].offsetParent!==null));}return info.join(' | ');})()")
    日志(f"验证码页inputs: {诊断}")

    // 策略 A：广泛选择器 + 逐字符填入
    变量 jsA = "(function(){var d='" + 验证码 + "';var sel='input[type=number], input[type=tel], input[maxlength], input[autocomplete=one-time-code], input[name=code], input[name=otp], input[name=verificationCode], input[name=pin], input[pattern], input[inputmode=numeric]';var ins=document.querySelectorAll(sel);var vis=[];for(var i=0;i<ins.length;i++){if(ins[i].offsetParent===null||ins[i].type==='hidden')continue;vis.push(ins[i]);}var setter=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value');if(vis.length>=6){for(var i=0;i<6;i++){vis[i].focus();vis[i].select&&vis[i].select();if(setter&&setter.set)setter.set.call(vis[i],d[i]);else vis[i].value=d[i];vis[i].dispatchEvent(new Event('input',{bubbles:true}));vis[i].dispatchEvent(new Event('change',{bubbles:true}));vis[i].dispatchEvent(new KeyboardEvent('keyup',{bubbles:true}));}return 'A_multi:'+vis.length;}else if(vis.length>0){vis[0].focus();vis[0].select&&vis[0].select();if(setter&&setter.set)setter.set.call(vis[0],d);else vis[0].value=d;vis[0].dispatchEvent(new Event('input',{bubbles:true}));vis[0].dispatchEvent(new Event('change',{bubbles:true}));vis[0].dispatchEvent(new KeyboardEvent('keyup',{bubbles:true}));return 'A_single:'+vis.length;}return 'A_none';})()"
    变量 结果A = 执行脚本(页面, jsA)
    日志(f"策略A: {结果A}")

    // 策略 B：所有可见 input
    如果 类型(结果A) == "字符串" 并且 包含(结果A, "none") {
        变量 jsB = "(function(){var d='" + 验证码 + "';var ins=document.querySelectorAll('input');var vis=[];for(var i=0;i<ins.length;i++){if(ins[i].type==='hidden'||ins[i].type==='submit'||ins[i].type==='button'||ins[i].type==='checkbox'||ins[i].type==='password'||ins[i].type==='email')continue;vis.push(ins[i]);}var setter=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value');if(vis.length>=6){for(var i=0;i<6;i++){vis[i].focus();if(setter&&setter.set)setter.set.call(vis[i],d[i]);else vis[i].value=d[i];vis[i].dispatchEvent(new Event('input',{bubbles:true}));vis[i].dispatchEvent(new Event('change',{bubbles:true}));}return 'B_multi:'+vis.length;}else if(vis.length>0){vis[0].focus();if(setter&&setter.set)setter.set.call(vis[0],d);else vis[0].value=d;vis[0].dispatchEvent(new Event('input',{bubbles:true}));vis[0].dispatchEvent(new Event('change',{bubbles:true}));return 'B_single:'+vis.length;}return 'B_none';})()"
        变量 结果B = 执行脚本(页面, jsB)
        日志(f"策略B: {结果B}")
    }

    睡眠(500)

    // 点击提交按钮
    变量 jsClick = "(function(){var names=['Continue','Verify','Submit','Next','Confirm','验证','提交','继续','下一步'];var els=document.querySelectorAll('button, a, [role=button], input[type=submit], input[type=button]');for(var i=0;i<els.length;i++){var t=(els[i].textContent||els[i].value||'').trim();for(var j=0;j<names.length;j++){if(t.indexOf(names[j])>=0&&!els[i].disabled){els[i].click();return 'clicked:'+t;}}}return 'no_btn';})()"
    变量 点击结果 = 执行脚本(页面, jsClick)
    日志(f"点击: {点击结果}")

    // 如果执行脚本报错（页面导航中），说明提交成功
    如果 类型(点击结果) == "错误" {
        变量 错误信息 = 字符串(点击结果)
        如果 包含(错误信息, "导航中") 或者 包含(错误信息, "context canceled") {
            日志("页面导航中，验证码可能已成功")
            返回 "成功"
        }
    }

    // 等待结果
    变量 开始 = 毫秒时间戳()
    当 毫秒时间戳() - 开始 < 30000 {
        变量 href = 当前网址(页面)
        如果 类型(href) == "字符串" {
            如果 正则验证(href, "app.openhack.com") 并且 正则验证(href, "auth.openhack.com") == 假 {
                返回 "成功"
            }
        }
        // 检测验证码输入框是否消失
        变量 还有输入框 = 执行脚本(页面, "(function(){var sel='input[type=number], input[type=tel], input[maxlength], input[autocomplete=one-time-code], input[name=code], input[name=otp], input[inputmode=numeric]';var ins=document.querySelectorAll(sel);for(var i=0;i<ins.length;i++){if(ins[i].offsetParent!==null)return '1';}return '0';})()")
        如果 类型(还有输入框) == "字符串" 并且 还有输入框 == "0" {
            返回 "成功"
        }
        // 检测错误提示
        变量 有错误 = 执行脚本(页面, "(function(){var t=(document.body&&document.body.innerText||'').replace(/\\s+/g,'');if(/invalid|incorrect|expired|wrong|错误|无效|过期/i.test(t))return '1';return '0';})()")
        如果 类型(有错误) == "字符串" 并且 有错误 == "1" {
            返回 "无效"
        }
        睡眠(500)
    }
    返回 "超时"
}

// 通过浏览器 fetch 获取 API Key
函数 提取API密钥(页面) {
    变量 开始 = 毫秒时间戳()

    // 尝试多种可能的 API 端点
    变量 js = "(async()=>{try{var r,s,ak='';var urls=['/api/keys','/api/api-keys','/api/settings','/api/user','/api/account'];for(var u of urls){try{r=await fetch('https://app.openhack.com'+u,{headers:{accept:'application/json'},credentials:'include'});if(!r.ok)continue;s=await r.text();if(s.indexOf('<!DOCTYPE')>=0)continue;var j=JSON.parse(s);if(j.apiKey||j.api_key||j.key){ak=j.apiKey||j.api_key||j.key;break;}if(j.keys&&Array.isArray(j.keys)&&j.keys.length>0){ak=j.keys[0].key||j.keys[0].apiKey||j.keys[0].value||'';break;}if(j.data&&j.data.apiKey){ak=j.data.apiKey;break;}}catch(e){}}if(ak)return JSON.stringify({apiKey:ak,source:'api'});var t=document.body.innerText||'';var m=t.match(/[A-Za-z0-9_-]{32,}/);if(m)return JSON.stringify({apiKey:m[0],source:'page'});return JSON.stringify({error:'no_key_found'});}catch(e){return JSON.stringify({error:e.message});}})()"

    当 毫秒时间戳() - 开始 < 60000 {
        变量 结果json = 执行脚本(页面, js)
        如果 类型(结果json) == "字符串" {
            变量 结果 = json解析(结果json)
            如果 类型(结果) == "哈希" 并且 类型(结果["apiKey"]) == "字符串" {
                日志(f"获取 API Key 成功 (来源: {结果["source"]})")
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

// 检测验证码输入框是否还在
函数 验证码框还在(页面) {
    变量 结果 = 执行脚本(页面, "(function(){var sel='input[type=number], input[type=tel], input[maxlength], input[autocomplete=one-time-code], input[name=code], input[name=otp], input[inputmode=numeric]';var ins=document.querySelectorAll(sel);for(var i=0;i<ins.length;i++){if(ins[i].offsetParent!==null)return '1';}return '0';})()")
    如果 类型(结果) == "字符串" 并且 结果 == "1" {
        返回 真
    }
    返回 假
}

// 点击重发验证码
函数 重发验证码(页面) {
    变量 结果 = 执行脚本(页面, "(function(){var els=document.querySelectorAll('a, button');for(var i=0;i<els.length;i++){var t=(els[i].textContent||'').trim().toLowerCase();if(t.indexOf('resend')>=0||t.indexOf('send again')>=0||t.indexOf('重新发送')>=0||t.indexOf('重发')>=0){els[i].click();return '1';}}return '0';})()")
    如果 类型(结果) == "字符串" 并且 结果 == "1" {
        日志("已点击重发验证码")
        睡眠(800)
        返回 真
    }
    日志("未找到重发验证码入口")
    返回 假
}

// --- 主注册流程 ---
// WorkOS 注册流程:
//   1. 邮箱页 → 输入邮箱 → Continue with email
//   2. 密码页 → 点 "Email sign-in code" 切换到验证码注册模式
//   3. WorkOS 发验证码到邮箱（可能有 bot 检测需手动通过）
//   4. 验证码输入页 → 自动填入验证码 → 提交
//   5. 跳转到 app 主页 → 获取 API Key

函数 注册一个(浏览器) {
    变量 页面 = 打开页面(浏览器)

    // 1. tmaily 纯 API 取号
    日志("正在获取临时邮箱...")
    变量 域名 = 获取域名()
    变量 邮箱 = 创建邮箱(域名)
    如果 邮箱 == "" {
        日志("获取邮箱失败")
        关闭页面(页面)
        返回 空
    }
    日志(f"邮箱: {邮箱}")

    // 2. 打开 OpenHack 注册页
    日志("正在打开 OpenHack 注册页...")
    导航(页面, APP + "/sign-up")

    // 3. 等待邮箱输入框出现
    日志("等待邮箱输入框...")
    变量 邮箱就绪 = 等待可见(页面, "input[name=email]|#email|input[type=email]", 30000)
    如果 邮箱就绪 == 假 {
        日志("邮箱输入框等待超时")
        截图(页面, "debug_email.png")
        关闭页面(页面)
        返回 空
    }

    // 填邮箱
    填写(页面, "input[name=email]|#email|input[type=email]", 邮箱)
    日志("填邮箱完成")
    睡眠(500)

    // 点击 Continue with email
    变量 点Continue结果 = 点击按钮(页面, "Continue with email|Continue|下一步|继续")
    日志(f"点Continue: {点Continue结果}")

    // 4. 等待密码页出现（WorkOS 会跳到密码页）
    日志("等待密码页...")
    变量 密码页就绪 = 等待可见(页面, "input[type=password]|#password|input[name=password]", 30000)
    如果 密码页就绪 == 假 {
        日志("密码页等待超时")
        截图(页面, "debug_password.png")
        关闭页面(页面)
        返回 空
    }
    睡眠(1000)

    // 5. 不填密码！点 "Email sign-in code" 切换到验证码注册模式
    日志("切换到 Email sign-in code 注册模式...")
    变量 点Code结果 = 点击按钮(页面, "Email sign-in code|Email code|Sign-in code")
    日志(f"点Email sign-in code: {点Code结果}")
    睡眠(2000)

    // 6. 等待 bot 验证通过或进入验证码步骤
    日志(">>> 如有 bot 验证请在浏览器中手动完成")
    变量 验证状态 = 等待Bot验证通过(页面)
    日志(f"验证状态: {验证状态}")

    如果 验证状态 == "超时" {
        日志("等待验证超时")
        截图(页面, "debug_timeout.png")
        关闭页面(页面)
        返回 空
    }

    // 7. 如果进入验证码步骤
    如果 验证状态 == "验证码步骤" 或者 验证码框还在(页面) {
        变量 otp成功 = 假
        对于 尝试 = 1; 尝试 <= 3 并且 otp成功 == 假; 尝试 = 尝试 + 1 {
            日志(f"等待验证码邮件... ({尝试}/3)")

            如果 尝试 > 1 {
                重发验证码(页面)
            }

            变量 code = 等待邮件验证码(邮箱, 邮件超时)
            如果 code != "" {
                日志(f"验证码: {code}")
                变量 otp结果 = 填写验证码并提交(页面, code)
                日志(f"验证码结果: {otp结果}")
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

    // 9. 等待跳转到 app 主页
    日志("等待跳转到 app 主页...")
    变量 跳转完成 = 等待跳转(页面, "app.openhack.com", 60000)
    如果 跳转完成 == 假 {
        变量 最终url = 当前网址(页面)
        日志(f"等待跳转超时，当前: {最终url}")
        截图(页面, "debug_stuck.png")
    }

    // 10. 确保在 app 主页
    变量 curUrl = 当前网址(页面)
    如果 类型(curUrl) == "字符串" 并且 正则验证(curUrl, "app.openhack.com") {
        如果 正则验证(curUrl, "auth.openhack.com") {
            导航(页面, APP + "/")
            睡眠(3000)
        }
    } 否则 {
        导航(页面, APP + "/")
        睡眠(3000)
    }

    // 11. 尝试导航到设置页获取 API Key
    日志("正在获取 API Key...")
    变量 apiKey = 提取API密钥(页面)

    如果 apiKey == "" {
        // 尝试导航到 settings 页面再获取
        日志("尝试从设置页获取...")
        导航(页面, APP + "/dashboard/settings")
        睡眠(3000)
        apiKey = 提取API密钥(页面)
    }

    如果 apiKey == "" {
        // 尝试导航到 settings/api-keys 页面
        日志("尝试从 API Keys 页面获取...")
        导航(页面, APP + "/settings")
        睡眠(3000)
        apiKey = 提取API密钥(页面)
    }

    如果 apiKey == "" {
        日志("获取 API Key 失败（可能需要手动从仪表盘获取）")
        // 仍然保存邮箱信息
        变量 行 = f"{邮箱}|NO_KEY"
        追加文件(OUT, 行 + "\n")
        打印(行)
        截图(页面, "debug_no_key.png")
        关闭页面(页面)
        返回 空
    }

    // 12. 保存结果
    变量 行 = f"{邮箱}|{apiKey}"
    追加文件(OUT, 行 + "\n")
    打印(行)
    日志("成功!")

    关闭页面(页面)
}

// --- 入口 ---
日志("=== OpenHack 半自动注册机 (zuzo 版) ===")
变量 数量 = 1
如果 运行参数(2) != "" {
    数量 = 整数(运行参数(2))
}
日志(f"注册数量: {数量}（bot 验证需人工）")

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
