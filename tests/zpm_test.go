package tests

// zpm_test.go — 包管理器测试（从 zpm/ 迁移）

import (
	"os"
	"path/filepath"
	"testing"

	"zuzo/zpm"
)

// ==================== Requirements 测试 ====================

func TestParseRequirements(t *testing.T) {
	data := `# 注释
网络请求
json工具==1.2.0
日期库>=1.0.0,<2.0.0
测试框架~=1.0.0

git+github.com/zuzo/dev@v1.0.0
-e ./libs/mylib
`
	reqs, err := zpm.ParseRequirements(data)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	if len(reqs.Items) != 6 {
		t.Fatalf("条目数: 期望 6, 得到 %d", len(reqs.Items))
	}

	// 网络请求 (无版本)
	r0 := reqs.Items[0]
	if r0.Name != "网络请求" {
		t.Errorf("Item[0] 名称: 期望 网络请求, 得到 %s", r0.Name)
	}
	if r0.VersionSpec != "" {
		t.Errorf("Item[0] 版本: 期望空, 得到 %s", r0.VersionSpec)
	}
	if r0.Source != "registry" {
		t.Errorf("Item[0] 来源: 期望 registry, 得到 %s", r0.Source)
	}

	// json工具==1.2.0
	r1 := reqs.Items[1]
	if r1.Name != "json工具" {
		t.Errorf("Item[1] 名称: 期望 json工具, 得到 %s", r1.Name)
	}
	if r1.VersionSpec != "==1.2.0" {
		t.Errorf("Item[1] 版本: 期望 ==1.2.0, 得到 %s", r1.VersionSpec)
	}

	// 日期库>=1.0.0,<2.0.0
	r2 := reqs.Items[2]
	if r2.Name != "日期库" {
		t.Errorf("Item[2] 名称: 期望 日期库, 得到 %s", r2.Name)
	}
	if r2.VersionSpec != ">=1.0.0,<2.0.0" {
		t.Errorf("Item[2] 版本: 期望 >=1.0.0,<2.0.0, 得到 %s", r2.VersionSpec)
	}

	// git+github.com/zuzo/dev@v1.0.0
	r4 := reqs.Items[4]
	if r4.Source != "git" {
		t.Errorf("Item[4] 来源: 期望 git, 得到 %s", r4.Source)
	}
	if r4.Name != "dev" {
		t.Errorf("Item[4] 名称: 期望 dev, 得到 %s", r4.Name)
	}

	// -e ./libs/mylib
	r5 := reqs.Items[5]
	if r5.Source != "local" {
		t.Errorf("Item[5] 来源: 期望 local, 得到 %s", r5.Source)
	}
	if r5.Name != "mylib" {
		t.Errorf("Item[5] 名称: 期望 mylib, 得到 %s", r5.Name)
	}
}

func TestParseRequirementLinePipStyle(t *testing.T) {
	tests := []struct {
		input   string
		name    string
		version string
		source  string
	}{
		{"网络请求", "网络请求", "", "registry"},
		{"json工具==1.2.0", "json工具", "==1.2.0", "registry"},
		{"json工具>=1.0.0", "json工具", ">=1.0.0", "registry"},
		{"json工具~=1.2.0", "json工具", "~=1.2.0", "registry"},
		{"json工具!=1.5.0", "json工具", "!=1.5.0", "registry"},
		{"json工具>1.0.0", "json工具", ">1.0.0", "registry"},
		{"json工具<2.0.0", "json工具", "<2.0.0", "registry"},
	}

	for _, tt := range tests {
		req, err := zpm.ParseRequirementLine(tt.input)
		if err != nil {
			t.Errorf("解析 %q 失败: %v", tt.input, err)
			continue
		}
		if req.Name != tt.name {
			t.Errorf("%q: 名称 期望 %s, 得到 %s", tt.input, tt.name, req.Name)
		}
		if req.VersionSpec != tt.version {
			t.Errorf("%q: 版本 期望 %s, 得到 %s", tt.input, tt.version, req.VersionSpec)
		}
		if req.Source != tt.source {
			t.Errorf("%q: 来源 期望 %s, 得到 %s", tt.input, tt.source, req.Source)
		}
	}
}

func TestParseRequirementLineGit(t *testing.T) {
	req, err := zpm.ParseRequirementLine("git+github.com/zuzo/utils@v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if req.Source != "git" {
		t.Errorf("来源: 期望 git, 得到 %s", req.Source)
	}
	if req.Name != "utils" {
		t.Errorf("名称: 期望 utils, 得到 %s", req.Name)
	}
}

func TestParseRequirementLineLocal(t *testing.T) {
	req, err := zpm.ParseRequirementLine("-e ./libs/mylib")
	if err != nil {
		t.Fatal(err)
	}
	if req.Source != "local" {
		t.Errorf("来源: 期望 local, 得到 %s", req.Source)
	}
	if req.Name != "mylib" {
		t.Errorf("名称: 期望 mylib, 得到 %s", req.Name)
	}
}

func TestRequirementsAddRemove(t *testing.T) {
	reqs := &zpm.Requirements{}

	reqs.Add(&zpm.Requirement{Name: "pkg1", Source: "registry"})
	reqs.Add(&zpm.Requirement{Name: "pkg2", Source: "registry"})

	if len(reqs.Items) != 2 {
		t.Errorf("条目数: 期望 2, 得到 %d", len(reqs.Items))
	}

	// 更新已有
	reqs.Add(&zpm.Requirement{Name: "pkg1", VersionSpec: "==2.0.0", Source: "registry"})
	if len(reqs.Items) != 2 {
		t.Errorf("更新后条目数: 期望 2, 得到 %d", len(reqs.Items))
	}
	found, _ := reqs.Find("pkg1")
	if found.VersionSpec != "==2.0.0" {
		t.Errorf("版本: 期望 ==2.0.0, 得到 %s", found.VersionSpec)
	}

	// 移除
	reqs.Remove("pkg1")
	if len(reqs.Items) != 1 {
		t.Errorf("移除后条目数: 期望 1, 得到 %d", len(reqs.Items))
	}
}

func TestSaveLoadRequirements(t *testing.T) {
	dir := t.TempDir()

	reqs := &zpm.Requirements{
		Items: []*zpm.Requirement{
			{Name: "网络请求", Source: "registry"},
			{Name: "json工具", VersionSpec: "==1.2.0", Source: "registry"},
		},
	}

	if err := zpm.SaveRequirements(dir, reqs); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	// 验证文件存在
	if _, err := os.Stat(filepath.Join(dir, zpm.RequirementsFile)); err != nil {
		t.Fatalf("文件未创建: %v", err)
	}

	// 重新加载
	reqs2, err := zpm.LoadRequirements(dir)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if len(reqs2.Items) != 2 {
		t.Fatalf("加载后条目数: 期望 2, 得到 %d", len(reqs2.Items))
	}
}

func TestFreeze(t *testing.T) {
	reqs := &zpm.Requirements{
		Items: []*zpm.Requirement{
			{Name: "网络请求", Source: "registry"},
			{Name: "json工具", Source: "registry"},
		},
	}

	installed := map[string]string{
		"网络请求":   "1.5.0",
		"json工具": "1.2.0",
	}

	output := reqs.Freeze(installed)

	if !containsStr(output, "json工具==1.2.0") {
		t.Errorf("freeze 输出应包含 json工具==1.2.0, 得到:\n%s", output)
	}
	if !containsStr(output, "网络请求==1.5.0") {
		t.Errorf("freeze 输出应包含 网络请求==1.5.0, 得到:\n%s", output)
	}
}

// ==================== Spec 测试 ====================

func TestParseSpecRegistry(t *testing.T) {
	s, err := zpm.ParseSpec("1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if !s.IsRegistry() {
		t.Error("应为注册表类型")
	}
	if s.Version != "1.2.0" {
		t.Errorf("版本: 期望 1.2.0, 得到 %s", s.Version)
	}
}

func TestParseSpecGit(t *testing.T) {
	s, err := zpm.ParseSpec("github.com/user/repo@v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if !s.IsGit() {
		t.Error("应为 Git 类型")
	}
	if s.GitURL != "github.com/user/repo" {
		t.Errorf("URL: 期望 github.com/user/repo, 得到 %s", s.GitURL)
	}
	if s.GitRef != "v1.0.0" {
		t.Errorf("Ref: 期望 v1.0.0, 得到 %s", s.GitRef)
	}
}

func TestParseSpecLocal(t *testing.T) {
	s, err := zpm.ParseSpec("file:../libs/mylib")
	if err != nil {
		t.Fatal(err)
	}
	if !s.IsLocal() {
		t.Error("应为本地类型")
	}
}

// ==================== Lockfile 测试 ====================

func TestLockfileSetGet(t *testing.T) {
	lf := zpm.NewLockfile()

	lf.Set(&zpm.LockEntry{
		Name:     "test",
		Version:  "1.0.0",
		Source:   "registry",
		Resolved: "https://registry.zuzo.dev/packages/test/1.0.0",
	})

	entry, ok := lf.Get("test")
	if !ok {
		t.Fatal("应能找到已设置的条目")
	}
	if entry.Version != "1.0.0" {
		t.Errorf("版本: 期望 1.0.0, 得到 %s", entry.Version)
	}

	if !lf.IsLocked("test") {
		t.Error("IsLocked 应返回 true")
	}

	lf.Remove("test")
	if lf.IsLocked("test") {
		t.Error("移除后 IsLocked 应返回 false")
	}
}

func TestLockfileSaveLoad(t *testing.T) {
	dir := t.TempDir()

	lf := zpm.NewLockfile()
	lf.Set(&zpm.LockEntry{
		Name:     "pkg1",
		Version:  "1.0.0",
		Source:   "registry",
		Resolved: "https://registry.zuzo.dev/packages/pkg1/1.0.0",
	})

	if err := lf.Save(dir); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	lf2, err := zpm.LoadLockfile(dir)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	entry, ok := lf2.Get("pkg1")
	if !ok {
		t.Fatal("加载后应能找到 pkg1")
	}
	if entry.Version != "1.0.0" {
		t.Errorf("版本: 期望 1.0.0, 得到 %s", entry.Version)
	}
}

// ==================== Registry 测试 ====================

func TestRegistryAddSearch(t *testing.T) {
	r := &zpm.Registry{
		Packages: make(map[string]*zpm.RegistryEntry),
	}

	r.AddOrUpdate(&zpm.RegistryEntry{
		Name:     "json工具",
		Versions: []string{"1.0.0", "1.2.0"},
		Latest:   "1.2.0",
		Source:   "github.com/zuzo/json_utils",
	})
	r.AddOrUpdate(&zpm.RegistryEntry{
		Name:     "网络请求",
		Versions: []string{"0.9.0"},
		Latest:   "0.9.0",
		Source:   "github.com/zuzo/net",
	})

	results := r.Search("json")
	if len(results) != 1 {
		t.Errorf("搜索 json: 期望 1 个结果, 得到 %d", len(results))
	}

	results = r.Search("不存在")
	if len(results) != 0 {
		t.Errorf("搜索不存在: 期望 0 个结果, 得到 %d", len(results))
	}
}

// ==================== GitHub 测试 ====================

func TestParseGitHubRepoShorthand(t *testing.T) {
	tests := []struct {
		input  string
		owner  string
		name   string
		ref    string
		subdir string
	}{
		{"owner/repo", "owner", "repo", "", ""},
		{"owner/repo@v1.0.0", "owner", "repo", "v1.0.0", ""},
		{"owner/repo#main", "owner", "repo", "main", ""},
		{"owner/repo/subdir", "owner", "repo", "", "subdir"},
		{"owner/repo/subdir@v2.0.0", "owner", "repo", "v2.0.0", "subdir"},
		{"github.com/owner/repo", "owner", "repo", "", ""},
		{"github.com/owner/repo@v1.0.0", "owner", "repo", "v1.0.0", ""},
		{"https://github.com/owner/repo", "owner", "repo", "", ""},
		{"https://github.com/owner/repo.git", "owner", "repo", "", ""},
		{"gh:owner/repo@v1.0.0", "owner", "repo", "v1.0.0", ""},
		{"gh:owner/repo/sub@v3.0.0", "owner", "repo", "v3.0.0", "sub"},
	}

	for _, tt := range tests {
		repo, ok := zpm.ParseGitHubRepo(tt.input)
		if !ok {
			t.Errorf("解析 %q 失败", tt.input)
			continue
		}
		if repo.Owner != tt.owner {
			t.Errorf("%q: Owner 期望 %s, 得到 %s", tt.input, tt.owner, repo.Owner)
		}
		if repo.Name != tt.name {
			t.Errorf("%q: Name 期望 %s, 得到 %s", tt.input, tt.name, repo.Name)
		}
		if repo.Ref != tt.ref {
			t.Errorf("%q: Ref 期望 %s, 得到 %s", tt.input, tt.ref, repo.Ref)
		}
		if repo.Subdir != tt.subdir {
			t.Errorf("%q: Subdir 期望 %s, 得到 %s", tt.input, tt.subdir, repo.Subdir)
		}
	}
}

func TestParseGitHubRepoInvalid(t *testing.T) {
	invalid := []string{
		"",
		"justaname",
		"./local/path",
		"file:../lib",
		"-e ./path",
		"git+github.com/owner/repo",
	}
	for _, input := range invalid {
		_, ok := zpm.ParseGitHubRepo(input)
		if ok {
			t.Errorf("%q 不应被解析为 GitHub 仓库", input)
		}
	}
}

func TestIsGitHubShorthand(t *testing.T) {
	valid := []string{
		"owner/repo",
		"zuzo-lang/utils",
		"user123/my-repo",
	}
	for _, s := range valid {
		if !zpm.IsGitHubShorthand(s) {
			t.Errorf("%q 应为 GitHub 简写", s)
		}
	}

	invalid := []string{
		"./local/path",
		"../parent",
		"file:path",
		"git+url",
		"github.com/owner/repo",
		"https://github.com/owner/repo",
		"gh:owner/repo",
		"包名",
	}
	for _, s := range invalid {
		if zpm.IsGitHubShorthand(s) {
			t.Errorf("%q 不应为 GitHub 简写", s)
		}
	}
}

func TestGitHubArchiveURL(t *testing.T) {
	tests := []struct {
		owner string
		name  string
		ref   string
		want  string
	}{
		{"owner", "repo", "", "https://github.com/owner/repo/archive/HEAD.tar.gz"},
		{"owner", "repo", "v1.0.0", "https://github.com/owner/repo/archive/refs/tags/v1.0.0.tar.gz"},
		{"owner", "repo", "main", "https://github.com/owner/repo/archive/refs/heads/main.tar.gz"},
		{"owner", "repo", "1.2.0", "https://github.com/owner/repo/archive/refs/tags/1.2.0.tar.gz"},
	}

	for _, tt := range tests {
		repo := &zpm.GitHubRepo{Owner: tt.owner, Name: tt.name, Ref: tt.ref}
		got := repo.ArchiveURL()
		if got != tt.want {
			t.Errorf("ArchiveURL(owner=%s, name=%s, ref=%s): 期望 %s, 得到 %s", tt.owner, tt.name, tt.ref, tt.want, got)
		}
	}
}

func TestParseRequirementLineGitHubShorthand(t *testing.T) {
	// owner/repo → GitHub 安装
	req, err := zpm.ParseRequirementLine("zuzo-lang/utils")
	if err != nil {
		t.Fatal(err)
	}
	if req.Source != "github" {
		t.Errorf("来源: 期望 github, 得到 %s", req.Source)
	}
	if req.Name != "utils" {
		t.Errorf("名称: 期望 utils, 得到 %s", req.Name)
	}
	if req.SourceURL != "zuzo-lang/utils" {
		t.Errorf("SourceURL: 期望 zuzo-lang/utils, 得到 %s", req.SourceURL)
	}
}

func TestParseRequirementLineGitHubWithTag(t *testing.T) {
	// owner/repo@v1.0.0 → GitHub + 标签
	req, err := zpm.ParseRequirementLine("zuzo-lang/utils@v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if req.Source != "github" {
		t.Errorf("来源: 期望 github, 得到 %s", req.Source)
	}
	if req.Name != "utils" {
		t.Errorf("名称: 期望 utils, 得到 %s", req.Name)
	}
	if req.VersionSpec != "@v1.0.0" {
		t.Errorf("版本规格: 期望 @v1.0.0, 得到 %s", req.VersionSpec)
	}
}

func TestParseRequirementLineGitHubFullURL(t *testing.T) {
	// github.com/owner/repo@v1.0.0 → GitHub
	req, err := zpm.ParseRequirementLine("github.com/zuzo-lang/utils@v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if req.Source != "github" {
		t.Errorf("来源: 期望 github, 得到 %s", req.Source)
	}
	if req.Name != "utils" {
		t.Errorf("名称: 期望 utils, 得到 %s", req.Name)
	}
}

func TestParseRequirementLineGitHubGhPrefix(t *testing.T) {
	// gh:owner/repo@v1.0.0 → GitHub
	req, err := zpm.ParseRequirementLine("gh:zuzo-lang/utils@v2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if req.Source != "github" {
		t.Errorf("来源: 期望 github, 得到 %s", req.Source)
	}
	if req.Name != "utils" {
		t.Errorf("名称: 期望 utils, 得到 %s", req.Name)
	}
	if req.VersionSpec != "@v2.0.0" {
		t.Errorf("版本规格: 期望 @v2.0.0, 得到 %s", req.VersionSpec)
	}
}

func TestParseRequirementLineGitHubSubdir(t *testing.T) {
	// owner/repo/subdir@v1.0.0 → GitHub + 子目录
	req, err := zpm.ParseRequirementLine("zuzo-lang/monorepo/pkg@v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if req.Source != "github" {
		t.Errorf("来源: 期望 github, 得到 %s", req.Source)
	}
	if req.Name != "pkg" {
		t.Errorf("名称: 期望 pkg, 得到 %s", req.Name)
	}
	if req.Subdir != "pkg" {
		t.Errorf("子目录: 期望 pkg, 得到 %s", req.Subdir)
	}
}

func TestSaveLoadRequirementsWithGitHub(t *testing.T) {
	dir := t.TempDir()

	reqs := &zpm.Requirements{
		Items: []*zpm.Requirement{
			{Name: "网络请求", Source: "registry"},
			{Name: "utils", Source: "github", SourceURL: "zuzo-lang/utils", VersionSpec: "@v1.0.0"},
		},
	}

	if err := zpm.SaveRequirements(dir, reqs); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	// 重新加载
	reqs2, err := zpm.LoadRequirements(dir)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if len(reqs2.Items) != 2 {
		t.Fatalf("加载后条目数: 期望 2, 得到 %d", len(reqs2.Items))
	}

	// 找到 GitHub 条目
	var ghReq *zpm.Requirement
	for _, item := range reqs2.Items {
		if item.Source == "github" {
			ghReq = item
			break
		}
	}
	if ghReq == nil {
		t.Fatal("未找到 github 来源的依赖")
	}
	if ghReq.Name != "utils" {
		t.Errorf("名称: 期望 utils, 得到 %s", ghReq.Name)
	}
	if ghReq.SourceURL != "zuzo-lang/utils" {
		t.Errorf("SourceURL: 期望 zuzo-lang/utils, 得到 %s", ghReq.SourceURL)
	}
}

func TestFreezeWithGitHub(t *testing.T) {
	reqs := &zpm.Requirements{
		Items: []*zpm.Requirement{
			{Name: "网络请求", Source: "registry"},
			{Name: "utils", Source: "github", SourceURL: "zuzo-lang/utils", VersionSpec: "@v1.0.0"},
		},
	}

	installed := map[string]string{
		"网络请求":  "1.5.0",
		"utils": "v1.0.0",
	}

	output := reqs.Freeze(installed)

	if !containsStr(output, "utils==v1.0.0") {
		t.Errorf("freeze 输出应包含 utils==v1.0.0, 得到:\n%s", output)
	}
}

// ==================== 辅助函数 ====================

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ==================== .z 包打包/解包测试 ====================

func TestZPackagePackUnpack(t *testing.T) {
	// 创建临时项目目录
	tmpDir := t.TempDir()

	// 创建 zpm.toml
	manifest := `[包]
名称 = "测试库"
版本 = "1.2.0"
描述 = "测试用包"
作者 = "zpm-test"

[依赖]
"网络请求" = "1.0.0"
`
	os.WriteFile(filepath.Join(tmpDir, "zpm.toml"), []byte(manifest), 0644)

	// 创建源文件
	os.MkdirAll(filepath.Join(tmpDir, "lib"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "main.hy"), []byte(`// 主入口
函数 主() {
  打印("Hello")
}
`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "lib", "utils.hy"), []byte(`// 工具函数
函数 辅助(x) {
  返回 x + 1
}
`), 0644)

	// 创建资源文件
	os.MkdirAll(filepath.Join(tmpDir, "resources", "data"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "resources", "data", "config.json"), []byte(`{"key":"value"}`), 0644)

	// 打包
	outputPath, err := zpm.Pack(tmpDir, zpm.PackOptions{IncludeReadme: false})
	if err != nil {
		t.Fatalf("打包失败: %v", err)
	}

	// 验证文件存在
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("输出文件不存在: %v", err)
	}

	// 验证文件名
	if !zpm.IsZPackage(outputPath) {
		t.Errorf("输出文件不是 .z 包: %s", outputPath)
	}

	// 验证包完整性
	if err := zpm.VerifyZPackage(outputPath); err != nil {
		t.Fatalf("包验证失败: %v", err)
	}

	// 读取存根
	m, err := zpm.ReadZManifest(outputPath)
	if err != nil {
		t.Fatalf("读取存根失败: %v", err)
	}
	if m.Name != "测试库" {
		t.Errorf("包名: 期望 测试库, 得到 %s", m.Name)
	}
	if m.Version != "1.2.0" {
		t.Errorf("版本: 期望 1.2.0, 得到 %s", m.Version)
	}

	// 解包
	unpackDir := t.TempDir()
	pkg, err := zpm.Unpack(outputPath, unpackDir)
	if err != nil {
		t.Fatalf("解包失败: %v", err)
	}

	if pkg.Manifest == nil {
		t.Fatal("解包后 Manifest 为 nil")
	}
	if pkg.Manifest.Name != "测试库" {
		t.Errorf("解包后包名: 期望 测试库, 得到 %s", pkg.Manifest.Name)
	}

	// 验证代码文件
	if len(pkg.Files) < 2 {
		t.Errorf("代码文件数: 期望 >= 2, 得到 %d", len(pkg.Files))
	}

	// 验证 main.hy 存在
	mainFound := false
	utilsFound := false
	for path := range pkg.Files {
		if path == "main.hy" {
			mainFound = true
		}
		if path == "lib/utils.hy" {
			utilsFound = true
		}
	}
	if !mainFound {
		t.Error("解包后未找到 main.hy")
	}
	if !utilsFound {
		t.Error("解包后未找到 lib/utils.hy")
	}

	// 验证资源文件
	if len(pkg.Resources) < 1 {
		t.Errorf("资源文件数: 期望 >= 1, 得到 %d", len(pkg.Resources))
	}
}

func TestZPackageListContents(t *testing.T) {
	tmpDir := t.TempDir()

	// 创建最小项目
	os.WriteFile(filepath.Join(tmpDir, "zpm.toml"), []byte(`[包]
名称 = "小库"
版本 = "0.1.0"
`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.hy"), []byte(`打印("hi")`), 0644)

	// 打包
	outputPath, err := zpm.Pack(tmpDir, zpm.PackOptions{})
	if err != nil {
		t.Fatalf("打包失败: %v", err)
	}

	// 列出内容
	entries, err := zpm.ListZContents(outputPath)
	if err != nil {
		t.Fatalf("列出内容失败: %v", err)
	}

	// 应包含: MANIFEST.toml, EXPORTS.zstub, DEPS.toml, code/main.hy
	hasManifest := false
	hasExports := false
	hasDeps := false
	hasCode := false
	for _, e := range entries {
		if e == zpm.ManifestStub {
			hasManifest = true
		}
		if e == zpm.ExportsStub {
			hasExports = true
		}
		if e == zpm.DepsStub {
			hasDeps = true
		}
		if e == zpm.CodeDir+"/main.hy" {
			hasCode = true
		}
	}

	if !hasManifest {
		t.Error("未找到 MANIFEST.toml 存根")
	}
	if !hasExports {
		t.Error("未找到 EXPORTS.zstub 存根")
	}
	if !hasDeps {
		t.Error("未找到 DEPS.toml 存根")
	}
	if !hasCode {
		t.Error("未找到 code/main.hy")
	}
}

func TestZPackageVerify(t *testing.T) {
	// 正常的包
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "zpm.toml"), []byte(`[包]
名称 = "验证库"
版本 = "1.0.0"
`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.hy"), []byte(`打印("ok")`), 0644)

	outputPath, _ := zpm.Pack(tmpDir, zpm.PackOptions{})

	// 验证通过
	if err := zpm.VerifyZPackage(outputPath); err != nil {
		t.Errorf("验证正常包应通过, 但失败: %v", err)
	}

	// 验证不存在的文件
	if err := zpm.VerifyZPackage("/nonexistent/file.z"); err == nil {
		t.Error("验证不存在的文件应失败")
	}
}

func TestParseZPackageRequirement(t *testing.T) {
	tests := []struct {
		input    string
		wantSrc  string
		wantName string
	}{
		{"mylib-1.0.0.z", "zpackage", "mylib-1.0.0"},
		{"./libs/utils.z", "zpackage", "utils"},
		{"-e ./libs/mylib.z", "zpackage", "mylib"},
	}

	for _, tt := range tests {
		req, err := zpm.ParseRequirementLine(tt.input)
		if err != nil {
			t.Errorf("解析 %q 失败: %v", tt.input, err)
			continue
		}
		if req.Source != tt.wantSrc {
			t.Errorf("解析 %q: Source 期望 %s, 得到 %s", tt.input, tt.wantSrc, req.Source)
		}
		if req.Name != tt.wantName {
			t.Errorf("解析 %q: Name 期望 %s, 得到 %s", tt.input, tt.wantName, req.Name)
		}
	}
}

func TestIsZPackage(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"mylib-1.0.0.z", true},
		{"./libs/utils.z", true},
		{"MYLIB.Z", true},
		{"mylib.hy", false},
		{"mylib.tar.gz", false},
		{"mylib", false},
	}

	for _, tt := range tests {
		if got := zpm.IsZPackage(tt.path); got != tt.want {
			t.Errorf("IsZPackage(%q) = %v, 期望 %v", tt.path, got, tt.want)
		}
	}
}

func TestSerializeExports(t *testing.T) {
	exports := &zpm.ExportTable{
		Functions: []zpm.ExportFunc{
			{Name: "加法", Params: "(a, b)", ReturnType: "整数", File: "main.hy", Line: 5},
		},
		Variables: []zpm.ExportVar{
			{Name: "版本号", Type: "字符串", File: "main.hy"},
		},
		Modules: []zpm.ExportModule{
			{Name: "工具", File: "lib/utils.hy"},
		},
	}

	data := zpm.SerializeExports(exports)

	// 验证序列化结果
	if !containsStr(data, "加法") {
		t.Error("序列化结果应包含函数名 '加法'")
	}
	if !containsStr(data, "版本号") {
		t.Error("序列化结果应包含变量名 '版本号'")
	}
	if !containsStr(data, "工具") {
		t.Error("序列化结果应包含模块名 '工具'")
	}
	if !containsStr(data, "[函数]") {
		t.Error("序列化结果应包含 [函数] 段")
	}
	if !containsStr(data, "[变量]") {
		t.Error("序列化结果应包含 [变量] 段")
	}
	if !containsStr(data, "[模块]") {
		t.Error("序列化结果应包含 [模块] 段")
	}

	// 解析回来
	parsed := zpm.ParseExports(data)
	if len(parsed.Functions) != 1 {
		t.Errorf("解析后函数数: 期望 1, 得到 %d", len(parsed.Functions))
	}
	if parsed.Functions[0].Name != "加法" {
		t.Errorf("解析后函数名: 期望 加法, 得到 %s", parsed.Functions[0].Name)
	}
	if len(parsed.Variables) != 1 {
		t.Errorf("解析后变量数: 期望 1, 得到 %d", len(parsed.Variables))
	}
	if len(parsed.Modules) != 1 {
		t.Errorf("解析后模块数: 期望 1, 得到 %d", len(parsed.Modules))
	}
}

func TestScanExports(t *testing.T) {
	source := `// 测试源码
变量 计数 = 0
常量 版本 = "1.0"

函数 加法(a, b) -> 整数 {
  返回 a + b
}

函数 打印信息() {
  打印("hello")
}
`
	exports := &zpm.ExportTable{}
	zpm.ScanExports(source, "test.hy", exports)

	// 应扫描到 2 个函数
	if len(exports.Functions) != 2 {
		t.Errorf("函数数: 期望 2, 得到 %d", len(exports.Functions))
	}

	// 第一个函数应该是 加法
	if len(exports.Functions) > 0 && exports.Functions[0].Name != "加法" {
		t.Errorf("第一个函数名: 期望 加法, 得到 %s", exports.Functions[0].Name)
	}

	// 应扫描到 2 个变量
	if len(exports.Variables) != 2 {
		t.Errorf("变量数: 期望 2, 得到 %d", len(exports.Variables))
	}
}

func TestPackToBytes(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "zpm.toml"), []byte(`[包]
名称 = "字节包"
版本 = "2.0.0"
`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.hy"), []byte(`打印("bytes")`), 0644)

	data, err := zpm.PackToBytes(tmpDir)
	if err != nil {
		t.Fatalf("PackToBytes 失败: %v", err)
	}

	// 验证返回的字节流非空且为有效 zip
	if len(data) < 100 {
		t.Errorf("字节数据过小: %d 字节", len(data))
	}

	// zip 文件以 PK 开头
	if len(data) < 2 || data[0] != 'P' || data[1] != 'K' {
		t.Error("字节数据不是有效的 zip 格式")
	}
}
