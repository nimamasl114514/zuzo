package zpm

// registry.go — 本地注册表索引
//
// 注册表维护一个本地缓存索引（~/.zuzo/registry/），记录已知的包及其可用版本。
// 在没有远程注册表服务器的情况下，注册表通过 Git 仓库或本地路径直接解析。
//
// 注册表索引文件格式 (index.json):
//
//	{
//	  "packages": {
//	    "json工具": {
//	      "versions": ["1.0.0", "1.1.0", "1.2.0"],
//	      "latest": "1.2.0",
//	      "source": "github.com/zuzo-lang/json_utils"
//	    }
//	  }
//	}

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// RegistryEntry 注册表中一个包的元信息
type RegistryEntry struct {
	Name        string   `json:"name"`
	Versions    []string `json:"versions"`
	Latest      string   `json:"latest"`
	Source      string   `json:"source"` // Git URL
	Description string   `json:"description"`
}

// Registry 本地注册表索引
type Registry struct {
	Packages map[string]*RegistryEntry `json:"packages"`
	path     string                    // 索引文件路径
}

// DefaultRegistryDir 默认注册表目录 (~/.zuzo/registry/)
func DefaultRegistryDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".zuzo", "registry")
}

// LoadRegistry 加载本地注册表索引，不存在则创建空索引
func LoadRegistry() (*Registry, error) {
	dir := DefaultRegistryDir()
	path := filepath.Join(dir, "index.json")

	r := &Registry{
		Packages: make(map[string]*RegistryEntry),
		path:     path,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil // 空注册表
		}
		return nil, fmt.Errorf("无法读取注册表: %w", err)
	}

	if err := json.Unmarshal(data, r); err != nil {
		return nil, fmt.Errorf("注册表格式错误: %w", err)
	}
	if r.Packages == nil {
		r.Packages = make(map[string]*RegistryEntry)
	}
	return r, nil
}

// Save 持久化注册表索引
func (r *Registry) Save() error {
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("无法创建注册表目录: %w", err)
	}

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化注册表失败: %w", err)
	}
	return os.WriteFile(r.path, data, 0644)
}

// Lookup 查找包
func (r *Registry) Lookup(name string) (*RegistryEntry, bool) {
	entry, ok := r.Packages[name]
	return entry, ok
}

// AddOrUpdate 添加或更新包信息
func (r *Registry) AddOrUpdate(entry *RegistryEntry) {
	r.Packages[entry.Name] = entry
}

// Remove 移除包
func (r *Registry) Remove(name string) bool {
	if _, ok := r.Packages[name]; ok {
		delete(r.Packages, name)
		return true
	}
	return false
}

// Search 搜索包名包含关键字的包
func (r *Registry) Search(keyword string) []*RegistryEntry {
	var results []*RegistryEntry
	for _, entry := range r.Packages {
		if contains(entry.Name, keyword) || contains(entry.Description, keyword) {
			results = append(results, entry)
		}
	}
	// 按名称排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results
}

// ListAll 列出所有包
func (r *Registry) ListAll() []*RegistryEntry {
	var results []*RegistryEntry
	for _, entry := range r.Packages {
		results = append(results, entry)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})
	return results
}

// contains 模糊匹配（不区分大小写）
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > 0 && len(substr) > 0 &&
				indexCI(s, substr) >= 0))
}

// indexCI 不区分大小写的子串搜索
func indexCI(s, substr string) int {
	s = toLower(s)
	substr = toLower(substr)
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func toLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
