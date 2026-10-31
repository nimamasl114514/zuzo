package nativecc

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRDataSectionWritable 守门第27轮定位的加载器崩溃：
// .rdata 同时承载导入表 IAT，加载器在 LdrProcessInitializationComplete 阶段要写 IAT；
// 若该段标记为只读（0x40000040），当字符串/导入表把 IAT 推过 0x1000 页边界时会访问违例
// （实测：examples/sort.hy 产物崩在 ntdll 里，RDX = .rdata + 0xFFA）。
// 判据：.rdata 段 Characteristics 必须含 IMAGE_SCN_MEM_WRITE（0x80000000）。
func TestRDataSectionWritable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "seg.exe")
	src := "#include <stdio.h>\nint main(int argc, char** argv) { printf(\"x\"); return 0; }\n"
	if err := CompileToExe([]Source{{Name: "t.c", Code: src}}, exe, Options{Verbose: true}); err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	if len(data) < 0x200 {
		t.Fatalf("产物过小: %d", len(data))
	}
	lfanew := int(binary.LittleEndian.Uint32(data[0x3c:]))
	coff := lfanew + 4
	nsec := int(binary.LittleEndian.Uint16(data[coff+2:]))
	optSize := int(binary.LittleEndian.Uint16(data[coff+16:]))
	secBase := coff + 20 + optSize
	for i := 0; i < nsec; i++ {
		s := secBase + i*40
		name := strings.TrimRight(string(data[s:s+8]), "\x00")
		chars := binary.LittleEndian.Uint32(data[s+36:])
		if name != ".rdata" {
			continue
		}
		if chars&0x80000000 == 0 {
			t.Fatalf(".rdata 段不可写（Characteristics=0x%08x）：加载器写 IAT 会访问只读页", chars)
		}
		return
	}
	t.Fatal("未找到 .rdata 段")
}

// TestManyStringConstants 覆盖「字符串常量多 + 打印」这条真实路径能跑完。
func TestManyStringConstants(t *testing.T) {
	var b strings.Builder
	b.WriteString("#include <stdio.h>\n")
	b.WriteString("int main(int argc, char** argv) {\n")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "    printf(\"str%03d\");\n", i)
	}
	b.WriteString("    return 0;\n}\n")
	buildAndRun(t, b.String(), 0)
}
