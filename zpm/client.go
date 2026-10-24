package zpm

// client.go — 中央注册表 HTTP 客户端
//
// 模拟 pip 的 PyPI 交互方式：
//
//	zpm install 网络请求          → 从中央注册表查询 → 下载 → 安装
//	zpm search 关键字            → 搜索中央注册表
//	zpm show 包名               → 查看包详情
//
// 注册表 API (兼容模式，可离线回退到本地注册表):
//
//	GET /packages/<name>           → 包信息 JSON
//	GET /packages/<name>/<version> → 下载包 (tar.gz)
//	GET /search?q=<keyword>        → 搜索结果 JSON
//
// 默认注册表地址可通过环境变量 ZPM_REGISTRY 配置

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultRegistryURL 默认中央注册表地址
const DefaultRegistryURL = "https://registry.zuzo.dev"

// RegistryURL 获取注册表地址（优先环境变量）
func RegistryURL() string {
	if url := os.Getenv("ZPM_REGISTRY"); url != "" {
		return strings.TrimRight(url, "/")
	}
	return DefaultRegistryURL
}

// PackageInfo 注册表返回的包信息
type PackageInfo struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Versions    []string `json:"versions"`
	Latest      string   `json:"latest"`
	DownloadURL string   `json:"download_url"` // 直接下载地址
	GitURL      string   `json:"git_url"`      // Git 克隆地址（回退方案）
}

// SearchResult 搜索结果条目
type SearchResult struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// Client 注册表 HTTP 客户端
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient 创建注册表客户端
func NewClient() *Client {
	return &Client{
		baseURL: RegistryURL(),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetPackage 从注册表查询包信息
func (c *Client) GetPackage(name string) (*PackageInfo, error) {
	url := fmt.Sprintf("%s/packages/%s", c.baseURL, name)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("无法连接注册表: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("注册表中未找到包: %s", name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("注册表返回错误: HTTP %d", resp.StatusCode)
	}

	var info PackageInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("解析注册表响应失败: %w", err)
	}
	return &info, nil
}

// DownloadPackage 下载指定版本的包，返回字节内容
func (c *Client) DownloadPackage(name, version string) ([]byte, error) {
	url := fmt.Sprintf("%s/packages/%s/%s", c.baseURL, name, version)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// Search 搜索注册表
func (c *Client) Search(keyword string) ([]SearchResult, error) {
	url := fmt.Sprintf("%s/search?q=%s", c.baseURL, keyword)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("搜索失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("搜索失败: HTTP %d", resp.StatusCode)
	}

	var results []SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("解析搜索结果失败: %w", err)
	}
	return results, nil
}

// IsOnline 检查注册表是否可访问
func (c *Client) IsOnline() bool {
	resp, err := c.httpClient.Get(c.baseURL + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ResolveVersion 解析版本规格，返回实际应安装的版本
// 如果注册表不可达，回退到本地注册表
func (c *Client) ResolveVersion(name, versionSpec string) (string, *PackageInfo, error) {
	// 尝试在线注册表
	info, err := c.GetPackage(name)
	if err == nil {
		// 在线成功，解析版本
		version := resolveVersionFromInfo(info, versionSpec)
		return version, info, nil
	}

	// 回退到本地注册表
	localReg, localErr := LoadRegistry()
	if localErr == nil {
		if entry, ok := localReg.Lookup(name); ok {
			info := &PackageInfo{
				Name:        entry.Name,
				Latest:      entry.Latest,
				Versions:    entry.Versions,
				Description: entry.Description,
				GitURL:      entry.Source,
			}
			version := resolveVersionFromInfo(info, versionSpec)
			return version, info, nil
		}
	}

	return "", nil, fmt.Errorf("无法解析包 %s: %w", name, err)
}

// resolveVersionFromInfo 根据版本规格从包信息中选择版本
func resolveVersionFromInfo(info *PackageInfo, versionSpec string) string {
	// 无规格 → 最新版本
	if versionSpec == "" || versionSpec == "*" {
		return info.Latest
	}

	// == 精确版本
	if strings.HasPrefix(versionSpec, "==") {
		want := strings.TrimSpace(strings.TrimPrefix(versionSpec, "=="))
		for _, v := range info.Versions {
			if v == want {
				return v
			}
		}
		// 精确版本不在列表中，仍返回它（可能是下载地址）
		return want
	}

	// >=, <=, >, <, ~= 范围匹配 → 选最新满足的
	if len(info.Versions) > 0 {
		return info.Latest // 简化：范围匹配返回最新
	}

	return info.Latest
}
