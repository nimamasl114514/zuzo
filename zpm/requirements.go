package zpm

// requirements.go — pip 风格依赖文件 (依赖.txt) 解析器
//
// 依赖文件格式（类似 pip 的 requirements.txt）:
//
//	# 注释行
//	网络请求                    # 安装最新版本
//	json工具==1.2.0             # 精确版本
//	日期库>=1.0.0,<2.0.0        # 版本范围
//	测试框架~=1.0.0             # 兼容版本
//	开发工具!=1.5.0             # 排除特定版本
//
//	# 从 Git 仓库安装
//	git+github.com/zuzo/dev@v1.0.0
//
//	# 从本地路径安装
//	-e ./libs/mylib
//	file:../libs/other

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RequirementsFile 依赖文件名
const RequirementsFile = "依赖.txt"

// Requirement 单条依赖需求
type Requirement struct {
	Name        string // 包名（注册表中的名称）
	VersionSpec string // 版本规格（==1.2.0, >=1.0.0, 空表示最新）
	Raw         string // 原始文本行
	Source      string // 来源类型: "registry" | "git" | "github" | "local"
	SourceURL   string // Git URL / owner/repo / 本地路径（仅 git/github/local 类型）
	Subdir      string // 仓库内子目录（仅 github/git 类型，可选）
}

// Requirements 依赖文件解析结果
type Requirements struct {
	Items []*Requirement
}

// ParseRequirements 从文本解析依赖文件
func ParseRequirements(data string) (*Requirements, error) {
	reqs := &Requirements{}

	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)

		// 空行或注释
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 去除行内注释
		if idx := strings.Index(line, " #"); idx > 0 {
			line = strings.TrimSpace(line[:idx])
		}

		req, err := parseRequirementLine(line)
		if err != nil {
			return nil, fmt.Errorf("解析依赖行 %q 失败: %w", line, err)
		}
		reqs.Items = append(reqs.Items, req)
	}

	return reqs, nil
}

// ParseRequirementLine 解析单行依赖（公开接口，供 CLI 调用）
func ParseRequirementLine(line string) (*Requirement, error) {
	return parseRequirementLine(line)
}

// parseRequirementLine 解析单行依赖
func parseRequirementLine(line string) (*Requirement, error) {
	req := &Requirement{Raw: line}

	// .z 包文件: xxx.z 或 ./path/xxx.z 或 /abs/path/xxx.z
	if IsZPackage(line) {
		req.Source = "zpackage"
		req.SourceURL = line
		// 从文件名推导包名（去掉 .z 后缀）
		base := filepath.Base(line)
		req.Name = strings.TrimSuffix(base, PackageExtension)
		return req, nil
	}

	// -e ./path 或 -e file:path → 本地可编辑安装
	if strings.HasPrefix(line, "-e ") {
		path := strings.TrimSpace(strings.TrimPrefix(line, "-e "))
		// -e ./xxx.z → .z 包安装
		if IsZPackage(path) {
			req.Source = "zpackage"
			req.SourceURL = path
			base := filepath.Base(path)
			req.Name = strings.TrimSuffix(base, PackageExtension)
			return req, nil
		}
		req.Source = "local"
		req.SourceURL = path
		req.Name = filepath.Base(path)
		return req, nil
	}

	// file:path → 本地安装
	if strings.HasPrefix(line, "file:") {
		path := strings.TrimPrefix(line, "file:")
		req.Source = "local"
		req.SourceURL = path
		req.Name = filepath.Base(path)
		return req, nil
	}

	// ./path 或 ../path → 本地安装
	if strings.HasPrefix(line, "./") || strings.HasPrefix(line, "../") {
		req.Source = "local"
		req.SourceURL = line
		req.Name = filepath.Base(line)
		return req, nil
	}

	// git+url[@ref] → Git 安装
	if strings.HasPrefix(line, "git+") {
		rest := strings.TrimPrefix(line, "git+")
		req.Source = "git"

		// 分离 URL 和 ref
		var url, ref string
		if idx := strings.LastIndex(rest, "@"); idx > 0 {
			url = rest[:idx]
			ref = rest[idx+1:]
		} else {
			url = rest
		}
		req.SourceURL = url
		if ref != "" {
			req.SourceURL += "@" + ref
		}

		// 从 URL 推导包名
		req.Name = packageNameFromGitURL(url)
		return req, nil
	}

	// gh:owner/repo[@ref] → GitHub 仓库安装
	if strings.HasPrefix(line, "gh:") {
		repo, ok := ParseGitHubRepo(line)
		if ok {
			return githubRepoToRequirement(repo, line), nil
		}
	}

	// github.com/... → GitHub/Git 安装
	if strings.HasPrefix(line, "github.com/") {
		repo, ok := ParseGitHubRepo(line)
		if ok {
			return githubRepoToRequirement(repo, line), nil
		}
	}

	// https://github.com/... → GitHub 安装
	if strings.HasPrefix(line, "https://github.com/") || strings.HasPrefix(line, "http://github.com/") {
		repo, ok := ParseGitHubRepo(line)
		if ok {
			return githubRepoToRequirement(repo, line), nil
		}
	}

	// https://...git 或 http://...git → 其他 Git 仓库
	if strings.HasPrefix(line, "https://") && strings.HasSuffix(line, ".git") ||
		strings.HasPrefix(line, "http://") && strings.HasSuffix(line, ".git") {
		req.Source = "git"

		var url, ref string
		if idx := strings.LastIndex(line, "@"); idx > 0 && !strings.HasPrefix(line, "git@") {
			url = line[:idx]
			ref = line[idx+1:]
		} else if idx := strings.LastIndex(line, "#"); idx > 0 {
			url = line[:idx]
			ref = line[idx+1:]
		} else {
			url = line
		}
		req.SourceURL = url
		if ref != "" {
			req.SourceURL += "@" + ref
		}
		req.Name = packageNameFromGitURL(url)
		return req, nil
	}

	// owner/repo → GitHub 简写安装（如 zuzo-lang/utils）
	if IsGitHubShorthand(line) {
		repo, ok := ParseGitHubRepo(line)
		if ok {
			return githubRepoToRequirement(repo, line), nil
		}
	}

	// 注册表包: name 或 name==version 或 name>=1.0.0 等
	req.Source = "registry"

	// 查找版本操作符的位置
	ops := []string{"==", ">=", "<=", "~=", "!=", ">", "<"}
	opIdx := -1
	for _, op := range ops {
		if idx := strings.Index(line, op); idx > 0 && (opIdx == -1 || idx < opIdx) {
			opIdx = idx
		}
	}

	if opIdx > 0 {
		req.Name = strings.TrimSpace(line[:opIdx])
		req.VersionSpec = strings.TrimSpace(line[opIdx:])
	} else {
		req.Name = strings.TrimSpace(line)
		req.VersionSpec = ""
	}

	return req, nil
}

// packageNameFromGitURL 从 Git URL 推导包名
func packageNameFromGitURL(url string) string {
	// 去除协议前缀
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "git@")

	// 去除 .git 后缀
	url = strings.TrimSuffix(url, ".git")

	// 取最后一段路径
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return url
}

// githubRepoToRequirement 将 GitHubRepo 转为 Requirement
func githubRepoToRequirement(repo *GitHubRepo, raw string) *Requirement {
	// 包名优先使用仓库名
	name := repo.Name

	// 如果有子目录，包名取子目录名
	if repo.Subdir != "" {
		parts := strings.Split(repo.Subdir, "/")
		name = parts[len(parts)-1]
	}

	// 版本规格使用 ref（tag/branch/commit）
	versionSpec := ""
	if repo.Ref != "" {
		versionSpec = "@" + repo.Ref
	}

	return &Requirement{
		Name:        name,
		Source:      "github",
		SourceURL:   repo.FullName(),
		VersionSpec: versionSpec,
		Subdir:      repo.Subdir,
		Raw:         raw,
	}
}

// LoadRequirements 从目录读取依赖文件
func LoadRequirements(dir string) (*Requirements, error) {
	path := filepath.Join(dir, RequirementsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("无法读取依赖文件 %s: %w", path, err)
	}
	return ParseRequirements(string(data))
}

// SaveRequirements 将依赖写入 依赖.txt
func SaveRequirements(dir string, reqs *Requirements) error {
	path := filepath.Join(dir, RequirementsFile)

	// 按名称排序
	items := make([]*Requirement, len(reqs.Items))
	copy(items, reqs.Items)
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})

	var b strings.Builder
	for _, req := range items {
		switch req.Source {
		case "local":
			b.WriteString(fmt.Sprintf("-e %s\n", req.SourceURL))
		case "git":
			b.WriteString(fmt.Sprintf("git+%s\n", req.SourceURL))
		case "github":
			// GitHub 来源: owner/repo[@ref] 或 owner/repo/subdir[@ref]
			line := req.SourceURL
			if req.Subdir != "" {
				line += "/" + req.Subdir
			}
			if req.VersionSpec != "" {
				line += req.VersionSpec // VersionSpec 已包含 @ 前缀
			}
			b.WriteString(line + "\n")
		case "zpackage":
			// .z 包: 直接写入文件路径
			b.WriteString(req.SourceURL + "\n")
		default:
			if req.VersionSpec != "" {
				b.WriteString(fmt.Sprintf("%s%s\n", req.Name, req.VersionSpec))
			} else {
				b.WriteString(fmt.Sprintf("%s\n", req.Name))
			}
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0644)
}

// AddRequirement 向依赖列表添加一条（如果已存在则更新）
func (r *Requirements) Add(req *Requirement) {
	for i, existing := range r.Items {
		if existing.Name == req.Name {
			r.Items[i] = req
			return
		}
	}
	r.Items = append(r.Items, req)
}

// RemoveRequirement 从依赖列表移除
func (r *Requirements) Remove(name string) bool {
	for i, existing := range r.Items {
		if existing.Name == name {
			r.Items = append(r.Items[:i], r.Items[i+1:]...)
			return true
		}
	}
	return false
}

// Find 查找依赖
func (r *Requirements) Find(name string) (*Requirement, bool) {
	for _, item := range r.Items {
		if item.Name == name {
			return item, true
		}
	}
	return nil, false
}

// Freeze 输出 freeze 格式（name==version）
func (r *Requirements) Freeze(installed map[string]string) string {
	var b strings.Builder
	items := make([]*Requirement, len(r.Items))
	copy(items, r.Items)
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})

	for _, req := range items {
		if version, ok := installed[req.Name]; ok {
			b.WriteString(fmt.Sprintf("%s==%s\n", req.Name, version))
		} else {
			// 未安装的用原始规格
			if req.VersionSpec != "" {
				b.WriteString(fmt.Sprintf("%s%s\n", req.Name, req.VersionSpec))
			} else {
				b.WriteString(fmt.Sprintf("%s\n", req.Name))
			}
		}
	}
	return b.String()
}
