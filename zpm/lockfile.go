package zpm

// lockfile.go — 锁文件 (zpm.lock) 生成与解析
//
// 锁文件确保可复现构建：记录每个依赖的精确来源和版本。
// 格式为 JSON，便于机器读写和 diff。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LockEntry 锁文件中单个依赖的记录
type LockEntry struct {
	Name     string `json:"name"`
	Version  string `json:"version"`             // 已安装版本
	Spec     string `json:"spec"`                 // 原始规格
	Source   string `json:"source"`               // 来源: git URL / file path / registry
	Resolved string `json:"resolved"`             // 精确解析地址
	Hash     string `json:"hash,omitempty"`       // 内容哈希（用于变更检测）
}

// Lockfile 锁文件结构
type Lockfile struct {
	Version  int                    `json:"version"`  // 锁文件格式版本
	Packages map[string]*LockEntry  `json:"packages"` // 包名 → 锁定记录
	path     string                  // 锁文件路径（内部使用）
}

// NewLockfile 创建空锁文件
func NewLockfile() *Lockfile {
	return &Lockfile{
		Version:  1,
		Packages: make(map[string]*LockEntry),
	}
}

// LoadLockfile 从目录读取 zpm.lock
func LoadLockfile(dir string) (*Lockfile, error) {
	path := filepath.Join(dir, LockFile)
	lf := NewLockfile()
	lf.path = path

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return lf, nil // 空锁文件
		}
		return nil, fmt.Errorf("无法读取锁文件: %w", err)
	}

	if err := json.Unmarshal(data, lf); err != nil {
		return nil, fmt.Errorf("锁文件格式错误: %w", err)
	}
	if lf.Packages == nil {
		lf.Packages = make(map[string]*LockEntry)
	}
	return lf, nil
}

// Save 将锁文件写入目录
func (lf *Lockfile) Save(dir string) error {
	path := filepath.Join(dir, LockFile)
	lf.path = path

	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化锁文件失败: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// Set 记录一个依赖的锁定信息
func (lf *Lockfile) Set(entry *LockEntry) {
	lf.Packages[entry.Name] = entry
}

// Get 查询一个依赖的锁定信息
func (lf *Lockfile) Get(name string) (*LockEntry, bool) {
	entry, ok := lf.Packages[name]
	return entry, ok
}

// Remove 移除一个依赖的锁定信息
func (lf *Lockfile) Remove(name string) bool {
	if _, ok := lf.Packages[name]; ok {
		delete(lf.Packages, name)
		return true
	}
	return false
}

// IsLocked 检查某个包是否已被锁定
func (lf *Lockfile) IsLocked(name string) bool {
	_, ok := lf.Packages[name]
	return ok
}
