// teamorouter_register.hy 纯逻辑测试
// 用法: zuzo test_teamorouter.hy
导入 "teamorouter_register.hy"

// 1. 主题提取验证码
变量 a = 从主题提取验证码("Your verification code is 483920")
如果 a == "483920" { 打印("✓ 主题提取验证码") } 否则 { 打印("✗ 主题提取验证码: " + a) }

// 2. 正文提取验证码（含 HTML 标签）
变量 b = 从正文提取验证码("<div>您的验证码是 483920</div>")
如果 b == "483920" { 打印("✓ 正文提取验证码") } 否则 { 打印("✗ 正文提取验证码: " + b) }

// 3. 占位符排除（123456）
变量 c = 从正文提取验证码("Votre code est 123456")
如果 c == "" { 打印("✓ 占位符排除") } 否则 { 打印("✗ 占位符排除: " + c) }

// 4. sk- key 提取
变量 d = 提取文本skkey("hello sk-teamo-abc1234567890abcdefghijklmnopqrstuvwxyz world")
如果 d == "sk-teamo-abc1234567890abcdefghijklmnopqrstuvwxyz" { 打印("✓ sk key 提取") } 否则 { 打印("✗ sk key 提取: " + d) }

// 5. 随机字符串
变量 e = 随机字符串(12)
如果 长度(e) == 12 { 打印("✓ 随机字符串 (" + e + ")") } 否则 { 打印("✗ 随机字符串: " + e) }

// 6. mail.tm 真实 API 测试（创建临时邮箱 + 登录 + 列邮件）
打印("--- mail.tm 真实 API 测试 ---")
变量 账号 = mail创建账号()
如果 账号["address"] == "" {
    打印("✗ mail创建账号 失败")
} 否则 {
    打印("✓ 邮箱创建成功: " + 账号["address"])
    变量 邮件 = mail列邮件(账号["token"])
    如果 类型(邮件) == "数组" {
        打印("✓ 列邮件正常（" + 字符串(长度(邮件)) + " 封）")
    } 否则 {
        打印("✗ 列邮件失败: " + 字符串(邮件))
    }
}

打印("=== 测试完成 ===")