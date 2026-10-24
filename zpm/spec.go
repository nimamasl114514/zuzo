package zpm

// spec.go — 依赖规格 (dependency spec) 解析
//
// 依赖规格格式：
//
//	"1.2.0"                          — 注册表语义版本
//	"^1.2.0"                         — 兼容版本 (^1.2.0 → >=1.2.0, <2.0.0)
//	"~1.2.0"                         — 补丁版本 (~1.2.0 → >=1.2.0, <1.3.0)
//	"github.com/user/repo"           — Git 仓库（最新）
//	"github.com/user/repo@v1.2.0"    — Git 仓库 + 标签
//	"github.com/user/repo#main"      — Git 仓库 + 分支
//	"file:../libs/mylib"             — 本地路径
//	"./libs/mylib"                   — 本地路径（简写）

import (
	"fmt"
	"strings"
)

// SpecType 依赖规格类型
type SpecType int

const (
	SpecRegistry SpecType = iota // 注册表版本
	SpecGit                      // Git 仓库
	SpecLocal                    // 本地路径
)

// Spec 解析后的依赖规格
type Spec struct {
	Raw       string   // 原始规格字符串
	Type      SpecType // 规格类型
	Version   string   // 语义版本（仅 SpecRegistry）
	GitURL    string   // Git URL（仅 SpecGit）
	GitRef    string   // Git 引用: tag/branch/commit（仅 SpecGit）
	GitSubdir string   // Git 仓库子目录（仅 SpecGit，可选）
	LocalPath string   // 本地路径（仅 SpecLocal）
}

// ParseSpec 解析依赖规格字符串
func ParseSpec(raw string) (*Spec, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("依赖规格不能为空")
	}

	spec := &Spec{Raw: raw}

	// 本地路径: file:xxx 或 ./xxx 或 ../xxx
	if strings.HasPrefix(raw, "file:") {
		spec.Type = SpecLocal
		spec.LocalPath = strings.TrimPrefix(raw, "file:")
		return spec, nil
	}
	if strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") {
		spec.Type = SpecLocal
		spec.LocalPath = raw
		return spec, nil
	}

	// Git: github.com/... 或 git@... 或 https://...git
	if strings.HasPrefix(raw, "github.com/") ||
		strings.HasPrefix(raw, "git@") ||
		strings.HasPrefix(raw, "https://") && strings.HasSuffix(raw, ".git") ||
		strings.HasPrefix(raw, "http://") && strings.HasSuffix(raw, ".git") {

		spec.Type = SpecGit

		// 分离 URL 和 ref
		// 格式: URL@tag  或  URL#branch
		var urlPart, refPart string
		if idx := strings.LastIndex(raw, "@"); idx > 0 && !strings.HasPrefix(raw, "git@") {
			urlPart = raw[:idx]
			refPart = raw[idx+1:]
		} else if idx := strings.LastIndex(raw, "#"); idx > 0 {
			urlPart = raw[:idx]
			refPart = raw[idx+1:]
		} else {
			urlPart = raw
		}

		// 处理子目录: URL:subdir
		if idx := strings.Index(urlPart, ":"); idx > 0 && !strings.HasPrefix(urlPart, "http") && !strings.HasPrefix(urlPart, "git@") {
			spec.GitURL = urlPart[:idx]
			spec.GitSubdir = urlPart[idx+1:]
		} else {
			spec.GitURL = urlPart
		}
		spec.GitRef = refPart
		return spec, nil
	}

	// 注册表版本
	spec.Type = SpecRegistry
	spec.Version = raw
	return spec, nil
}

// String 返回规格的可读描述
func (s *Spec) String() string {
	switch s.Type {
	case SpecRegistry:
		return fmt.Sprintf("注册表:%s", s.Version)
	case SpecGit:
		if s.GitRef != "" {
			return fmt.Sprintf("Git:%s@%s", s.GitURL, s.GitRef)
		}
		return fmt.Sprintf("Git:%s", s.GitURL)
	case SpecLocal:
		return fmt.Sprintf("本地:%s", s.LocalPath)
	}
	return s.Raw
}

// IsRegistry 是否为注册表依赖
func (s *Spec) IsRegistry() bool { return s.Type == SpecRegistry }

// IsGit 是否为 Git 依赖
func (s *Spec) IsGit() bool { return s.Type == SpecGit }

// IsLocal 是否为本地依赖
func (s *Spec) IsLocal() bool { return s.Type == SpecLocal }
