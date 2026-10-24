package main

// zpm — zuzo 包管理器 (pip 风格)
//
// 用法:
//   zpm install <包名>[==版本]     安装包（类似 pip install）
//   zpm install -r 依赖.txt        从依赖文件安装
//   zpm install                    安装 依赖.txt 中所有依赖
//   zpm uninstall <包名>           卸载包
//   zpm list                       列出已安装的包
//   zpm freeze                     输出已安装包及精确版本
//   zpm search <关键字>            搜索注册表
//   zpm show <包名>                显示包详情
//   zpm init                       初始化新项目
//   zpm --help                     显示帮助

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"zuzo/zpm"
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(0)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "install", "i":
		cmdInstall(args)
	case "uninstall", "remove", "rm":
		cmdUninstall(args)
	case "pack", "package":
		cmdPack(args)
	case "unpack":
		cmdUnpack(args)
	case "inspect", "info":
		cmdInspect(args)
	case "list", "ls":
		cmdList(args)
	case "freeze":
		cmdFreeze(args)
	case "search":
		cmdSearch(args)
	case "show":
		cmdShow(args)
	case "init":
		cmdInit(args)
	case "--help", "-h", "help":
		printHelp()
	case "--version", "-v":
		fmt.Println("zpm 1.0.0 (pip 风格 + .z 包)")
	default:
		fmt.Fprintf(os.Stderr, "错误: 未知命令 %q\n\n", cmd)
		printHelp()
		os.Exit(1)
	}
}

// ==================== install ====================

func cmdInstall(args []string) {
	dir, _ := os.Getwd()

	// zpm install -r 依赖.txt
	if len(args) >= 2 && (args[0] == "-r" || args[0] == "--requirement") {
		reqPath := args[1]
		data, err := os.ReadFile(reqPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误: 无法读取依赖文件 %s: %v\n", reqPath, err)
			os.Exit(1)
		}
		reqs, err := zpm.ParseRequirements(string(data))
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			os.Exit(1)
		}
		installRequirements(dir, reqs)
		return
	}

	// zpm install <包名> [包名...]
	if len(args) >= 1 && !strings.HasPrefix(args[0], "-") {
		reqs := &zpm.Requirements{}
		for _, arg := range args {
			req, err := parseInstallArg(arg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "错误: %v\n", err)
				os.Exit(1)
			}
			reqs.Items = append(reqs.Items, req)
		}
		installRequirements(dir, reqs)

		// 写入依赖.txt
		appendToRequirementsFile(dir, reqs)
		return
	}

	// zpm install (无参数) → 安装 依赖.txt
	reqs, err := zpm.LoadRequirements(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "错误: 未找到 依赖.txt 文件")
			fmt.Fprintln(os.Stderr, "提示: 使用 zpm install <包名> 安装包，或创建 依赖.txt")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
	installRequirements(dir, reqs)
}

func installRequirements(dir string, reqs *zpm.Requirements) {
	if len(reqs.Items) == 0 {
		fmt.Println("没有依赖需要安装")
		return
	}

	installer, err := zpm.NewInstaller(dir, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("正在安装 %d 个依赖...\n\n", len(reqs.Items))

	results, err := installer.InstallAll(reqs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n✗ %v\n", err)
		os.Exit(1)
	}

	installed, skipped := 0, 0
	for _, r := range results {
		if r.Skipped {
			skipped++
		} else {
			installed++
		}
	}

	fmt.Printf("\n✓ 安装完成: %d 个新安装, %d 个已满足\n", installed, skipped)
}

// parseInstallArg 解析 install 命令的参数
// 支持格式: 包名, 包名==1.0.0, 包名>=1.0.0, git+url, -e ./path
func parseInstallArg(arg string) (*zpm.Requirement, error) {
	// 委托给 zpm 的行解析器
	return zpm.ParseRequirementLine(arg)
}

// appendToRequirementsFile 将新安装的包追加到 依赖.txt
func appendToRequirementsFile(dir string, reqs *zpm.Requirements) {
	// 尝试加载现有依赖文件
	existing, _ := zpm.LoadRequirements(dir)
	if existing == nil {
		existing = &zpm.Requirements{}
	}

	// 合并
	for _, req := range reqs.Items {
		existing.Add(req)
	}

	if err := zpm.SaveRequirements(dir, existing); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 无法更新 依赖.txt: %v\n", err)
	}
}

// ==================== uninstall ====================

func cmdUninstall(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "用法: zpm uninstall <包名> [包名...]")
		os.Exit(1)
	}

	dir, _ := os.Getwd()
	installer, err := zpm.NewInstaller(dir, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}

	for _, name := range args {
		fmt.Printf("正在卸载 %s...\n", name)
		if err := installer.Uninstall(name); err != nil {
			fmt.Fprintf(os.Stderr, "✗ %v\n", err)
			continue
		}
		fmt.Printf("  ✓ 已卸载 %s\n", name)

		// 从依赖文件移除
		if reqs, _ := zpm.LoadRequirements(dir); reqs != nil {
			reqs.Remove(name)
			zpm.SaveRequirements(dir, reqs)
		}
	}

	// 保存锁文件
	installer.SaveLockfile(dir)
}

// ==================== list ====================

func cmdList(args []string) {
	dir, _ := os.Getwd()
	installer, err := zpm.NewInstaller(dir, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}

	installed := installer.ListInstalled()
	if len(installed) == 0 {
		fmt.Println("未安装任何包")
		fmt.Println("提示: 使用 zpm install <包名> 安装包")
		return
	}

	// 按名称排序
	names := make([]string, 0, len(installed))
	for name := range installed {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Println("已安装的包:")
	for _, name := range names {
		version := installed[name]
		if version != "" {
			fmt.Printf("  %s (%s)\n", name, version)
		} else {
			fmt.Printf("  %s\n", name)
		}
	}
	fmt.Printf("\n共 %d 个包\n", len(names))
}

// ==================== freeze ====================

func cmdFreeze(args []string) {
	dir, _ := os.Getwd()
	installer, err := zpm.NewInstaller(dir, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}

	installed := installer.ListInstalled()

	// 按名称排序
	names := make([]string, 0, len(installed))
	for name := range installed {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		version := installed[name]
		if version != "" {
			fmt.Printf("%s==%s\n", name, version)
		} else {
			fmt.Printf("%s\n", name)
		}
	}
}

// ==================== search ====================

func cmdSearch(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "用法: zpm search <关键字>")
		os.Exit(1)
	}

	keyword := args[0]
	client := zpm.NewClient()

	// 尝试在线搜索
	if client.IsOnline() {
		results, err := client.Search(keyword)
		if err == nil && len(results) > 0 {
			fmt.Printf("搜索结果 (在线注册表):\n\n")
			for _, r := range results {
				fmt.Printf("  %s (%s)", r.Name, r.Version)
				if r.Description != "" {
					fmt.Printf(" — %s", r.Description)
				}
				fmt.Println()
			}
			return
		}
	}

	// 回退到本地注册表
	registry, err := zpm.LoadRegistry()
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}

	results := registry.Search(keyword)
	if len(results) == 0 {
		fmt.Printf("未找到匹配 %q 的包\n", keyword)
		return
	}

	fmt.Printf("搜索结果 (本地注册表):\n\n")
	for _, entry := range results {
		fmt.Printf("  %s (%s)", entry.Name, entry.Latest)
		if entry.Description != "" {
			fmt.Printf(" — %s", entry.Description)
		}
		fmt.Println()
	}
}

// ==================== show ====================

func cmdShow(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "用法: zpm show <包名 | owner/repo>")
		os.Exit(1)
	}

	name := args[0]

	// 尝试解析为 GitHub 仓库
	if repo, ok := zpm.ParseGitHubRepo(name); ok {
		ghClient := zpm.NewGitHubClient()
		info, err := ghClient.GetRepoInfo(repo.Owner, repo.Name)
		if err == nil {
			fmt.Printf("名称: %s\n", info.FullName)
			fmt.Printf("类型: GitHub 仓库\n")
			if info.Description != "" {
				fmt.Printf("描述: %s\n", info.Description)
			}
			fmt.Printf("默认分支: %s\n", info.DefaultBranch)
			fmt.Printf("星标: %d\n", info.Stars)
			fmt.Printf("Fork: %d\n", info.Forks)
			if info.HTMLURL != "" {
				fmt.Printf("主页: %s\n", info.HTMLURL)
			}

			// 列出 tags
			tags, err := ghClient.ListTags(repo.Owner, repo.Name)
			if err == nil && len(tags) > 0 {
				if len(tags) > 10 {
					fmt.Printf("可用标签: %s ... (共 %d 个)\n", strings.Join(tags[:10], ", "), len(tags))
				} else {
					fmt.Printf("可用标签: %s\n", strings.Join(tags, ", "))
				}
			}
			return
		}
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}

	// 中央注册表查询
	client := zpm.NewClient()
	if client.IsOnline() {
		info, err := client.GetPackage(name)
		if err == nil {
			fmt.Printf("名称: %s\n", info.Name)
			fmt.Printf("最新版本: %s\n", info.Latest)
			if info.Description != "" {
				fmt.Printf("描述: %s\n", info.Description)
			}
			if info.Author != "" {
				fmt.Printf("作者: %s\n", info.Author)
			}
			if len(info.Versions) > 0 {
				fmt.Printf("可用版本: %s\n", strings.Join(info.Versions, ", "))
			}
			if info.GitURL != "" {
				fmt.Printf("Git 源: %s\n", info.GitURL)
			}
			return
		}
	}

	// 回退到本地注册表
	registry, _ := zpm.LoadRegistry()
	if entry, ok := registry.Lookup(name); ok {
		fmt.Printf("名称: %s\n", entry.Name)
		fmt.Printf("最新版本: %s\n", entry.Latest)
		if entry.Description != "" {
			fmt.Printf("描述: %s\n", entry.Description)
		}
		if len(entry.Versions) > 0 {
			fmt.Printf("可用版本: %s\n", strings.Join(entry.Versions, ", "))
		}
		if entry.Source != "" {
			fmt.Printf("Git 源: %s\n", entry.Source)
		}
		return
	}

	// 检查是否已安装
	dir, _ := os.Getwd()
	modulesDir := filepath.Join(dir, zpm.ModulesDir, name)
	if _, err := os.Stat(modulesDir); err == nil {
		fmt.Printf("名称: %s\n", name)
		fmt.Printf("来源: 本地安装\n")
		return
	}

	fmt.Fprintf(os.Stderr, "错误: 未找到包 %s\n", name)
	os.Exit(1)
}

// ==================== pack ====================

func cmdPack(args []string) {
	dir, _ := os.Getwd()

	// 解析选项
	opts := zpm.PackOptions{
		IncludeReadme: true,
	}
	outputName := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o", "--output":
			if i+1 < len(args) {
				opts.OutputDir = args[i+1]
				i++
			}
		case "-n", "--name":
			if i+1 < len(args) {
				outputName = args[i+1]
				i++
			}
		case "--include-tests":
			opts.IncludeTests = true
		case "--no-readme":
			opts.IncludeReadme = false
		}
	}
	opts.OutputName = outputName

	outputPath, err := zpm.Pack(dir, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ 打包失败: %v\n", err)
		os.Exit(1)
	}

	// 显示打包结果
	manifest, _ := zpm.LoadManifest(dir)
	fmt.Println("✓ 打包完成!")
	fmt.Printf("  文件: %s\n", outputPath)

	if manifest != nil {
		fmt.Printf("  包名: %s\n", manifest.Name)
		fmt.Printf("  版本: %s\n", manifest.Version)
	}

	// 列出包内容
	entries, _ := zpm.ListZContents(outputPath)
	stubCount := 0
	codeCount := 0
	resCount := 0
	for _, e := range entries {
		if strings.HasPrefix(e, "META-INF/") {
			stubCount++
		} else if strings.HasPrefix(e, "code/") {
			codeCount++
		} else if strings.HasPrefix(e, "resources/") {
			resCount++
		}
	}
	fmt.Printf("  存根: %d 个文件\n", stubCount)
	fmt.Printf("  代码: %d 个文件\n", codeCount)
	if resCount > 0 {
		fmt.Printf("  资源: %d 个文件\n", resCount)
	}
}

// ==================== unpack ====================

func cmdUnpack(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "用法: zpm unpack <xxx.z> [目标目录]")
		os.Exit(1)
	}

	zFile := args[0]
	destDir := "."
	if len(args) >= 2 {
		destDir = args[1]
	}

	pkg, err := zpm.Unpack(zFile, destDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ 解包失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ 解包完成!")
	fmt.Printf("  目标: %s/\n", destDir)
	if pkg.Manifest != nil {
		fmt.Printf("  包名: %s\n", pkg.Manifest.Name)
		fmt.Printf("  版本: %s\n", pkg.Manifest.Version)
	}
	fmt.Printf("  代码: %d 个文件\n", len(pkg.Files))
	if len(pkg.Resources) > 0 {
		fmt.Printf("  资源: %d 个文件\n", len(pkg.Resources))
	}
}

// ==================== inspect ====================

func cmdInspect(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "用法: zpm inspect <xxx.z>")
		os.Exit(1)
	}

	zFile := args[0]

	// 验证
	if err := zpm.VerifyZPackage(zFile); err != nil {
		fmt.Fprintf(os.Stderr, "✗ 包验证失败: %v\n", err)
		os.Exit(1)
	}

	// 读取存根
	manifest, err := zpm.ReadZManifest(zFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}

	fmt.Println("════════════════════════════════════")
	fmt.Println("  .z 包信息")
	fmt.Println("════════════════════════════════════")
	fmt.Printf("  名称: %s\n", manifest.Name)
	fmt.Printf("  版本: %s\n", manifest.Version)
	if manifest.Description != "" {
		fmt.Printf("  描述: %s\n", manifest.Description)
	}
	if manifest.Author != "" {
		fmt.Printf("  作者: %s\n", manifest.Author)
	}
	if manifest.Entry != "" {
		fmt.Printf("  入口: %s\n", manifest.Entry)
	}
	if len(manifest.Dependencies) > 0 {
		fmt.Printf("  依赖: %d 个\n", len(manifest.Dependencies))
	}

	// 列出内容
	entries, _ := zpm.ListZContents(zFile)
	fmt.Println("\n包内容:")
	for _, entry := range entries {
		if strings.HasPrefix(entry, "META-INF/") {
			fmt.Printf("  [存根] %s\n", entry)
		} else if strings.HasPrefix(entry, "code/") {
			fmt.Printf("  [代码] %s\n", entry)
		} else if strings.HasPrefix(entry, "resources/") {
			fmt.Printf("  [资源] %s\n", entry)
		} else {
			fmt.Printf("  [其他] %s\n", entry)
		}
	}
}

// ==================== init ====================

func cmdInit(args []string) {
	dir, _ := os.Getwd()

	// 创建 依赖.txt
	reqPath := filepath.Join(dir, zpm.RequirementsFile)
	if _, err := os.Stat(reqPath); err != nil {
		os.WriteFile(reqPath, []byte("# zuzo 依赖文件\n# 格式: 包名 或 包名==版本\n# 示例:\n# 网络请求\n# json工具==1.2.0\n"), 0644)
	}

	// 创建 zpm_modules 目录
	os.MkdirAll(filepath.Join(dir, zpm.ModulesDir), 0755)

	// 创建 .gitignore
	gitignorePath := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gitignorePath); err != nil {
		os.WriteFile(gitignorePath, []byte("zpm_modules/\nzpm.lock\n"), 0644)
	}

	// 创建入口文件
	entryPath := filepath.Join(dir, "main.hy")
	if _, err := os.Stat(entryPath); err != nil {
		name := filepath.Base(dir)
		os.WriteFile(entryPath, []byte(fmt.Sprintf("// %s\n打印(\"Hello, zuzo!\")\n", name)), 0644)
	}

	fmt.Println("✓ 项目已初始化!")
	fmt.Printf("  依赖文件: %s\n", zpm.RequirementsFile)
	fmt.Printf("  依赖目录: %s/\n", zpm.ModulesDir)
	fmt.Printf("  入口文件: main.hy\n")
	fmt.Println("\n下一步:")
	fmt.Println("  zpm install <包名>    安装依赖")
	fmt.Println("  zpm list              查看已安装")
}

// ==================== 帮助 ====================

func printHelp() {
	fmt.Println(`zpm — zuzo 包管理器 (pip 风格 + .z 包)

用法:
  zpm <命令> [参数]

命令:
  install <包名>[==版本]        安装包（可指定多个）
  install xxx.z                 从 .z 包安装
  install -r <文件>             从依赖文件安装
  install                       安装 依赖.txt 中所有依赖
  install owner/repo[@tag]     从 GitHub 仓库安装
  uninstall <包名>              卸载包（可指定多个）
  pack                          打包当前项目为 .z 文件
  unpack <xxx.z> [目录]         解包 .z 文件到目录
  inspect <xxx.z>               查看 .z 包信息
  list                          列出已安装的包
  freeze                        输出已安装包及精确版本
  search <关键字>               搜索注册表
  show <包名/owner/repo>        显示包详情
  init                          初始化新项目

.z 包格式 (类似 Java .jar):
  META-INF/
    MANIFEST.toml   ← 存根: 包名、版本、作者、入口
    EXPORTS.zstub   ← 存根: 导出符号表（函数/变量/模块签名）
    DEPS.toml       ← 存根: 依赖声明
  code/
    *.hy            ← 代码: zuzo 源文件
  resources/         ← 资源文件（可选）

依赖规格格式:
  网络请求                               最新版本（注册表）
  网络请求==1.2.0                        精确版本
  owner/repo                             GitHub 仓库（最新）
  owner/repo@v1.0.0                      GitHub 仓库 + 标签
  xxx.z                                  .z 包文件
  -e ./libs/mylib                        本地路径

文件:
  依赖.txt          依赖声明文件
  zpm.toml          项目清单（zpm pack 使用）
  zpm.lock          依赖锁文件
  zpm_modules/      依赖安装目录

示例:
  zpm pack                            # 打包当前项目 → 名称-版本.z
  zpm pack -o ./dist                  # 指定输出目录
  zpm inspect mylib-1.0.0.z           # 查看 .z 包内容
  zpm install mylib-1.0.0.z           # 从 .z 包安装
  zpm unpack mylib-1.0.0.z ./src      # 解包到指定目录
  zpm install 网络请求                  # 注册表安装
  zpm install zuzo-lang/utils@v1.0.0   # GitHub 仓库
  zpm list                            # 查看已安装
  zpm freeze                          # 导出精确版本`)
}
