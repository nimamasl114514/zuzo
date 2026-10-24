package zpm

// installer.go — pip 风格依赖安装器
//
// 核心流程:
//
//	zpm install 网络请求
//	  → Client.ResolveVersion("网络请求", "")
//	  → 如果在线: 下载 tar.gz → 解压到 zpm_modules/
//	  → 如果离线或注册表无: 回退到本地注册表 → Git 克隆
//	  → 记录到 zpm.lock
//
// 安装目录: ./zpm_modules/<包名>/

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Installer pip 风格安装器
type Installer struct {
	projectDir string
	modulesDir string
	client     *Client
	ghClient   *GitHubClient
	lockfile   *Lockfile
	verbose    bool
}

// NewInstaller 创建安装器
func NewInstaller(projectDir string, verbose bool) (*Installer, error) {
	modulesDir := filepath.Join(projectDir, ModulesDir)

	lockfile, err := LoadLockfile(projectDir)
	if err != nil {
		return nil, fmt.Errorf("加载锁文件失败: %w", err)
	}

	return &Installer{
		projectDir: projectDir,
		modulesDir: modulesDir,
		client:     NewClient(),
		ghClient:   NewGitHubClient(),
		lockfile:   lockfile,
		verbose:    verbose,
	}, nil
}

// InstallResult 安装结果
type InstallResult struct {
	Name    string
	Version string
	Source  string // "registry" | "git" | "local"
	Skipped bool
}

// InstallReq 安装单个 Requirement（pip 风格）
func (ins *Installer) InstallReq(req *Requirement) (*InstallResult, error) {
	switch req.Source {
	case "local":
		return ins.installLocal(req)
	case "git":
		return ins.installGit(req)
	case "github":
		return ins.installFromGitHub(req)
	case "zpackage":
		return ins.installFromZPackage(req)
	default:
		return ins.installFromRegistry(req)
	}
}

// InstallAll 安装 依赖.txt 中的所有依赖
func (ins *Installer) InstallAll(reqs *Requirements) ([]InstallResult, error) {
	var results []InstallResult

	for _, req := range reqs.Items {
		result, err := ins.InstallReq(req)
		if err != nil {
			return results, fmt.Errorf("安装 %s 失败: %w", req.Name, err)
		}
		results = append(results, *result)
	}

	// 保存锁文件
	if err := ins.lockfile.Save(ins.projectDir); err != nil {
		return results, fmt.Errorf("保存锁文件失败: %w", err)
	}

	return results, nil
}

// installFromRegistry 从中央注册表安装
func (ins *Installer) installFromRegistry(req *Requirement) (*InstallResult, error) {
	name := req.Name
	versionSpec := req.VersionSpec
	targetDir := filepath.Join(ins.modulesDir, name)

	// 检查锁文件：如果已锁定且文件存在，跳过
	if locked, ok := ins.lockfile.Get(name); ok {
		if _, err := os.Stat(targetDir); err == nil {
			// 检查版本是否满足
			if versionSpec == "" || locked.Version == versionSpec ||
				strings.HasPrefix(versionSpec, "==") && locked.Version == strings.TrimSpace(strings.TrimPrefix(versionSpec, "==")) {
				ins.log("  ✓ %s==%s (已满足, 跳过)", name, locked.Version)
				return &InstallResult{
					Name: name, Version: locked.Version, Source: "registry", Skipped: true,
				}, nil
			}
		}
	}

	// 解析版本
	version, info, err := ins.client.ResolveVersion(name, versionSpec)
	if err != nil {
		return nil, err
	}

	ins.log("  ⬇ %s==%s", name, version)

	// 优先尝试在线下载
	if ins.client.IsOnline() && info.DownloadURL != "" {
		if err := ins.downloadAndExtract(name, version, targetDir); err == nil {
			ins.log("  ✓ %s==%s (已下载)", name, version)
			ins.lockfile.Set(&LockEntry{
				Name: name, Version: version, Spec: req.VersionSpec,
				Source: "registry", Resolved: info.DownloadURL,
			})
			return &InstallResult{Name: name, Version: version, Source: "registry"}, nil
		}
		// 下载失败，回退到 Git
	}

	// 回退: 如果注册表条目有 Git 源
	if info.GitURL != "" {
		gitReq := &Requirement{
			Name:      name,
			Source:    "git",
			SourceURL: info.GitURL,
		}
		// 如果有版本，附加为 Git ref
		if version != "" {
			gitReq.SourceURL = info.GitURL + "@" + version
		}
		return ins.installGit(gitReq)
	}

	return nil, fmt.Errorf("无法安装 %s: 注册表无可用下载源", name)
}

// downloadAndExtract 下载并解压包
func (ins *Installer) downloadAndExtract(name, version, targetDir string) error {
	data, err := ins.client.DownloadPackage(name, version)
	if err != nil {
		return err
	}

	// 清理旧目录
	os.RemoveAll(targetDir)
	os.MkdirAll(ins.modulesDir, 0755)

	// 解压 tar.gz
	return extractTarGz(data, targetDir)
}

// installFromGitHub 从 GitHub 仓库安装
// 优先使用 GitHub Archive API 下载（无需 git），失败时回退到 git clone
func (ins *Installer) installFromGitHub(req *Requirement) (*InstallResult, error) {
	name := req.Name
	targetDir := filepath.Join(ins.modulesDir, name)

	// 解析 SourceURL (owner/repo) 和 ref
	fullName := req.SourceURL // e.g. "owner/repo"
	ref := ""
	if req.VersionSpec != "" {
		// VersionSpec 格式: "@v1.0.0" 或 "@main"
		ref = strings.TrimPrefix(req.VersionSpec, "@")
	}

	// 构建锁文件 key
	lockKey := fullName
	if ref != "" {
		lockKey += "@" + ref
	}

	// 检查锁文件：如果已锁定且文件存在，跳过
	if locked, ok := ins.lockfile.Get(name); ok {
		if locked.Resolved == lockKey {
			if _, err := os.Stat(targetDir); err == nil {
				ins.log("  ✓ %s (已安装, 跳过)", name)
				return &InstallResult{
					Name: name, Version: ref, Source: "github", Skipped: true,
				}, nil
			}
		}
	}

	// 构建 GitHubRepo
	repo := &GitHubRepo{
		Owner:  strings.Split(fullName, "/")[0],
		Name:   strings.Split(fullName, "/")[1],
		Ref:    ref,
		Subdir: req.Subdir,
	}

	ins.log("  ⬇ %s @ %s (GitHub Archive)", fullName, ref)

	// 尝试 1: GitHub Archive 下载（无需 git）
	data, err := ins.ghClient.DownloadArchive(repo)
	if err == nil {
		// 清理旧目录
		os.RemoveAll(targetDir)
		os.MkdirAll(ins.modulesDir, 0755)

		// 解压
		if err := ExtractGitHubArchive(data, targetDir); err != nil {
			ins.log("  ⚠ Archive 解压失败: %v, 尝试 git clone...", err)
		} else {
			// 如果有子目录，只保留子目录内容
			if req.Subdir != "" {
				subdirPath := filepath.Join(targetDir, req.Subdir)
				if _, err := os.Stat(subdirPath); err == nil {
					// 将子目录内容移到顶层
					tmpMove := targetDir + ".move"
					os.Rename(subdirPath, tmpMove)
					os.RemoveAll(targetDir)
					os.Rename(tmpMove, targetDir)
				}
			}

			ins.log("  ✓ %s @ %s (已下载)", name, ref)

			archiveURL := repo.ArchiveURL()
			ins.lockfile.Set(&LockEntry{
				Name:     name,
				Version:  ref,
				Spec:     req.Raw,
				Source:   "github",
				Resolved: lockKey,
				Hash:     archiveURL,
			})

			return &InstallResult{
				Name:    name,
				Version: ref,
				Source:  "github",
			}, nil
		}
	} else {
		ins.log("  ⚠ GitHub Archive 下载失败: %v", err)
	}

	// 尝试 2: git clone 回退
	if HasGit() {
		ins.log("  ⬇ 回退到 git clone %s...", repo.CloneURL())
		gitReq := &Requirement{
			Name:      name,
			Source:    "git",
			SourceURL: repo.CloneURL(),
			Raw:       req.Raw,
		}
		if ref != "" {
			gitReq.SourceURL += "@" + ref
		}
		return ins.installGit(gitReq)
	}

	return nil, fmt.Errorf("无法安装 %s: GitHub Archive 下载失败且系统未安装 git", name)
}

// installGit 从 Git 仓库安装
func (ins *Installer) installGit(req *Requirement) (*InstallResult, error) {
	name := req.Name
	targetDir := filepath.Join(ins.modulesDir, name)

	// 解析 URL 和 ref
	gitURL := req.SourceURL
	ref := ""
	if idx := strings.LastIndex(gitURL, "@"); idx > 0 && !strings.HasPrefix(gitURL, "git@") {
		gitURL = gitURL[:idx]
		ref = req.SourceURL[idx+1:]
	}
	if ref == "" {
		ref = "HEAD"
	}

	// 检查锁文件
	if locked, ok := ins.lockfile.Get(name); ok {
		if locked.Resolved == gitURL+"@"+ref {
			if _, err := os.Stat(targetDir); err == nil {
				ins.log("  ✓ %s (已安装, 跳过)", name)
				return &InstallResult{
					Name: name, Version: ref, Source: "git", Skipped: true,
				}, nil
			}
		}
	}

	os.RemoveAll(targetDir)
	tmpDir := targetDir + ".tmp"
	os.RemoveAll(tmpDir)

	// git clone
	args := []string{"clone", "--depth", "1"}
	if ref != "HEAD" {
		args = append(args, "--branch", ref)
	}
	args = append(args, gitURL, tmpDir)

	ins.log("  ⬇ 克隆 %s (%s)...", gitURL, ref)

	cmd := exec.Command("git", args...)
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		cmd2 := exec.Command("git", "clone", gitURL, tmpDir)
		if err := cmd2.Run(); err != nil {
			return nil, fmt.Errorf("git clone 失败: %w", err)
		}
		if ref != "HEAD" {
			exec.Command("git", "-C", tmpDir, "checkout", ref).Run()
		}
	}

	os.MkdirAll(ins.modulesDir, 0755)
	if err := copyDir(tmpDir, targetDir); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("安装 %s 失败: %w", name, err)
	}
	os.RemoveAll(tmpDir)
	os.RemoveAll(filepath.Join(targetDir, ".git"))

	ins.log("  ✓ %s @ %s", name, ref)

	ins.lockfile.Set(&LockEntry{
		Name: name, Version: ref, Spec: req.Raw,
		Source: "git", Resolved: gitURL + "@" + ref,
	})

	return &InstallResult{Name: name, Version: ref, Source: "git"}, nil
}

// installFromZPackage 从 .z 包文件安装
func (ins *Installer) installFromZPackage(req *Requirement) (*InstallResult, error) {
	zFilePath := req.SourceURL
	if !filepath.IsAbs(zFilePath) {
		zFilePath = filepath.Join(ins.projectDir, zFilePath)
	}
	zFilePath = filepath.Clean(zFilePath)

	// 验证文件存在
	if _, err := os.Stat(zFilePath); err != nil {
		return nil, fmt.Errorf(".z 包文件不存在: %s", zFilePath)
	}

	// 验证包完整性
	if err := VerifyZPackage(zFilePath); err != nil {
		return nil, fmt.Errorf(".z 包验证失败: %w", err)
	}

	// 读取存根获取包名
	manifest, err := ReadZManifest(zFilePath)
	if err != nil {
		return nil, fmt.Errorf("读取 .z 包存根失败: %w", err)
	}

	name := manifest.Name
	if req.Name != "" {
		name = req.Name
	}
	version := manifest.Version
	targetDir := filepath.Join(ins.modulesDir, name)

	ins.log("  ⬇ 安装 .z 包: %s (%s)", name, version)

	// 清理旧目录
	os.RemoveAll(targetDir)
	os.MkdirAll(ins.modulesDir, 0755)

	// 解包到目标目录
	_, err = Unpack(zFilePath, targetDir)
	if err != nil {
		return nil, fmt.Errorf("解包 %s 失败: %w", zFilePath, err)
	}

	// 将 code/ 目录内容提升到顶层（使 .hy 文件直接在 zpm_modules/<包名>/ 下可用）
	codeDir := filepath.Join(targetDir, CodeDir)
	if _, err := os.Stat(codeDir); err == nil {
		// 将 code/ 下的文件移到顶层
		entries, _ := os.ReadDir(codeDir)
		for _, entry := range entries {
			srcPath := filepath.Join(codeDir, entry.Name())
			dstPath := filepath.Join(targetDir, entry.Name())
			os.Rename(srcPath, dstPath)
		}
		os.RemoveAll(codeDir)
	}

	// 清理 META-INF 目录（运行时不需要存根）
	// metaDir := filepath.Join(targetDir, MetaDir)
	// 保留存根用于版本检查

	ins.log("  ✓ %s (%s) [from .z]", name, version)

	// 记录到锁文件
	ins.lockfile.Set(&LockEntry{
		Name:     name,
		Version:  version,
		Spec:     req.Raw,
		Source:   "zpackage",
		Resolved: zFilePath,
	})

	return &InstallResult{
		Name:    name,
		Version: version,
		Source:  "zpackage",
	}, nil
}

// installLocal 从本地路径安装
func (ins *Installer) installLocal(req *Requirement) (*InstallResult, error) {
	name := req.Name
	sourcePath := req.SourceURL
	if !filepath.IsAbs(sourcePath) {
		sourcePath = filepath.Join(ins.projectDir, sourcePath)
	}
	sourcePath = filepath.Clean(sourcePath)
	targetDir := filepath.Join(ins.modulesDir, name)

	if _, err := os.Stat(sourcePath); err != nil {
		return nil, fmt.Errorf("本地依赖路径不存在: %s", sourcePath)
	}

	// 检查已有链接
	if info, err := os.Lstat(targetDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if existing, _ := os.Readlink(targetDir); existing == sourcePath {
				ins.log("  ✓ %s (已链接, 跳过)", name)
				return &InstallResult{Name: name, Source: "local", Skipped: true}, nil
			}
		}
		os.RemoveAll(targetDir)
	}

	if err := os.Symlink(sourcePath, targetDir); err != nil {
		if err := copyDir(sourcePath, targetDir); err != nil {
			return nil, fmt.Errorf("无法安装 %s: %w", name, err)
		}
		ins.log("  ✓ %s (已复制 from %s)", name, sourcePath)
	} else {
		ins.log("  ✓ %s (已链接 → %s)", name, sourcePath)
	}

	ins.lockfile.Set(&LockEntry{
		Name: name, Spec: req.Raw, Source: "local", Resolved: sourcePath,
	})

	return &InstallResult{Name: name, Source: "local"}, nil
}

// Uninstall 卸载包
func (ins *Installer) Uninstall(name string) error {
	targetDir := filepath.Join(ins.modulesDir, name)
	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("卸载 %s 失败: %w", name, err)
	}
	ins.lockfile.Remove(name)
	return nil
}

// SaveLockfile 保存锁文件到项目目录
func (ins *Installer) SaveLockfile(dir string) error {
	return ins.lockfile.Save(dir)
}

// ListInstalled 列出已安装的包
func (ins *Installer) ListInstalled() map[string]string {
	installed := make(map[string]string)
	entries, err := os.ReadDir(ins.modulesDir)
	if err != nil {
		return installed
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != ".gitkeep" {
			name := entry.Name()
			version := ""
			if locked, ok := ins.lockfile.Get(name); ok {
				version = locked.Version
			}
			installed[name] = version
		}
	}
	return installed
}

// log 详细日志
func (ins *Installer) log(format string, args ...any) {
	if ins.verbose {
		fmt.Printf(format+"\n", args...)
	}
}

// ==================== 辅助函数 ====================

// extractTarGz 解压 tar.gz 到目标目录
func extractTarGz(data []byte, dest string) error {
	gzReader, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("解压 gzip 失败: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	os.MkdirAll(dest, 0755)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取 tar 失败: %w", err)
		}

		// 安全检查：防止路径穿越
		target := filepath.Join(dest, header.Name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dest)+string(os.PathSeparator)) {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0755)
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			io.Copy(f, tarReader)
			f.Close()
		}
	}
	return nil
}

// copyDir 递归复制目录
func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("源路径不是目录: %s", src)
	}

	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if entry.Name() == ".git" {
				continue
			}
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}
