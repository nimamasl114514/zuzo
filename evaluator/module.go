package evaluator

// module.go — 模块导入系统
//
// 支持两种导入方式：
//
//	导入 "文件.hy"          — 直接导入：在当前环境中执行文件，所有定义变为可用
//	导入 "文件.hy" 为 模块名  — 命名导入：在新环境中执行，结果包装为 Module 对象
//	导入 "mylib.z"          — .z 包导入：自动解包并执行入口文件
//	导入 "mylib"            — 自动搜索: 先找 .hy, 再找 .z 包
//
// 模块搜索路径（按优先级）：
//  1. 相对路径（基于当前文件所在目录）
//  2. zpm_modules/ 目录（zpm 包管理器安装的依赖）
//  3. ~/​.zuzo/stdlib/ 目录（全局标准库）
//  4. ~/.zuzo/.cache/zpackages/ 目录（.z 包解包缓存）
//
// 特性：
//   - 相对路径解析（基于导入文件所在目录）
//   - zpm 包自动发现（搜索 zpm_modules/）
//   - .z 包直接导入（自动解包到缓存目录）
//   - 循环导入检测
//   - 模块缓存（同一文件只执行一次）
//   - .hy 扩展名自动补全

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"zuzo/ast"
	"zuzo/diagnostic"
	zuzoEncoding "zuzo/encoding"
	"zuzo/infer"
	"zuzo/lexer"
	"zuzo/object"
	"zuzo/parser"
	"zuzo/zpm"
)

// ModuleManager 模块管理器：处理文件导入、路径解析、循环检测
type ModuleManager struct {
	baseDir     string                   // 基准目录（用于解析相对路径）
	projectRoot string                   // 项目根目录（包含 zpm.toml 的目录，用于搜索 zpm_modules/）
	loading     map[string]bool          // 正在加载的文件（循环检测）
	cache       map[string]object.Object // 已加载的模块缓存
}

// NewModuleManager 创建模块管理器
func NewModuleManager(baseDir string) *ModuleManager {
	return &ModuleManager{
		baseDir:     baseDir,
		projectRoot: findProjectRoot(baseDir),
		loading:     make(map[string]bool),
		cache:       make(map[string]object.Object),
	}
}

// moduleManager 全局模块管理器实例
var moduleManager *ModuleManager

// SetModuleManager 设置全局模块管理器
func SetModuleManager(m *ModuleManager) {
	moduleManager = m
}

// findProjectRoot 从指定目录向上搜索包含 zpm.toml 的目录
// 找不到则返回原目录
func findProjectRoot(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "zpm.toml")); err == nil {
			return abs
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			break // 到达根目录
		}
		abs = parent
	}
	return dir
}

// ResolvePath 解析模块路径，返回绝对路径（导出版本，用于测试）
func (m *ModuleManager) ResolvePath(path string) string {
	return m.resolvePath(path)
}

// resolvePath 解析模块路径，返回绝对路径
// 搜索顺序: 相对路径 → zpm_modules/ → ~/.zuzo/stdlib/
// 支持的扩展名: .hy（源文件）, .z（打包文件）
func (m *ModuleManager) resolvePath(path string) string {
	// 如果路径已经带 .z 扩展名，直接按原路径查找
	if strings.HasSuffix(path, zpm.PackageExtension) {
		return m.resolvePathExact(path)
	}

	// 自动补全 .hy 扩展名
	hyPath := path
	if !strings.HasSuffix(path, ".hy") {
		hyPath = path + ".hy"
	}

	// 先查找 .hy 文件
	resolved := m.resolvePathExact(hyPath)
	if _, err := os.Stat(resolved); err == nil {
		return resolved
	}

	// .hy 不存在时，尝试查找同名 .z 包
	zPath := path
	if strings.HasSuffix(path, ".hy") {
		zPath = strings.TrimSuffix(path, ".hy")
	}
	if !strings.HasSuffix(zPath, zpm.PackageExtension) {
		zPath += zpm.PackageExtension
	}
	resolvedZ := m.resolvePathExact(zPath)
	if _, err := os.Stat(resolvedZ); err == nil {
		return resolvedZ
	}

	// 回退到 .hy 路径（即使文件不存在，也返回以便产生有意义的错误）
	return resolved
}

// resolvePathExact 精确路径解析（不自动补全扩展名）
func (m *ModuleManager) resolvePathExact(path string) string {
	// 如果是绝对路径，直接使用
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}

	// 1. 相对路径：基于基准目录解析
	candidate := filepath.Clean(filepath.Join(m.baseDir, path))
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}

	// 2. 搜索 zpm_modules/ 目录（zpm 包管理器安装的依赖）
	if m.projectRoot != "" {
		pkgCandidate := filepath.Clean(filepath.Join(m.projectRoot, "zpm_modules", path))
		if _, err := os.Stat(pkgCandidate); err == nil {
			return pkgCandidate
		}
	}

	// 3. 搜索全局标准库 ~/.zuzo/stdlib/
	if home, err := os.UserHomeDir(); err == nil {
		stdCandidate := filepath.Clean(filepath.Join(home, ".zuzo", "stdlib", path))
		if _, err := os.Stat(stdCandidate); err == nil {
			return stdCandidate
		}
	}

	return candidate
}

// LoadModule 加载并执行一个模块文件
// 返回模块的环境（包含所有定义的变量和函数）
// 支持 .hy 源文件和 .z 打包文件
func (m *ModuleManager) LoadModule(path string) (object.Object, *object.Error) {
	absPath := m.resolvePath(path)

	// 检查缓存
	if mod, ok := m.cache[absPath]; ok {
		return mod, nil
	}

	// 循环导入检测
	if m.loading[absPath] {
		return NULL, &object.Error{
			Message: fmt.Sprintf("循环导入: %s", path),
			Code:    diagnostic.ErrCircularImport.Code,
		}
	}

	// 判断是否为 .z 包
	if strings.HasSuffix(absPath, zpm.PackageExtension) {
		return m.loadZPackage(absPath, path)
	}

	// 普通的 .hy 文件加载
	return m.loadHyFile(absPath, path)
}

// loadZPackage 加载 .z 包：解包到缓存目录，然后执行入口文件
func (m *ModuleManager) loadZPackage(absPath string, importPath string) (object.Object, *object.Error) {
	// 解包 .z 包到缓存目录
	codeDir, manifest, errObj := m.unpackZPackage(absPath)
	if errObj != nil {
		return NULL, errObj
	}

	// 确定入口文件
	entryFile := manifest.Entry
	if entryFile == "" {
		entryFile = "main.hy"
	}
	entryPath := filepath.Join(codeDir, entryFile)

	// 检查入口文件是否存在
	if _, err := os.Stat(entryPath); err != nil {
		// 尝试在解包目录中搜索任意 .hy 文件
		entries, _ := os.ReadDir(codeDir)
		found := false
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".hy") {
				entryPath = filepath.Join(codeDir, e.Name())
				entryFile = e.Name()
				found = true
				break
			}
		}
		if !found {
			return NULL, &object.Error{
				Message: fmt.Sprintf(".z 包 %s 中未找到入口文件 %s", importPath, entryFile),
				Code:    diagnostic.ErrFileRead.Code,
			}
		}
	}

	// 标记为正在加载
	m.loading[absPath] = true
	defer delete(m.loading, absPath)

	// 读取入口文件
	source, err := os.ReadFile(entryPath)
	if err != nil {
		return NULL, &object.Error{
			Message: fmt.Sprintf("无法读取 .z 包入口文件 %s: %v", entryFile, err),
			Code:    diagnostic.ErrFileRead.Code,
		}
	}

	// 编码预处理
	normalized := zuzoEncoding.NormalizeSource(source)

	// 保存旧的全局状态
	oldBaseDir := m.baseDir
	// 切换基准目录到解包后的 code/ 目录（使包内嵌套导入能找到同包的其他文件）
	m.baseDir = codeDir

	// 解析
	l := lexer.New(normalized)
	p := parser.New(l)
	program := p.ParseProgram()

	if p.Diagnostics().HasErrors() {
		m.baseDir = oldBaseDir
		return NULL, &object.Error{
			Message: fmt.Sprintf(".z 包 %s 入口文件 %s 有语法错误", importPath, entryFile),
			Code:    diagnostic.ErrImportSyntax.Code,
		}
	}

	// 类型推断
	inf := infer.New()
	inf.Infer(program)

	// SAP 编译（如果全局 SAP 编译器已启用）
	if sapCompiler != nil {
		sap := NewSAPCompiler(inf)
		sap.CompileProgram(program)
		oldSAP := sapCompiler
		sapCompiler = sap
		defer func() { sapCompiler = oldSAP }()
	}

	// 创建模块环境
	modEnv := NewEnvironment()

	// 在模块环境中执行
	result := Eval(program, modEnv)

	// 恢复基准目录
	m.baseDir = oldBaseDir

	// 检查执行结果
	if result != nil && isError(result) {
		return NULL, result.(*object.Error)
	}

	// 创建模块对象（使用包名而非文件名）
	modName := manifest.Name
	if modName == "" {
		modName = strings.TrimSuffix(filepath.Base(absPath), zpm.PackageExtension)
	}
	mod := &object.Module{
		Name: modName,
		Env:  modEnv,
	}

	// 缓存模块
	m.cache[absPath] = mod

	return mod, nil
}

// unpackZPackage 解包 .z 文件到缓存目录
// 返回解包后的 code/ 目录路径和清单
func (m *ModuleManager) unpackZPackage(zFilePath string) (string, *zpm.Manifest, *object.Error) {
	// 计算缓存 key: 基于文件路径 + 修改时间 + 文件大小的哈希
	info, err := os.Stat(zFilePath)
	if err != nil {
		return "", nil, &object.Error{
			Message: fmt.Sprintf("无法访问 .z 包 %s: %v", zFilePath, err),
			Code:    diagnostic.ErrFileRead.Code,
		}
	}

	// 生成缓存 key
	hashInput := fmt.Sprintf("%s|%d|%d", zFilePath, info.ModTime().UnixNano(), info.Size())
	h := sha256.Sum256([]byte(hashInput))
	cacheKey := hex.EncodeToString(h[:16]) // 取前 16 字节作为 key

	// 缓存目录: ~/.zuzo/.cache/zpackages/<hash>/
	home, err := os.UserHomeDir()
	if err != nil {
		return "", nil, &object.Error{
			Message: fmt.Sprintf("无法获取用户目录: %v", err),
			Code:    diagnostic.ErrFileRead.Code,
		}
	}
	cacheDir := filepath.Join(home, ".zuzo", ".cache", "zpackages", cacheKey)
	codeDir := filepath.Join(cacheDir, "code")

	// 如果缓存已存在且包含 code/ 目录，直接复用
	if _, err := os.Stat(codeDir); err == nil {
		// 读取缓存的清单
		manifestPath := filepath.Join(cacheDir, "META-INF", "MANIFEST.toml")
		manifestData, err := os.ReadFile(manifestPath)
		if err == nil {
			manifest, err := zpm.ParseManifest(string(manifestData))
			if err == nil {
				return codeDir, manifest, nil
			}
		}
	}

	// 解包 .z 文件到缓存目录
	pkg, err := zpm.Unpack(zFilePath, cacheDir)
	if err != nil {
		return "", nil, &object.Error{
			Message: fmt.Sprintf("解包 .z 包 %s 失败: %v", zFilePath, err),
			Code:    diagnostic.ErrFileRead.Code,
		}
	}

	if pkg.Manifest == nil {
		return "", nil, &object.Error{
			Message: fmt.Sprintf(".z 包 %s 缺少清单存根", zFilePath),
			Code:    diagnostic.ErrFileRead.Code,
		}
	}

	return codeDir, pkg.Manifest, nil
}

// loadHyFile 加载普通的 .hy 源文件
func (m *ModuleManager) loadHyFile(absPath string, importPath string) (object.Object, *object.Error) {
	// 读取文件
	source, err := os.ReadFile(absPath)
	if err != nil {
		return NULL, &object.Error{
			Message: fmt.Sprintf("无法读取导入文件 %s: %v", importPath, err),
			Code:    diagnostic.ErrFileRead.Code,
		}
	}

	// 编码预处理：自动检测编码并转换为 UTF-8（支持 GBK/Big5/UTF-16 等）
	normalized := zuzoEncoding.NormalizeSource(source)

	// 标记为正在加载
	m.loading[absPath] = true
	defer delete(m.loading, absPath)

	// 保存旧的全局状态
	oldBaseDir := m.baseDir
	// 切换基准目录到被导入文件所在目录（支持嵌套导入）
	m.baseDir = filepath.Dir(absPath)

	// 解析
	l := lexer.New(normalized)
	p := parser.New(l)
	program := p.ParseProgram()

	if p.Diagnostics().HasErrors() {
		m.baseDir = oldBaseDir
		return NULL, &object.Error{
			Message: fmt.Sprintf("导入文件 %s 有语法错误", importPath),
			Code:    diagnostic.ErrImportSyntax.Code,
		}
	}

	// 类型推断
	inf := infer.New()
	inf.Infer(program)

	// SAP 编译（如果全局 SAP 编译器已启用）
	if sapCompiler != nil {
		sap := NewSAPCompiler(inf)
		sap.CompileProgram(program)
		// 注意：导入的模块使用自己的 SAP 编译器，不覆盖全局的
		oldSAP := sapCompiler
		sapCompiler = sap
		defer func() { sapCompiler = oldSAP }()
	}

	// 创建模块环境（带内置函数，使模块文件能使用 打印/读文件 等）
	modEnv := NewEnvironment()

	// 在模块环境中执行
	result := Eval(program, modEnv)

	// 恢复基准目录
	m.baseDir = oldBaseDir

	// 检查执行结果
	if result != nil && isError(result) {
		return NULL, result.(*object.Error)
	}

	// 创建模块对象
	mod := &object.Module{
		Name: filepath.Base(absPath),
		Env:  modEnv,
	}

	// 缓存模块
	m.cache[absPath] = mod

	return mod, nil
}

// evalImportStatement 处理导入语句
func evalImportStatement(node *ast.ImportStatement, env *object.Environment) object.Object {
	if moduleManager == nil {
		return newErrorWithNode(node, diagnostic.ErrModuleNotInit.Code, "模块系统未初始化，无法导入文件")
	}

	mod, errObj := moduleManager.LoadModule(node.Path)
	if errObj != nil {
		// 为 LoadModule 返回的错误补充位置信息
		errObj.Line = getNodeLine(node)
		errObj.Column = getNodeColumn(node)
		if errObj.Code == "" {
			errObj.Code = diagnostic.ErrFileRead.Code
		}
		return errObj
	}

	module, ok := mod.(*object.Module)
	if !ok {
		return newErrorWithNode(node, diagnostic.ErrImportNotModule.Code, "导入结果不是模块对象")
	}

	if node.Alias == "" {
		// 直接导入：将模块环境中的所有变量合并到当前环境
		for name, val := range module.Env.GetAll() {
			// 跳过内置函数（避免覆盖当前环境的内置函数）
			if _, isBuiltin := val.(*object.Builtin); isBuiltin {
				continue
			}
			env.Set(name, val)
		}
		return NULL
	}

	// 命名导入：将模块对象绑定到别名
	env.Set(node.Alias, module)
	return NULL
}
