# 跨语言互操作（zuzo ↔ Python）

> 2026-10-25 新增。目标：**任何入口都能双向调用**，不限于 `.hypy` 混合文件。

## 一、三个入口的能力

| 入口 | zuzo 调 Python | Python 调 zuzo |
|---|---|---|
| `.hypy` 混合文件 | ✅ `py调用` 等（第 4 轮起） | ✅ `zuzo.调用` 等（第 4 轮起） |
| 纯 `.hy` | ✅ `py求值` / `py执行` / `py调用` / `py获取` / `py设置` / `py存在` | —（同进程无 py 主程序） |
| 纯 `.py` | —（同进程无 zuzo 主程序） | ✅ `import zuzo` → `调用` / `获取` / `设置` / `存在` |

实现：`interop.go`（main 包）把 `mix.Bridge` 的双向通道从「混合文件专用」提升为「入口即装」；
两侧共享同一个 zuzo `Environment`，变量互见。

## 二、对象转换规则（mix/convert.go）

| zuzo | Python |
|---|---|
| 整数 / 浮点 / 字符串 / 布尔 / 空 | int / float / str / bool / None |
| 数组 | list |
| 字典 | dict |
| 函数 / 内置 | 可调用代理（调用时跨回原侧执行） |

容器为深拷贝；跨语言共享可变对象需显式 `py设置` / `zuzo.设置` 写回。

## 三、示例

```zuzo
// 纯 .hy
变量 r = py求值("2 ** 10")        // 1024
打印(py求值("'-'.join(['a','b'])")) // a-b
py执行("import math")
变量 pi = py获取("math").pi
```

```python
# 纯 .py
import zuzo
zuzo.设置("x", 5)
print(zuzo.获取("x"))            # 5
print(zuzo.调用("长度", [1,2,3])) # 3
```

## 四、边界

- 同一进程只有一侧是「主程序」；另一侧的运行时按需惰性创建。
- 异常不跨语言传播（Python 异常转成 zuzo Error 对象，反之转 RuntimeError）。
- 不做引用共享：容器跨语言传递是快照语义。
