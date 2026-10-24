package zpm

// packager.go — .z 包格式（类似 Java .jar）
//
// .z 包是一个 zip 归档文件，包含两个主要部分:
//
// ┌──────────────────────────────────────────┐
// │                .z 包结构                   │
// ├──────────────────────────────────────────┤
// │ META-INF/                                │
// │   ├── MANIFEST.toml    ← 存根: 包元信息    │
// │   ├── EXPORTS.zstub     ← 存根: 导出符号表  │
// │   └── DEPS.toml         ← 存根: 依赖声明    │
// │                                          │
// │ code/                                    │
// │   ├── main.hy          ← 代码: 入口文件    │
// │   ├── utils.hy         ← 代码: 模块文件    │
// │   └── ...                                │
// │                                          │
// │ resources/             ← 资源文件（可选）   │
// │   └── ...                                │
// └──────────────────────────────────────────┘
//
// 存根 (Stub) 部分:
//   - MANIFEST.toml: 包名、版本、描述、作者、入口
//   - EXPORTS.zstub: 导出的函数/变量/类型签名（供 IDE 和类型检查用）
//   - DEPS.toml: 运行时依赖列表
//
// 代码 (Code) 部分:
//   - .hy 源文件（保持原始结构）
//
// 使用方式:
//   zpm pack                  → 将当前项目打包为 名称-版本.z
//   zpm install xxx.z         → 从 .z 包安装
//   zpm run xxx.z             → 直接运行 .z 包

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ==================== 常量 ====================

const (
	PackageExtension = ".z"        // .z 包文件扩展名
	MetaDir          = "META-INF"  // 存根目录
	CodeDir          = "code"      // 代码目录
	ResourceDir      = "resources" // 资源目录

	ManifestStub = "META-INF/MANIFEST.toml" // 包元信息存根
	ExportsStub  = "META-INF/EXPORTS.zstub" // 导出符号存根
	DepsStub     = "META-INF/DEPS.toml"     // 依赖存根
	ReadmeStub   = "META-INF/README.md"     // 说明文件存根
)

// ZPackage .z 包结构
type ZPackage struct {
	Manifest  *Manifest         // 包元信息
	Exports   *ExportTable      // 导出符号表
	Files     map[string][]byte // 代码文件: 相对路径 → 内容
	Resources map[string][]byte // 资源文件: 相对路径 → 内容
}

// ExportTable 导出符号表（存根）
type ExportTable struct {
	Functions []ExportFunc   `json:"functions"`
	Variables []ExportVar    `json:"variables"`
	Modules   []ExportModule `json:"modules"`
}

// ExportFunc 导出的函数签名
type ExportFunc struct {
	Name       string `json:"name"`
	Params     string `json:"params"`      // 参数列表，如 "(a, b)"
	ReturnType string `json:"return_type"` // 返回类型，如 "字符串"
	File       string `json:"file"`        // 定义所在文件
	Line       int    `json:"line"`
}

// ExportVar 导出的变量
type ExportVar struct {
	Name string `json:"name"`
	Type string `json:"type"`
	File string `json:"file"`
}

// ExportModule 导出的模块
type ExportModule struct {
	Name string `json:"name"`
	File string `json:"file"`
}

// ==================== 打包 ====================

// PackOptions 打包选项
type PackOptions struct {
	OutputDir     string // 输出目录（默认当前目录）
	OutputName    string // 自定义输出文件名（不含扩展名）
	IncludeTests  bool   // 是否包含测试文件
	IncludeReadme bool   // 是否包含 README
}

// Pack 将项目目录打包为 .z 文件
func Pack(projectDir string, opts PackOptions) (string, error) {
	// 加载清单
	manifest, err := LoadManifest(projectDir)
	if err != nil {
		return "", fmt.Errorf("无法加载清单: %w", err)
	}

	// 收集代码文件
	files, err := collectSourceFiles(projectDir, opts.IncludeTests)
	if err != nil {
		return "", fmt.Errorf("收集源文件失败: %w", err)
	}

	// 生成导出符号表（存根）
	exports := generateExports(projectDir, manifest, files)

	// 收集资源文件
	resources, _ := collectResources(projectDir)

	// 确定输出文件名
	outputName := opts.OutputName
	if outputName == "" {
		outputName = fmt.Sprintf("%s-%s", manifest.Name, manifest.Version)
	}

	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = projectDir
	}
	outputPath := filepath.Join(outputDir, outputName+PackageExtension)

	// 创建 .z 文件
	if err := writeZPackage(outputPath, manifest, exports, files, resources, projectDir, opts); err != nil {
		return "", fmt.Errorf("写入 .z 包失败: %w", err)
	}

	return outputPath, nil
}

// writeZPackage 写入 .z 包（zip 格式）
func writeZPackage(outputPath string, manifest *Manifest, exports *ExportTable,
	files map[string][]byte, resources map[string][]byte,
	projectDir string, opts PackOptions) error {

	// 确保输出目录存在
	os.MkdirAll(filepath.Dir(outputPath), 0755)

	zipFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// 1. 写入存根: MANIFEST.toml
	manifestData := []byte(manifest.Serialize())
	if err := addZipEntry(zipWriter, ManifestStub, manifestData); err != nil {
		return err
	}

	// 2. 写入存根: EXPORTS.zstub
	exportsData := []byte(serializeExports(exports))
	if err := addZipEntry(zipWriter, ExportsStub, exportsData); err != nil {
		return err
	}

	// 3. 写入存根: DEPS.toml
	depsData := []byte(serializeDeps(manifest))
	if err := addZipEntry(zipWriter, DepsStub, depsData); err != nil {
		return err
	}

	// 4. 写入 README（如果存在且要求包含）
	if opts.IncludeReadme {
		if readmeData, err := os.ReadFile(filepath.Join(projectDir, "README.md")); err == nil {
			if err := addZipEntry(zipWriter, ReadmeStub, readmeData); err != nil {
				return err
			}
		}
	}

	// 5. 写入代码文件
	// 按路径排序
	filePaths := make([]string, 0, len(files))
	for path := range files {
		filePaths = append(filePaths, path)
	}
	sort.Strings(filePaths)

	for _, path := range filePaths {
		zipPath := CodeDir + "/" + path
		if err := addZipEntry(zipWriter, zipPath, files[path]); err != nil {
			return err
		}
	}

	// 6. 写入资源文件
	for path, data := range resources {
		zipPath := ResourceDir + "/" + path
		if err := addZipEntry(zipWriter, zipPath, data); err != nil {
			return err
		}
	}

	return nil
}

// addZipEntry 向 zip 添加一个文件条目
func addZipEntry(w *zip.Writer, path string, data []byte) error {
	writer, err := w.Create(path)
	if err != nil {
		return fmt.Errorf("创建 zip 条目 %s 失败: %w", path, err)
	}
	_, err = writer.Write(data)
	return err
}

// ==================== 解包 ====================

// Unpack 解包 .z 文件到目标目录
func Unpack(zFilePath string, destDir string) (*ZPackage, error) {
	reader, err := zip.OpenReader(zFilePath)
	if err != nil {
		return nil, fmt.Errorf("无法打开 .z 包: %w", err)
	}
	defer reader.Close()

	pkg := &ZPackage{
		Files:     make(map[string][]byte),
		Resources: make(map[string][]byte),
	}

	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}

		data, err := readZipFile(file)
		if err != nil {
			return nil, fmt.Errorf("读取 %s 失败: %w", file.Name, err)
		}

		switch {
		case file.Name == ManifestStub:
			m, err := ParseManifest(string(data))
			if err != nil {
				return nil, fmt.Errorf("解析存根 MANIFEST.toml 失败: %w", err)
			}
			pkg.Manifest = m

		case file.Name == ExportsStub:
			pkg.Exports = parseExports(string(data))

		case file.Name == DepsStub:
			// 依赖信息已在 Manifest 中

		case strings.HasPrefix(file.Name, CodeDir+"/"):
			relPath := strings.TrimPrefix(file.Name, CodeDir+"/")
			pkg.Files[relPath] = data

		case strings.HasPrefix(file.Name, ResourceDir+"/"):
			relPath := strings.TrimPrefix(file.Name, ResourceDir+"/")
			pkg.Resources[relPath] = data
		}

		// 同时写入到目标目录
		targetPath := filepath.Join(destDir, file.Name)
		os.MkdirAll(filepath.Dir(targetPath), 0755)
		os.WriteFile(targetPath, data, 0644)
	}

	if pkg.Manifest == nil {
		return nil, fmt.Errorf(".z 包缺少存根 MANIFEST.toml")
	}

	return pkg, nil
}

// ReadZManifest 只读取 .z 包的存根（不解包代码）
func ReadZManifest(zFilePath string) (*Manifest, error) {
	reader, err := zip.OpenReader(zFilePath)
	if err != nil {
		return nil, fmt.Errorf("无法打开 .z 包: %w", err)
	}
	defer reader.Close()

	for _, file := range reader.File {
		if file.Name == ManifestStub {
			data, err := readZipFile(file)
			if err != nil {
				return nil, err
			}
			return ParseManifest(string(data))
		}
	}

	return nil, fmt.Errorf(".z 包缺少存根 MANIFEST.toml")
}

// readZipFile 读取 zip 中的单个文件
func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// ==================== 存根序列化 ====================

// SerializeExports 序列化导出符号表（导出版本，用于测试）
func SerializeExports(exports *ExportTable) string {
	return serializeExports(exports)
}

// serializeExports 序列化导出符号表
func serializeExports(exports *ExportTable) string {
	var b strings.Builder

	b.WriteString("# 导出符号表 (EXPORTS.zstub)\n")
	b.WriteString("# 此文件由 zpm pack 自动生成，请勿手动修改\n\n")

	if exports == nil {
		b.WriteString("# (无导出符号)\n")
		return b.String()
	}

	// 函数
	if len(exports.Functions) > 0 {
		b.WriteString("[函数]\n")
		for _, fn := range exports.Functions {
			b.WriteString(fmt.Sprintf("%q = { 参数 = %q, 返回 = %q, 文件 = %q, 行 = %d }\n",
				fn.Name, fn.Params, fn.ReturnType, fn.File, fn.Line))
		}
		b.WriteString("\n")
	}

	// 变量
	if len(exports.Variables) > 0 {
		b.WriteString("[变量]\n")
		for _, v := range exports.Variables {
			b.WriteString(fmt.Sprintf("%q = { 类型 = %q, 文件 = %q }\n", v.Name, v.Type, v.File))
		}
		b.WriteString("\n")
	}

	// 模块
	if len(exports.Modules) > 0 {
		b.WriteString("[模块]\n")
		for _, m := range exports.Modules {
			b.WriteString(fmt.Sprintf("%q = { 文件 = %q }\n", m.Name, m.File))
		}
	}

	return b.String()
}

// serializeDeps 序列化依赖信息
func serializeDeps(manifest *Manifest) string {
	var b strings.Builder

	b.WriteString("# 依赖声明 (DEPS.toml)\n\n")

	if len(manifest.Dependencies) > 0 {
		b.WriteString("[依赖]\n")
		writeSortedDeps(&b, manifest.Dependencies)
	}

	return b.String()
}

// ParseExports 解析导出符号表（导出版本，用于测试）
func ParseExports(data string) *ExportTable {
	return parseExports(data)
}

// parseExports 解析导出符号表
func parseExports(data string) *ExportTable {
	exports := &ExportTable{}
	section := ""

	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}

		// 简化解析: 提取名称
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		name := unquote(strings.TrimSpace(line[:idx]))

		switch section {
		case "函数":
			exports.Functions = append(exports.Functions, ExportFunc{Name: name})
		case "变量":
			exports.Variables = append(exports.Variables, ExportVar{Name: name})
		case "模块":
			exports.Modules = append(exports.Modules, ExportModule{Name: name})
		}
	}

	return exports
}

// ==================== 源文件收集 ====================

// collectSourceFiles 收集项目中的 .hy 源文件
func collectSourceFiles(projectDir string, includeTests bool) (map[string][]byte, error) {
	files := make(map[string][]byte)

	err := filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			// 跳过依赖目录、隐藏目录、.git
			if name == ModulesDir || name == ".git" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		// 只收集 .hy 文件
		if !strings.HasSuffix(path, ".hy") {
			return nil
		}

		// 跳过测试文件（如果不包含测试）
		if !includeTests && (strings.HasSuffix(path, "_test.hy") || strings.HasSuffix(path, ".test.hy")) {
			return nil
		}

		// 计算相对路径
		relPath, err := filepath.Rel(projectDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		files[relPath] = data
		return nil
	})

	return files, err
}

// collectResources 收集资源文件
func collectResources(projectDir string) (map[string][]byte, error) {
	resources := make(map[string][]byte)
	resDir := filepath.Join(projectDir, "resources")

	if _, err := os.Stat(resDir); err != nil {
		return resources, nil // 无资源目录
	}

	err := filepath.Walk(resDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		relPath, err := filepath.Rel(resDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		resources[relPath] = data
		return nil
	})

	return resources, err
}

// ==================== 导出符号生成 ====================

// generateExports 从源文件中提取导出符号（简化版）
// 完整实现需要解析 AST，这里通过简单的文本扫描提取函数/变量定义
func generateExports(projectDir string, manifest *Manifest, files map[string][]byte) *ExportTable {
	exports := &ExportTable{}

	// 对每个文件扫描导出符号
	for filePath, content := range files {
		scanExports(string(content), filePath, exports)
	}

	return exports
}

// ScanExports 扫描源码文本，提取函数/变量定义（导出版本，用于测试）
func ScanExports(source string, filePath string, exports *ExportTable) {
	scanExports(source, filePath, exports)
}

// scanExports 扫描源码文本，提取函数/变量定义
func scanExports(source string, filePath string, exports *ExportTable) {
	lines := strings.Split(source, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 函数定义: 函数 名称(参数) { 或 函数 名称(参数) -> 返回类型 {
		if strings.HasPrefix(trimmed, "函数 ") || strings.HasPrefix(trimmed, "func ") {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "函数 "))
			rest = strings.TrimSpace(strings.TrimPrefix(rest, "func "))

			// 提取函数名和参数
			parenIdx := strings.Index(rest, "(")
			if parenIdx > 0 {
				name := strings.TrimSpace(rest[:parenIdx])
				params := ""
				if endIdx := strings.Index(rest, ")"); endIdx > parenIdx {
					params = rest[parenIdx : endIdx+1]
				}

				returnType := ""
				if arrowIdx := strings.Index(rest, "->"); arrowIdx > 0 {
					returnType = strings.TrimSpace(rest[arrowIdx+2:])
					// 去除末尾的 { 或换行
					returnType = strings.TrimRight(returnType, " {")
				}

				exports.Functions = append(exports.Functions, ExportFunc{
					Name:       name,
					Params:     params,
					ReturnType: returnType,
					File:       filePath,
					Line:       i + 1,
				})
			}
			continue
		}

		// 变量定义: 变量 名称 = ... 或 常量 名称 = ...
		if strings.HasPrefix(trimmed, "变量 ") || strings.HasPrefix(trimmed, "常量 ") ||
			strings.HasPrefix(trimmed, "var ") || strings.HasPrefix(trimmed, "const ") {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "变量 "))
			rest = strings.TrimSpace(strings.TrimPrefix(rest, "常量 "))
			rest = strings.TrimSpace(strings.TrimPrefix(rest, "var "))
			rest = strings.TrimSpace(strings.TrimPrefix(rest, "const "))

			eqIdx := strings.Index(rest, "=")
			if eqIdx > 0 {
				name := strings.TrimSpace(rest[:eqIdx])
				exports.Variables = append(exports.Variables, ExportVar{
					Name: name,
					Type: "", // 类型推断需要 AST
					File: filePath,
				})
			}
		}

		// 导出模块声明: 导出 "文件.hy" 为 模块名
		if strings.HasPrefix(trimmed, "导出 ") || strings.HasPrefix(trimmed, "export ") {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "导出 "))
			rest = strings.TrimSpace(strings.TrimPrefix(rest, "export "))

			if asIdx := strings.Index(rest, "为"); asIdx > 0 {
				name := strings.TrimSpace(rest[asIdx+3:])
				exports.Modules = append(exports.Modules, ExportModule{
					Name: name,
					File: filePath,
				})
			} else if asIdx := strings.Index(rest, "as"); asIdx > 0 {
				name := strings.TrimSpace(rest[asIdx+2:])
				exports.Modules = append(exports.Modules, ExportModule{
					Name: name,
					File: filePath,
				})
			}
		}
	}
}

// ==================== 工具函数 ====================

// IsZPackage 判断文件是否为 .z 包
func IsZPackage(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), PackageExtension)
}

// ListZContents 列出 .z 包内容
func ListZContents(zFilePath string) ([]string, error) {
	reader, err := zip.OpenReader(zFilePath)
	if err != nil {
		return nil, fmt.Errorf("无法打开 .z 包: %w", err)
	}
	defer reader.Close()

	var entries []string
	for _, file := range reader.File {
		entries = append(entries, file.Name)
	}
	return entries, nil
}

// VerifyZPackage 验证 .z 包完整性
func VerifyZPackage(zFilePath string) error {
	reader, err := zip.OpenReader(zFilePath)
	if err != nil {
		return fmt.Errorf("无法打开 .z 包: %w", err)
	}
	defer reader.Close()

	hasManifest := false
	hasCode := false

	for _, file := range reader.File {
		if file.Name == ManifestStub {
			hasManifest = true
		}
		if strings.HasPrefix(file.Name, CodeDir+"/") {
			hasCode = true
		}
	}

	if !hasManifest {
		return fmt.Errorf(".z 包缺少存根 MANIFEST.toml")
	}
	if !hasCode {
		return fmt.Errorf(".z 包缺少代码文件")
	}

	return nil
}

// PackToBytes 将项目打包为字节流（用于网络传输）
func PackToBytes(projectDir string) ([]byte, error) {
	manifest, err := LoadManifest(projectDir)
	if err != nil {
		return nil, err
	}

	files, err := collectSourceFiles(projectDir, false)
	if err != nil {
		return nil, err
	}

	exports := generateExports(projectDir, manifest, files)
	resources, _ := collectResources(projectDir)

	// 写入内存中的 zip
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	// 存根
	addZipEntry(zipWriter, ManifestStub, []byte(manifest.Serialize()))
	addZipEntry(zipWriter, ExportsStub, []byte(serializeExports(exports)))
	addZipEntry(zipWriter, DepsStub, []byte(serializeDeps(manifest)))

	// 代码
	filePaths := make([]string, 0, len(files))
	for path := range files {
		filePaths = append(filePaths, path)
	}
	sort.Strings(filePaths)

	for _, path := range filePaths {
		addZipEntry(zipWriter, CodeDir+"/"+path, files[path])
	}

	// 资源
	for path, data := range resources {
		addZipEntry(zipWriter, ResourceDir+"/"+path, data)
	}

	zipWriter.Close()
	return buf.Bytes(), nil
}
