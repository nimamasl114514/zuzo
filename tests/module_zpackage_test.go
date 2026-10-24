package tests

// module_zpackage_test.go — .z 包直接导入功能测试（从 evaluator/ 迁移）
//
// 测试覆盖:
//   1. 打包 .z 包后直接导入（显式 .z 扩展名）
//   2. 省略扩展名自动搜索 .z 包
//   3. 命名导入（导入 ... 为 ...）
//   4. .z 包内嵌套导入（包内文件互相导入）
//   5. 从 zpm_modules/ 目录导入 .z 包
//   6. 入口文件自定义（MANIFEST.toml 中指定 入口）
//   7. 缓存机制验证（同一 .z 包多次导入只解包一次）
//   8. 错误场景（不存在的 .z 包）
//   9. 多文件 .z 包（入口导入子目录模块）
//  10. 路径解析优先级（.hy 优先于 .z）

import (
	"os"
	"path/filepath"
	"testing"

	"zuzo/evaluator"
	"zuzo/object"
	"zuzo/zpm"
)

// ==================== 辅助函数 ====================

// createTestProject 创建一个临时 Zuzo 项目（含 zpm.toml + 源文件）
// 返回项目目录路径
func createTestProject(t *testing.T, name, version, entryCode string, extraFiles map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	// 创建 zpm.toml
	manifest := "[包]\n名称 = \"" + name + "\"\n版本 = \"" + version + "\"\n"
	if entryCode != "" {
		manifest += "入口 = \"main.hy\"\n"
	}
	os.WriteFile(filepath.Join(dir, "zpm.toml"), []byte(manifest), 0644)

	// 创建入口文件
	if entryCode != "" {
		os.WriteFile(filepath.Join(dir, "main.hy"), []byte(entryCode), 0644)
	}

	// 创建额外文件
	for relPath, content := range extraFiles {
		fullPath := filepath.Join(dir, relPath)
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		os.WriteFile(fullPath, []byte(content), 0644)
	}

	return dir
}

// packTestProject 打包测试项目为 .z 文件，返回 .z 文件路径
func packTestProject(t *testing.T, projectDir string) string {
	t.Helper()
	outputPath, err := zpm.Pack(projectDir, zpm.PackOptions{IncludeReadme: false})
	if err != nil {
		t.Fatalf("打包失败: %v", err)
	}
	return outputPath
}

// newTestModuleManager 创建并设置全局 ModuleManager
func newTestModuleManager(baseDir string) *evaluator.ModuleManager {
	mm := evaluator.NewModuleManager(baseDir)
	evaluator.SetModuleManager(mm)
	return mm
}

// copyFileTest 复制文件
func copyFileTest(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("读取源文件失败: %v", err)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		t.Fatalf("写入目标文件失败: %v", err)
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// ==================== 测试 ====================

// TestImportZPackageDirect 测试显式导入 .z 包
func TestImportZPackageDirect(t *testing.T) {
	// 创建一个将被打包的库项目
	libDir := createTestProject(t, "数学库", "1.0.0", `// 数学库入口
变量 PI = 314

函数 平方(x) {
  返回 x * x
}
`, nil)

	// 打包
	zFile := packTestProject(t, libDir)

	// 创建主项目目录，将 .z 包复制过去
	mainDir := t.TempDir()
	copyFileTest(t, zFile, filepath.Join(mainDir, "数学库.z"))

	// 创建并设置模块管理器
	mm := newTestModuleManager(mainDir)

	// 导入 .z 包并验证
	mod, errObj := mm.LoadModule("数学库.z")
	if errObj != nil {
		t.Fatalf("导入 .z 包失败: %v", errObj.Message)
	}

	module, ok := mod.(*object.Module)
	if !ok {
		t.Fatal("返回的不是 Module 对象")
	}

	// 验证模块名
	if module.Name != "数学库" {
		t.Errorf("模块名: 期望 数学库, 得到 %s", module.Name)
	}

	// 验证 PI 变量
	piVal, found := module.Env.Get("PI")
	if !found {
		t.Fatal("未找到导出变量 PI")
	}
	if obj, ok := piVal.(*object.Integer); ok {
		if obj.Value != 314 {
			t.Errorf("PI 值: 期望 314, 得到 %d", obj.Value)
		}
	} else {
		t.Errorf("PI 类型错误: 期望 Integer, 得到 %T", piVal)
	}

	// 验证 平方 函数
	if _, found := module.Env.Get("平方"); !found {
		t.Fatal("未找到导出函数 平方")
	}
}

// TestImportZPackageOmitExtension 测试省略扩展名自动搜索 .z 包
func TestImportZPackageOmitExtension(t *testing.T) {
	libDir := createTestProject(t, "工具", "2.0.0", `变量 版本 = "2.0"
`, nil)

	zFile := packTestProject(t, libDir)

	mainDir := t.TempDir()
	copyFileTest(t, zFile, filepath.Join(mainDir, "工具.z"))

	mm := newTestModuleManager(mainDir)

	// 省略 .z 扩展名导入
	mod, errObj := mm.LoadModule("工具")
	if errObj != nil {
		t.Fatalf("省略扩展名导入失败: %v", errObj.Message)
	}

	module, ok := mod.(*object.Module)
	if !ok {
		t.Fatal("返回的不是 Module 对象")
	}

	// 验证模块名
	if module.Name != "工具" {
		t.Errorf("模块名: 期望 工具, 得到 %s", module.Name)
	}

	// 验证 版本 变量
	v, found := module.Env.Get("版本")
	if !found {
		t.Fatal("未找到导出变量 版本")
	}
	if obj, ok := v.(*object.String); ok {
		if obj.Value != "2.0" {
			t.Errorf("版本值: 期望 2.0, 得到 %s", obj.Value)
		}
	} else {
		t.Errorf("版本类型错误: 期望 String, 得到 %T", v)
	}
}

// TestImportZPackageNamed 测试命名导入
func TestImportZPackageNamed(t *testing.T) {
	libDir := createTestProject(t, "字符串库", "1.5.0", `变量 前缀 = ">>"
函数 拼接(a, b) {
  返回 a + b
}
`, nil)

	zFile := packTestProject(t, libDir)

	mainDir := t.TempDir()
	copyFileTest(t, zFile, filepath.Join(mainDir, "字符串库.z"))

	mm := newTestModuleManager(mainDir)
	mod, errObj := mm.LoadModule("字符串库.z")
	if errObj != nil {
		t.Fatalf("命名导入失败: %v", errObj.Message)
	}

	module, ok := mod.(*object.Module)
	if !ok {
		t.Fatal("返回的不是 Module 对象")
	}

	// 验证模块名（应使用清单中的包名）
	if module.Name != "字符串库" {
		t.Errorf("模块名: 期望 字符串库, 得到 %s", module.Name)
	}

	// 验证 前缀 变量
	if _, found := module.Env.Get("前缀"); !found {
		t.Fatal("未找到导出变量 前缀")
	}
}

// TestImportZPackageNestedImports 测试 .z 包内嵌套导入
// 包内 main.hy 导入同包的 utils.hy
func TestImportZPackageNestedImports(t *testing.T) {
	// 创建库项目: main.hy 导入 utils.hy
	libDir := createTestProject(t, "嵌套库", "1.0.0",
		`// main.hy - 导入同包的 utils
导入 "utils" 为 工具

变量 结果 = 工具.加倍(5)
`,
		map[string]string{
			"utils.hy": `// utils.hy
函数 加倍(x) {
  返回 x * 2
}
`,
		})

	zFile := packTestProject(t, libDir)

	mainDir := t.TempDir()
	copyFileTest(t, zFile, filepath.Join(mainDir, "嵌套库.z"))

	mm := newTestModuleManager(mainDir)
	mod, errObj := mm.LoadModule("嵌套库.z")
	if errObj != nil {
		t.Fatalf("嵌套导入失败: %v", errObj.Message)
	}

	module, ok := mod.(*object.Module)
	if !ok {
		t.Fatal("返回的不是 Module 对象")
	}

	// 验证 结果 变量（应为 5 * 2 = 10）
	result, found := module.Env.Get("结果")
	if !found {
		t.Fatal("未找到导出变量 结果")
	}
	if obj, ok := result.(*object.Integer); ok {
		if obj.Value != 10 {
			t.Errorf("结果值: 期望 10, 得到 %d", obj.Value)
		}
	} else {
		t.Errorf("结果类型错误: 期望 Integer, 得到 %T", result)
	}
}

// TestImportZPackageFromModulesDir 测试从 zpm_modules/ 导入
func TestImportZPackageFromModulesDir(t *testing.T) {
	libDir := createTestProject(t, "模块库", "3.0.0", `变量 标志 = 真
`, nil)

	zFile := packTestProject(t, libDir)

	// 创建主项目，含 zpm.toml 和 zpm_modules/
	mainDir := t.TempDir()
	os.WriteFile(filepath.Join(mainDir, "zpm.toml"), []byte(`[包]
名称 = "主项目"
版本 = "1.0.0"
`), 0644)

	// 将 .z 包放到 zpm_modules/ 目录
	modulesDir := filepath.Join(mainDir, "zpm_modules")
	os.MkdirAll(modulesDir, 0755)
	copyFileTest(t, zFile, filepath.Join(modulesDir, "模块库.z"))

	mm := newTestModuleManager(mainDir)
	mod, errObj := mm.LoadModule("模块库.z")
	if errObj != nil {
		t.Fatalf("从 zpm_modules/ 导入失败: %v", errObj.Message)
	}

	module, ok := mod.(*object.Module)
	if !ok {
		t.Fatal("返回的不是 Module 对象")
	}

	// 验证 标志 变量
	if _, found := module.Env.Get("标志"); !found {
		t.Fatal("未找到导出变量 标志")
	}
}

// TestImportZPackageCustomEntry 测试自定义入口文件
func TestImportZPackageCustomEntry(t *testing.T) {
	dir := t.TempDir()

	// 创建 zpm.toml，指定入口为 lib/entry.hy
	os.WriteFile(filepath.Join(dir, "zpm.toml"), []byte(`[包]
名称 = "自定义入口"
版本 = "1.0.0"
入口 = "lib/entry.hy"
`), 0644)

	// 创建 lib/entry.hy
	os.MkdirAll(filepath.Join(dir, "lib"), 0755)
	os.WriteFile(filepath.Join(dir, "lib", "entry.hy"), []byte(`变量 入口值 = 42
`), 0644)

	// 也创建一个 main.hy（不应该被使用）
	os.WriteFile(filepath.Join(dir, "main.hy"), []byte(`变量 错误值 = 999
`), 0644)

	zFile := packTestProject(t, dir)

	mainDir := t.TempDir()
	copyFileTest(t, zFile, filepath.Join(mainDir, "自定义入口.z"))

	mm := newTestModuleManager(mainDir)
	mod, errObj := mm.LoadModule("自定义入口.z")
	if errObj != nil {
		t.Fatalf("自定义入口导入失败: %v", errObj.Message)
	}

	module, ok := mod.(*object.Module)
	if !ok {
		t.Fatal("返回的不是 Module 对象")
	}

	// 验证使用了自定义入口
	val, found := module.Env.Get("入口值")
	if !found {
		t.Fatal("未找到入口值变量，可能未使用自定义入口文件")
	}
	if obj, ok := val.(*object.Integer); ok {
		if obj.Value != 42 {
			t.Errorf("入口值: 期望 42, 得到 %d", obj.Value)
		}
	}

	// 验证未使用 main.hy
	if _, wrongFound := module.Env.Get("错误值"); wrongFound {
		t.Error("不应加载 main.hy 中的变量")
	}
}

// TestImportZPackageCache 测试缓存机制
// 同一 .z 包多次导入应返回缓存的模块
func TestImportZPackageCache(t *testing.T) {
	libDir := createTestProject(t, "缓存库", "1.0.0", `变量 值 = 100
`, nil)

	zFile := packTestProject(t, libDir)

	mainDir := t.TempDir()
	copyFileTest(t, zFile, filepath.Join(mainDir, "缓存库.z"))

	mm := newTestModuleManager(mainDir)

	// 第一次导入
	mod1, errObj := mm.LoadModule("缓存库.z")
	if errObj != nil {
		t.Fatalf("第一次导入失败: %v", errObj.Message)
	}

	// 第二次导入（应从缓存返回）
	mod2, errObj := mm.LoadModule("缓存库.z")
	if errObj != nil {
		t.Fatalf("第二次导入失败: %v", errObj.Message)
	}

	// 应该是同一个对象（缓存命中）
	if mod1 != mod2 {
		t.Error("第二次导入未返回缓存对象")
	}
}

// TestImportZPackageNonExistent 测试导入不存在的 .z 包
func TestImportZPackageNonExistent(t *testing.T) {
	mainDir := t.TempDir()
	mm := newTestModuleManager(mainDir)

	_, errObj := mm.LoadModule("不存在的包.z")
	if errObj == nil {
		t.Fatal("导入不存在的 .z 包应返回错误")
	}

	// 错误信息应包含包名
	if errObj.Message == "" {
		t.Error("错误信息不应为空")
	}
}

// TestImportZPackageRelativePath 测试相对路径导入 .z 包
func TestImportZPackageRelativePath(t *testing.T) {
	libDir := createTestProject(t, "相对库", "1.0.0", `变量 数据 = 777
`, nil)

	zFile := packTestProject(t, libDir)

	mainDir := t.TempDir()

	// 将 .z 包放到 libs/ 子目录
	libsDir := filepath.Join(mainDir, "libs")
	os.MkdirAll(libsDir, 0755)
	copyFileTest(t, zFile, filepath.Join(libsDir, "相对库.z"))

	mm := newTestModuleManager(mainDir)

	// 使用相对路径导入
	mod, errObj := mm.LoadModule("./libs/相对库.z")
	if errObj != nil {
		t.Fatalf("相对路径导入失败: %v", errObj.Message)
	}

	module, ok := mod.(*object.Module)
	if !ok {
		t.Fatal("返回的不是 Module 对象")
	}

	// 验证 数据 变量
	val, found := module.Env.Get("数据")
	if !found {
		t.Fatal("未找到导出变量 数据")
	}
	if obj, ok := val.(*object.Integer); ok {
		if obj.Value != 777 {
			t.Errorf("数据值: 期望 777, 得到 %d", obj.Value)
		}
	}
}

// TestImportZPackageMultipleFiles 测试多文件 .z 包
// 入口 main.hy 导入子目录 math/advanced.hy
func TestImportZPackageMultipleFiles(t *testing.T) {
	libDir := createTestProject(t, "多文件库", "1.0.0",
		`// main.hy
导入 "math/advanced" 为 高级数学

变量 计算结果 = 高级数学.立方(3)
`,
		map[string]string{
			"math/advanced.hy": `// math/advanced.hy
函数 立方(x) {
  返回 x * x * x
}
`,
			"math/basic.hy": `// math/basic.hy
函数 加(x, y) {
  返回 x + y
}
`,
		})

	zFile := packTestProject(t, libDir)

	mainDir := t.TempDir()
	copyFileTest(t, zFile, filepath.Join(mainDir, "多文件库.z"))

	mm := newTestModuleManager(mainDir)
	mod, errObj := mm.LoadModule("多文件库.z")
	if errObj != nil {
		t.Fatalf("多文件包导入失败: %v", errObj.Message)
	}

	module, ok := mod.(*object.Module)
	if !ok {
		t.Fatal("返回的不是 Module 对象")
	}

	// 验证 计算结果（3 的立方 = 27）
	result, found := module.Env.Get("计算结果")
	if !found {
		t.Fatal("未找到导出变量 计算结果")
	}
	if obj, ok := result.(*object.Integer); ok {
		if obj.Value != 27 {
			t.Errorf("计算结果: 期望 27, 得到 %d", obj.Value)
		}
	}
}

// TestResolvePathZPackageHyFallback 测试路径解析优先级
// 当 .hy 和 .z 同时存在时，应优先使用 .hy
func TestResolvePathZPackageHyFallback(t *testing.T) {
	dir := t.TempDir()

	// 同时创建 mylib.hy 和 mylib.z
	os.WriteFile(filepath.Join(dir, "mylib.hy"), []byte(`变量 来源 = "hy"`), 0644)

	// 创建一个 .z 包
	libDir := createTestProject(t, "mylib", "1.0.0", `变量 来源 = "z"`, nil)
	zFile := packTestProject(t, libDir)
	copyFileTest(t, zFile, filepath.Join(dir, "mylib.z"))

	mm := newTestModuleManager(dir)

	// 导入 "mylib" — 应优先找到 .hy
	resolved := mm.ResolvePath("mylib")
	if !hasSuffix(resolved, ".hy") {
		t.Errorf("应优先解析为 .hy 文件, 得到: %s", resolved)
	}
}

// TestResolvePathZPackageFallbackToZ 测试 .hy 不存在时回退到 .z
func TestResolvePathZPackageFallbackToZ(t *testing.T) {
	dir := t.TempDir()

	// 只创建 .z 包，不创建 .hy
	libDir := createTestProject(t, "onlyz", "1.0.0", `变量 来源 = "z"`, nil)
	zFile := packTestProject(t, libDir)
	copyFileTest(t, zFile, filepath.Join(dir, "onlyz.z"))

	mm := newTestModuleManager(dir)

	// 导入 "onlyz" — .hy 不存在，应回退到 .z
	resolved := mm.ResolvePath("onlyz")
	if !hasSuffix(resolved, zpm.PackageExtension) {
		t.Errorf("应回退解析为 .z 文件, 得到: %s", resolved)
	}
}
