package zpm

// github.go — GitHub 仓库作为包存储后端
//
// 支持:
//
//	# 直接安装（archive 下载，无需 git）
//	zpm install github.com/user/repo
//	zpm install github.com/user/repo@v1.0.0
//	zpm install user/repo                  # GitHub 简写
//	zpm install user/repo@v1.0.0           # 简写 + 版本
//
//	# 依赖文件中
//	github.com/user/repo==v1.0.0           # 依赖.txt
//	user/repo@v1.0.0
//
//	# 子目录
//	github.com/user/monorepo/subdir@v1.0.0
//
// GitHub Archive API (无需 git 即可下载):
//
//	https://github.com/<owner>/<repo>/archive/refs/tags/<tag>.tar.gz
//	https://github.com/<owner>/<repo>/archive/refs/heads/<branch>.tar.gz
//	https://github.com/<owner>/<repo>/archive/<commit>.tar.gz
//
// GitHub API (列出版本):
//
//	GET https://api.github.com/repos/<owner>/<repo>/tags
//	GET https://api.github.com/repos/<owner>/<repo>/releases

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// GitHubRepo 解析后的 GitHub 仓库地址
type GitHubRepo struct {
	Owner    string // 仓库所有者
	Name     string // 仓库名
	Ref      string // tag / branch / commit（空表示默认分支 HEAD）
	Subdir   string // 仓库内子目录（可选）
	RawInput string // 原始输入字符串
}

// IsGitHubShorthand 判断字符串是否为 GitHub 简写格式 (owner/repo)
// 排除: 本地路径 (./xx), file:, git+, http://, 已有 github.com/ 前缀
var gitHubShorthandRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*/[a-zA-Z0-9][a-zA-Z0-9._-]*`)

// IsGitHubShorthand 判断是否为 GitHub owner/repo 简写
func IsGitHubShorthand(s string) bool {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") ||
		strings.HasPrefix(s, "file:") || strings.HasPrefix(s, "git+") ||
		strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "github.com/") || strings.HasPrefix(s, "gh:") {
		return false
	}
	return gitHubShorthandRe.MatchString(s)
}

// IsGitHubURL 判断是否为完整 GitHub URL
func IsGitHubURL(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "github.com/") ||
		strings.HasPrefix(s, "https://github.com/") ||
		strings.HasPrefix(s, "https://www.github.com/") ||
		strings.HasPrefix(s, "gh:")
}

// ParseGitHubRepo 从多种格式解析 GitHub 仓库地址
//
// 支持格式:
//
//	github.com/owner/repo
//	github.com/owner/repo@v1.0.0
//	github.com/owner/repo#main
//	github.com/owner/repo/subdir@v1.0.0
//	https://github.com/owner/repo
//	https://github.com/owner/repo.git
//	gh:owner/repo
//	gh:owner/repo@v1.0.0
//	owner/repo
//	owner/repo@v1.0.0
func ParseGitHubRepo(raw string) (*GitHubRepo, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}

	repo := &GitHubRepo{RawInput: raw}

	input := raw

	// gh: 前缀
	if strings.HasPrefix(input, "gh:") {
		input = strings.TrimPrefix(input, "gh:")
		input = "github.com/" + input
	}

	// https:// 前缀
	input = strings.TrimPrefix(input, "https://")
	input = strings.TrimPrefix(input, "http://")
	input = strings.TrimPrefix(input, "www.")

	// 必须以 github.com/ 开头
	if !strings.HasPrefix(input, "github.com/") {
		// 检查是否为 owner/repo 简写
		if !gitHubShorthandRe.MatchString(input) {
			return nil, false
		}
		input = "github.com/" + input
	}

	// 去掉 github.com/ 前缀
	path := strings.TrimPrefix(input, "github.com/")

	// 去掉 .git 后缀
	path = strings.TrimSuffix(path, ".git")

	// 分离 ref (@tag 或 #branch)
	ref := ""
	if idx := strings.LastIndex(path, "@"); idx > 0 {
		ref = path[idx+1:]
		path = path[:idx]
	} else if idx := strings.LastIndex(path, "#"); idx > 0 {
		ref = path[idx+1:]
		path = path[:idx]
	}

	// 分离 owner/repo 和 subdir
	// owner/repo → 2 段
	// owner/repo/subdir → 前 2 段是 owner/repo，其余是 subdir
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return nil, false
	}

	repo.Owner = parts[0]
	repo.Name = parts[1]
	repo.Ref = ref
	if len(parts) > 2 {
		repo.Subdir = strings.Join(parts[2:], "/")
	}

	return repo, true
}

// FullName 返回 owner/repo 格式
func (r *GitHubRepo) FullName() string {
	return r.Owner + "/" + r.Name
}

// CloneURL 返回 git clone 用的 URL
func (r *GitHubRepo) CloneURL() string {
	return "https://github.com/" + r.Owner + "/" + r.Name + ".git"
}

// ArchiveURL 返回 GitHub Archive 下载 URL (tar.gz)
// 优先使用 tag/branch，无 ref 则用 HEAD
func (r *GitHubRepo) ArchiveURL() string {
	base := "https://github.com/" + r.Owner + "/" + r.Name
	if r.Ref == "" {
		return base + "/archive/HEAD.tar.gz"
	}
	// 判断是 tag 还是 branch: 假设以 v 开头的是 tag
	if strings.HasPrefix(r.Ref, "v") || isSemVerTag(r.Ref) {
		return base + "/archive/refs/tags/" + r.Ref + ".tar.gz"
	}
	// 否则按 branch 处理
	return base + "/archive/refs/heads/" + r.Ref + ".tar.gz"
}

// isSemVerTag 判断是否为语义版本标签
func isSemVerTag(s string) bool {
	// 匹配 1.0.0, 1.0, 1 等格式
	semverRe := regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+)?`)
	return semverRe.MatchString(s)
}

// ==================== GitHub API 客户端 ====================

// GitHubClient GitHub API 客户端
type GitHubClient struct {
	httpClient *http.Client
	token      string // 可选: GitHub Personal Access Token（提高 API 限制）
}

// NewGitHubClient 创建 GitHub 客户端
func NewGitHubClient() *GitHubClient {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	return &GitHubClient{
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		token: token,
	}
}

// httpGet 带 token 的 HTTP GET
func (c *GitHubClient) httpGet(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "zpm/1.0")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.httpClient.Do(req)
}

// DownloadArchive 下载 GitHub 仓库的 tar.gz archive
// 返回字节内容和解压后的顶层目录名
func (c *GitHubClient) DownloadArchive(repo *GitHubRepo) ([]byte, error) {
	url := repo.ArchiveURL()

	resp, err := c.httpGet(url)
	if err != nil {
		return nil, fmt.Errorf("下载 GitHub archive 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("GitHub 仓库或分支/标签不存在: %s", repo.FullName())
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub archive 下载失败: HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// ListTags 列出仓库的所有 tags（作为可用版本）
func (c *GitHubClient) ListTags(owner, repo string) ([]string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/tags?per_page=100", owner, repo)

	resp, err := c.httpGet(url)
	if err != nil {
		return nil, fmt.Errorf("获取 tags 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("仓库不存在: %s/%s", owner, repo)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API 错误: HTTP %d", resp.StatusCode)
	}

	var tags []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, fmt.Errorf("解析 tags 响应失败: %w", err)
	}

	result := make([]string, len(tags))
	for i, t := range tags {
		result[i] = t.Name
	}
	return result, nil
}

// GetRepoInfo 获取仓库基本信息
func (c *GitHubClient) GetRepoInfo(owner, repo string) (*GitHubRepoInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)

	resp, err := c.httpGet(url)
	if err != nil {
		return nil, fmt.Errorf("获取仓库信息失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("仓库不存在: %s/%s", owner, repo)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API 错误: HTTP %d", resp.StatusCode)
	}

	var info GitHubRepoInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("解析仓库信息失败: %w", err)
	}
	info.Owner = owner
	info.Repo = repo
	return &info, nil
}

// GitHubRepoInfo 仓库基本信息
type GitHubRepoInfo struct {
	Owner         string `json:"-"`
	Repo          string `json:"-"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
	Homepage      string `json:"homepage"`
	Stars         int    `json:"stargazers_count"`
	Forks         int    `json:"forks_count"`
	License       string `json:"license"`
}

// ListReleases 列出仓库的 releases
func (c *GitHubClient) ListReleases(owner, repo string) ([]GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases?per_page=30", owner, repo)

	resp, err := c.httpGet(url)
	if err != nil {
		return nil, fmt.Errorf("获取 releases 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API 错误: HTTP %d", resp.StatusCode)
	}

	var releases []GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("解析 releases 响应失败: %w", err)
	}
	return releases, nil
}

// GitHubRelease GitHub Release 信息
type GitHubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	CreatedAt   string `json:"created_at"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
	ZipballURL  string `json:"zipball_url"`
	TarballURL  string `json:"tarball_url"`
}

// ==================== Archive 解压 ====================

// ExtractGitHubArchive 解压 GitHub archive tar.gz 到目标目录
//
// GitHub archive 的特点是顶层有一个目录 (如 owner-repo-v1.0.0/)，
// 我们需要去掉这个顶层目录，直接将内容放到 dest 下。
func ExtractGitHubArchive(data []byte, dest string) error {
	gzReader, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("解压 gzip 失败: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	os.MkdirAll(dest, 0755)

	// GitHub archive 顶层目录名 (如 "owner-repo-v1.0.0/")
	topDir := ""

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取 tar 失败: %w", err)
		}

		name := header.Name

		// 检测顶层目录
		if topDir == "" {
			if idx := strings.Index(name, "/"); idx > 0 {
				topDir = name[:idx+1]
			}
		}

		// 去掉顶层目录前缀
		if topDir != "" && strings.HasPrefix(name, topDir) {
			name = strings.TrimPrefix(name, topDir)
		}
		if name == "" {
			continue
		}

		// 安全检查：防止路径穿越
		target := filepath.Join(dest, name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dest)+string(os.PathSeparator)) {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0755)
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			io.Copy(f, tarReader)
			f.Close()
		case tar.TypeSymlink:
			// 跳过符号链接（安全考虑）
			continue
		}
	}
	return nil
}

// HasGit 检查系统是否安装了 git
func HasGit() bool {
	_, err := exec.LookPath("git")
	return err == nil
}
