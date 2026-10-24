# 跨语言无缝调用示例

## .hy 调 Python

```bash
./zuzo.exe examples/interop/zuzo_calls_py.hy
```

```
py求值 2**10 = 1024
py求值 join = a-b-c
zuzo 加法 = 7
```

可用内置：`py求值(表达式)` / `py执行(代码)` / `py调用(函数名, 参数...)` / `py获取(变量名)` / `py设置(变量名, 值)` / `py存在(变量名)`

## .py 调 zuzo

```bash
./zuzo.exe examples/interop/py_calls_zuzo.py
```

```
zuzo x = 5
zuzo 存在 x: True 存在 y: False
zuzo 长度: 4
zuzo 类型: 字符串
```

可用 API：`zuzo.调用(函数名, 参数...)` / `zuzo.获取(变量名)` / `zuzo.设置(变量名, 值)` / `zuzo.存在(变量名)`

两侧共享同一个 zuzo 环境，变量互见；标量与容器自动深拷贝转换，函数跨语言调用走代理。
