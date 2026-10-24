package zpm

// manifest.go — 包清单 (zpm.toml) 定义与解析
//
// 清单格式示例:
//
//	[包]
//	名称 = "myapp"
//	版本 = "1.0.0"
//	描述 = "我的应用"
//	作者 = "zuzo"
//
//	[依赖]
//	"json工具" = "1.2.0"
//	"网络请求" = "github.com/user/netreq@v1.0.0"
//	"本地库" = "file:../libs/mylib"
//
//	[开发依赖]
//	"测试框架" = "1.0.0"

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Manifest 包清单结构
type Manifest struct {
	Name         string            // 包名
	Version      string            // 语义化版本号
	Description  string            // 简短描述
	Author       string            // 作者
	License      string            // 许可证
	Entry        string            // 入口文件（默认 main.hy）
	Dependencies map[string]string // 运行时依赖: name → spec
	DevDeps      map[string]string // 开发依赖: name → spec

	// 来源信息（仅对已安装的包有意义）
	Source string // git URL / file path / registry
}

// NewManifest 创建空清单
func NewManifest() *Manifest {
	return &Manifest{
		Dependencies: make(map[string]string),
		DevDeps:      make(map[string]string),
	}
}

// ManifestFile 清单文件名
const ManifestFile = "zpm.toml"

// LockFile 锁文件名
const LockFile = "zpm.lock"

// ModulesDir 依赖安装目录名
const ModulesDir = "zpm_modules"

// ParseManifest 从原始文本解析清单
func ParseManifest(data string) (*Manifest, error) {
	m := NewManifest()
	section := ""
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 区段标记 [xxx]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.Trim(line, "[]"))
			continue
		}

		// key = value
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])

		// 去除引号
		key = unquote(key)
		val = unquote(val)

		switch section {
		case "包", "package":
			switch key {
			case "名称", "name":
				m.Name = val
			case "版本", "version":
				m.Version = val
			case "描述", "description":
				m.Description = val
			case "作者", "author":
				m.Author = val
			case "许可证", "license":
				m.License = val
			case "入口", "entry":
				m.Entry = val
			}
		case "依赖", "dependencies":
			m.Dependencies[key] = val
		case "开发依赖", "dev-dependencies", "devdeps":
			m.DevDeps[key] = val
		}
	}

	if err := m.Validate(); err != nil {
		return nil, err
	}
	return m, nil
}

// Validate 校验清单基本完整性
func (m *Manifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("包清单缺少 [包] 名称")
	}
	if m.Version == "" {
		return fmt.Errorf("包清单缺少 [包] 版本")
	}
	return nil
}

// Serialize 将清单序列化为 zpm.toml 格式文本
func (m *Manifest) Serialize() string {
	var b strings.Builder

	b.WriteString("[包]\n")
	b.WriteString(fmt.Sprintf("名称 = %q\n", m.Name))
	b.WriteString(fmt.Sprintf("版本 = %q\n", m.Version))
	if m.Description != "" {
		b.WriteString(fmt.Sprintf("描述 = %q\n", m.Description))
	}
	if m.Author != "" {
		b.WriteString(fmt.Sprintf("作者 = %q\n", m.Author))
	}
	if m.License != "" {
		b.WriteString(fmt.Sprintf("许可证 = %q\n", m.License))
	}
	if m.Entry != "" {
		b.WriteString(fmt.Sprintf("入口 = %q\n", m.Entry))
	}
	b.WriteString("\n")

	if len(m.Dependencies) > 0 {
		b.WriteString("[依赖]\n")
		writeSortedDeps(&b, m.Dependencies)
		b.WriteString("\n")
	}

	if len(m.DevDeps) > 0 {
		b.WriteString("[开发依赖]\n")
		writeSortedDeps(&b, m.DevDeps)
		b.WriteString("\n")
	}

	return b.String()
}

func writeSortedDeps(b *strings.Builder, deps map[string]string) {
	keys := make([]string, 0, len(deps))
	for k := range deps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(fmt.Sprintf("%q = %q\n", k, deps[k]))
	}
}

// LoadManifest 从目录读取 zpm.toml
func LoadManifest(dir string) (*Manifest, error) {
	path := filepath.Join(dir, ManifestFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("无法读取清单文件 %s: %w", path, err)
	}
	return ParseManifest(string(data))
}

// SaveManifest 将清单写入目录下的 zpm.toml
func SaveManifest(dir string, m *Manifest) error {
	path := filepath.Join(dir, ManifestFile)
	return os.WriteFile(path, []byte(m.Serialize()), 0644)
}

// unquote 去除字符串两端的引号
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
