package tests

// diagnostic_pkg_test.go — diagnostic 包测试（从 diagnostic/ 迁移）

import (
	"strings"
	"testing"

	"zuzo/diagnostic"
)

func TestReportEmpty(t *testing.T) {
	r := diagnostic.New()
	if r.HasErrors() {
		t.Error("空报告不应有错误")
	}
	if r.Count() != 0 {
		t.Errorf("空报告 Count() 应为 0, 得到 %d", r.Count())
	}
	if r.Render() != "" {
		t.Errorf("空报告 Render() 应为空字符串")
	}
}

func TestReportRootOnly(t *testing.T) {
	r := diagnostic.New()
	r.AddRoot("E001", 3, 10, "缺少右括号", "请补全 ')'")

	if !r.HasErrors() {
		t.Error("应有错误")
	}
	if r.Count() != 1 {
		t.Errorf("Count 应为 1, 得到 %d", r.Count())
	}

	rendered := r.Render()
	if !strings.Contains(rendered, "根因") {
		t.Error("应包含 '根因'")
	}
	if !strings.Contains(rendered, "E001") {
		t.Error("应包含 'E001'")
	}
	if !strings.Contains(rendered, "缺少右括号") {
		t.Error("应包含错误消息")
	}
	if !strings.Contains(rendered, "请补全") {
		t.Error("应包含建议")
	}
	if !strings.Contains(rendered, "行 3") {
		t.Error("应包含行号")
	}
}

func TestReportCausalChain(t *testing.T) {
	r := diagnostic.New()
	root := r.AddRoot("E001", 1, 5, "缺少右括号", "补全 ')'")
	r.AddCascade(root, "E102", 2, 1, "因条件未闭合，解析偏移", "")
	r.AddType(root, "E203", 3, 10, "变量 a 类型推断失效", "先修复根本原因")

	if r.Count() != 3 {
		t.Errorf("Count 应为 3, 得到 %d", r.Count())
	}

	rendered := r.Render()
	// 应包含所有三个级别
	if !strings.Contains(rendered, "根因") {
		t.Error("应包含 '根因'")
	}
	if !strings.Contains(rendered, "连带") {
		t.Error("应包含 '连带'")
	}
	if !strings.Contains(rendered, "类型") {
		t.Error("应包含 '类型'")
	}
	// 应包含因果链提示
	if !strings.Contains(rendered, "因:") {
		t.Error("应包含 '因:' 提示")
	}
}

func TestReportMultipleRoots(t *testing.T) {
	r := diagnostic.New()
	r.AddRoot("E001", 1, 0, "第一个错误", "")
	r.AddRoot("E002", 5, 0, "第二个错误", "")

	if r.Count() != 2 {
		t.Errorf("Count 应为 2, 得到 %d", r.Count())
	}

	rendered := r.Render()
	// 两个根因都应出现
	if !strings.Contains(rendered, "第一个错误") {
		t.Error("应包含第一个错误")
	}
	if !strings.Contains(rendered, "第二个错误") {
		t.Error("应包含第二个错误")
	}
}

func TestFindRootOnLine(t *testing.T) {
	r := diagnostic.New()
	r.AddRoot("E001", 3, 0, "行3错误", "")

	if found := r.FindRootOnLine(3); found == nil {
		t.Error("应在行3找到根因")
	}
	if found := r.FindRootOnLine(5); found != nil {
		t.Error("不应在行5找到根因")
	}
}

func TestReportBoxFormat(t *testing.T) {
	r := diagnostic.New()
	r.AddRoot("E001", 1, 1, "测试错误", "测试建议")

	rendered := r.Render()
	// v2 格式：使用 ━━━ 作为标题边框
	if !strings.Contains(rendered, "━━━") {
		t.Error("应包含标题边框 '━━━'")
	}
	if !strings.Contains(rendered, "发现") {
		t.Error("应包含 '发现' 标题")
	}
	if !strings.Contains(rendered, "问题") {
		t.Error("应包含 '问题' 字样")
	}
}

func TestSimpleReport(t *testing.T) {
	r := diagnostic.SimpleReport([]string{"错误1", "错误2"})
	if r.Count() != 2 {
		t.Errorf("Count 应为 2, 得到 %d", r.Count())
	}
	rendered := r.Render()
	if !strings.Contains(rendered, "错误1") {
		t.Error("应包含 '错误1'")
	}
}

func TestCausalChainCollect(t *testing.T) {
	r := diagnostic.New()
	root := r.AddRoot("E001", 1, 0, "根因", "")
	c1 := r.AddCascade(root, "E102", 2, 0, "连带1", "")
	r.AddCascade(c1, "E103", 3, 0, "连带2", "") // c1 的下游
	r.AddType(root, "E203", 4, 0, "类型后果", "")

	// 收集 root 的整条链
	chain := r.CollectCausalChain(root)
	if len(chain) != 4 {
		t.Errorf("因果链应有 4 个活字, 得到 %d", len(chain))
	}
	// 第一个应是根因
	if chain[0] != root {
		t.Error("链首应是根因")
	}
}
