# 自带工具链（zig cc）

> 第32轮（2026-11-01）。一句话：**zuzo 自己带一套 C 编译器**，`--emit-exe` 与 `zpm build` 不再要求机器预装 LLVM。

## 一、为什么是 zig cc

| 方案 | 代价 |
|---|---|
| 要求用户装 LLVM/clang | 300MB+ 安装包、版本漂移、Windows 上 PATH 常配不对 |
| 要求用户装 MinGW/gcc | 同理，且与 MSVC 运行库混用容易炸 |
| **zig cc** | 单个发行版目录（`zig.exe` + `lib/`），自带 clang 前端 + lld 链接器 + libc，一条命令就是完整交叉工具链 |

所以 zuzo 的编译链按「**clang 优先、zig cc 兜底**」解析；仓库里放 `tools/zig/` 就是「自带」。

## 二、查找顺序

```
编译器（CC）
  ZUZO_CC（可带参数，如 "C:/zig/zig.exe cc -target x86_64-windows-msvc"）
  → ZUZO_CLANG
  → clang（PATH、Program Files\LLVM、msys64）
  → zig cc（下表的 zig 查找顺序）

zig
  ZUZO_ZIG
  → <zuzo.exe 同级>/tools/zig/zig.exe      ← 仓库自带位置（推荐）
  → <zuzo.exe 同级>/zig/zig.exe
  → PATH 里的 zig
  → %LOCALAPPDATA%\zuzo\toolchain\zig\zig.exe   （zuzo toolchain install 的落点）
```

**完整性校验**：`zig.exe` 同级必须有 `lib/`，否则直接报「zig 不完整（缺同级 lib/）」——只拷 `zig.exe` 是跑不起来的（zig 自己会报 `unable to find zig installation directory`）。

## 三、命令

```bash
zuzo toolchain status          # 当前用谁（CC / zig 版本 / clang / 工具链目录）
zuzo toolchain env             # 打印 CGO_ENABLED / CC，可直接 source 或 set
zuzo toolchain install [目录]  # 把整个 zig 发行版装到用户工具链目录
zuzo toolchain install <URL>   # 从官方 .zip / .tar.xz 直下并解压校验后落地（第33轮）
zuzo toolchain                 # 等价 status

zuzo --emit-exe 主程序.hy -o app.exe    # 走上面解析出的 CC（clang 或 zig cc）
zpm doctor                              # 体检：工具链 + zuzo + 当前包
zpm build 主程序.hy -o app.exe          # 包管理器侧的一键构建
```

实测（本机）：

```text
$ ./zuzo.exe toolchain status
CC        : clang
CC 路径    : C:\Program Files\LLVM\bin\clang.exe
zig       : D:\...\zuzo\tools\zig\zig.exe 0.16.0
clang     : C:\Program Files\LLVM\bin\clang.exe
用户工具链 : C:\Users\wwww\AppData\Local\zuzo\toolchain\zig

$ ZUZO_CC="D:\...\tools\zig\zig.exe" ./zuzo.exe --emit-exe tests_llvm/t01_print.hy -o tmp/tc/b.exe
已生成可执行文件: ...\tmp\tc\b.exe        # 产物输出与解释器逐字节一致
```

## 四、实现要点（踩过的坑）

1. **zig cc 不认 `-x ir`**：`error: language not recognized: 'ir'` —— IR 靠 `.ll` 扩展名识别即可，只有 clang 才加 `-x ir`。
2. **Windows 目标要钉 triple**：`zig cc -target x86_64-windows-msvc`，否则 zig 默认 gnu，与运行期 `zuzo_rt.obj` 的 ABI 不一致（IR 模块头是 msvc）。
3. **路径必须正斜杠**：zig 用 `argv[0]` 推安装目录，反斜杠 Windows 路径会被它解析失败；Go 侧统一 `filepath.ToSlash`。
4. **自带 ≠ 只拷 exe**：`lib/` 必须一起带上（本仓库 `tools/zig/` 已含完整发行版，约 381MB，`.gitignore` 忽略不入库）。
5. **URL 下载要校验 `lib/`**：`toolchain install https://ziglang.org/download/0.16.0/zig-windows-x86_64-0.16.0.zip` 下载后解压 → `findZigRoot` 定位 zig.exe → `zigComplete()` 校验同级 `lib/` → 复制落地；**只下 zig.exe 不完整**会被明确拒绝（第32轮踩过的坑）。安全防护：zip 解压带 zip-slip 检查（拒 `..` / 绝对路径），临时目录 `defer os.RemoveAll`。
6. **cgo 也要它**：`go build` 需要 C 编译器时用
   ```bash
   CGO_ENABLED=1 CC="$(./zuzo.exe toolchain status | awk -F': ' '/^zig/{print $2}' | awk '{print $1}') cc -target x86_64-windows-msvc" go build -o zuzo.exe ./
   ```
   或直接 `eval "$(./zuzo.exe toolchain env | grep '^export')"`。

## 五、验收

```bash
./run_tests_toolchain.sh
# TOOLCHAIN PASS=6 FAIL=0
```

覆盖：status 能报 zig / env 给出 CC / **zig cc 编出的 exe 输出与解释器逐字节一致** / `zpm doctor` / `zpm build` 产物可运行 / `toolchain install` 落地。
