package nativecc

import (
	"encoding/binary"
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	peImageBase    = 0x140000000
	peSectionAlign = 0x1000
	peFileAlign    = 0x200
)

func alignTo(v, a int) int {
	if a <= 1 {
		return v
	}
	return (v + a - 1) / a * a
}

func putU32(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }
func putU16(b []byte, v uint16) { binary.LittleEndian.PutUint16(b, v) }
func putU64(b []byte, v uint64) { binary.LittleEndian.PutUint64(b, v) }

type dllLayout struct {
	name     string
	funcs    []string
	nameOff  int
	hintOffs []int
	iltOff   int
	iatOff   int
	descOff  int
}

// WritePE 把编译产物写成 PE32+ 可执行文件
func WritePE(c *Compiled, outPath string) error {
	// ── 1. 按 DLL 分组导入 ──
	importsByDLL := map[string][]string{}
	for src, sym := range c.Imports {
		dll := strings.ToLower(sym.DLL)
		importsByDLL[dll] = append(importsByDLL[dll], src)
	}
	var dlls []string
	for d := range importsByDLL {
		dlls = append(dlls, d)
	}
	sort.Strings(dlls)
	for _, d := range dlls {
		sort.Strings(importsByDLL[d])
	}

	// ── 2. 布局导入表（追加到 .rdata）──
	rdata := append([]byte{}, c.RData...)
	rdataLabels := map[string]int{}
	for k, v := range c.RDataLabels {
		rdataLabels[k] = v
	}

	var layouts []*dllLayout
	for _, d := range dlls {
		l := &dllLayout{name: d, funcs: importsByDLL[d]}
		for _, f := range l.funcs {
			exp := c.Imports[f].Export
			if exp == "" {
				exp = f
			}
			l.hintOffs = append(l.hintOffs, len(rdata))
			rdata = append(rdata, 0, 0) // Hint
			rdata = append(rdata, []byte(exp)...)
			rdata = append(rdata, 0)
			if len(rdata)%2 != 0 {
				rdata = append(rdata, 0)
			}
		}
		l.iltOff = len(rdata)
		rdata = append(rdata, make([]byte, (len(l.funcs)+1)*8)...)
		l.iatOff = len(rdata)
		rdata = append(rdata, make([]byte, (len(l.funcs)+1)*8)...)
		for i, f := range l.funcs {
			rdataLabels["__imp_"+f] = l.iatOff + i*8
		}
		l.nameOff = len(rdata)
		rdata = append(rdata, []byte(l.name)...)
		rdata = append(rdata, 0)
		layouts = append(layouts, l)
	}
	descOff := len(rdata)
	rdata = append(rdata, make([]byte, (len(layouts)+1)*20)...)
	for i, l := range layouts {
		l.descOff = descOff + i*20
	}
	// 对齐到 16
	for len(rdata)%16 != 0 {
		rdata = append(rdata, 0)
	}

	// ── 3. 计算 RVA ──
	textSize := len(c.Text)
	if textSize == 0 {
		textSize = 16
	}
	rvaText := peSectionAlign
	rvaRData := rvaText + alignTo(textSize, peSectionAlign)
	rvaData := rvaRData + alignTo(len(rdata), peSectionAlign)

	// ── 4. 回填导入表内容 ──
	for _, l := range layouts {
		for i := range l.funcs {
			hnRVA := uint64(rvaRData + l.hintOffs[i])
			putU64(rdata[l.iltOff+i*8:], hnRVA)
			putU64(rdata[l.iatOff+i*8:], hnRVA)
		}
		d := rdata[l.descOff : l.descOff+20]
		putU32(d[0:], uint32(rvaRData+l.iltOff))
		putU32(d[4:], 0)
		putU32(d[8:], 0)
		putU32(d[12:], uint32(rvaRData+l.nameOff))
		putU32(d[16:], uint32(rvaRData+l.iatOff))
	}

	// ── 5. patch .text ──
	resolve := func(label string) (int, bool) {
		if off, ok := c.TextLabels[label]; ok {
			return rvaText + off, true
		}
		if off, ok := rdataLabels[label]; ok {
			return rvaRData + off, true
		}
		if off, ok := c.DataLabels[label]; ok {
			return rvaData + off, true
		}
		return 0, false
	}
	if c.Asm != nil {
		for _, f := range c.Asm.fixups {
			tgt, ok := resolve(f.Label)
			if !ok {
				return fmt.Errorf("未定义的标签: %s", f.Label)
			}
			next := rvaText + f.Pos + 4
			rel := int32(tgt + f.Add - next)
			binary.LittleEndian.PutUint32(c.Text[f.Pos:], uint32(rel))
		}
	}

	// ── 6. .data 中指向 .rdata 的指针重定位 ──
	for _, r := range c.PtrRelocs {
		if r.DataOff+8 <= len(c.Data) {
			putU64(c.Data[r.DataOff:], uint64(rvaRData+r.RDataOff))
		}
	}

	// ── 7. 组装文件 ──
	dataSeg := c.Data
	if len(dataSeg) == 0 {
		dataSeg = []byte{0}
	}
	textRaw := alignTo(textSize, peFileAlign)
	rdataRaw := alignTo(len(rdata), peFileAlign)
	dataRaw := alignTo(len(dataSeg), peFileAlign)
	if dataRaw == 0 {
		dataRaw = peFileAlign
	}

	sectionCount := 3
	optSize := 240
	headerSize := 0x40 + 4 + 20 + optSize + sectionCount*40
	sizeOfHeaders := alignTo(headerSize, peFileAlign)

	rawText := sizeOfHeaders
	rawRData := rawText + textRaw
	rawData := rawRData + rdataRaw

	sizeOfImage := alignTo(rvaData+len(dataSeg), peSectionAlign)
	if sizeOfImage <= rvaData {
		sizeOfImage = rvaData + peSectionAlign
	}

	hdr := make([]byte, sizeOfHeaders)

	// DOS header
	hdr[0] = 'M'
	hdr[1] = 'Z'
	putU32(hdr[0x3c:], 0x40)

	// PE signature
	copy(hdr[0x40:], []byte{'P', 'E', 0, 0})

	// COFF header @0x44
	coff := hdr[0x44:]
	putU16(coff[0:], 0x8664) // Machine AMD64
	putU16(coff[2:], uint16(sectionCount))
	putU32(coff[4:], 0)
	putU32(coff[8:], 0)
	putU32(coff[12:], 0)
	putU16(coff[16:], uint16(optSize))
	putU16(coff[18:], 0x0022) // EXECUTABLE_IMAGE | LARGE_ADDRESS_AWARE

	// Optional header @0x58
	opt := hdr[0x58:]
	putU16(opt[0:], 0x20B) // PE32+
	opt[2] = 1             // linker version
	opt[3] = 0
	putU32(opt[4:], uint32(textRaw))                 // SizeOfCode
	putU32(opt[8:], uint32(rdataRaw+dataRaw))        // SizeOfInitializedData
	putU32(opt[0x0c:], 0)                            // SizeOfUninitializedData
	entryRVA := uint32(rvaText)
	if off, ok := c.TextLabels["L_entry"]; ok {
		entryRVA = uint32(rvaText + off)
	}
	putU32(opt[0x10:], entryRVA)
	putU32(opt[0x14:], uint32(rvaText)) // BaseOfCode
	putU64(opt[0x18:], peImageBase)
	putU32(opt[0x20:], peSectionAlign)
	putU32(opt[0x24:], peFileAlign)
	putU16(opt[0x28:], 6) // OS version
	putU16(opt[0x2a:], 0)
	putU16(opt[0x2c:], 0)
	putU16(opt[0x2e:], 0)
	putU16(opt[0x30:], 6) // subsystem version
	putU16(opt[0x32:], 0)
	putU32(opt[0x34:], 0)
	putU32(opt[0x38:], uint32(sizeOfImage))
	putU32(opt[0x3c:], uint32(sizeOfHeaders))
	putU32(opt[0x40:], 0) // checksum
	putU16(opt[0x44:], 3) // CONSOLE
	putU16(opt[0x46:], 0) // 无 ASLR / CFG
	putU64(opt[0x48:], 0x1000000)
	putU64(opt[0x50:], 0x1000)
	putU64(opt[0x58:], 0x100000)
	putU64(opt[0x60:], 0x1000)
	putU32(opt[0x68:], 0)
	putU32(opt[0x6c:], 16)

	// Data directories @ opt+0x70
	dd := opt[0x70:]
	if len(layouts) > 0 {
		putU32(dd[1*8:], uint32(rvaRData+descOff))
		putU32(dd[1*8+4:], uint32(len(layouts)*20))
		// IAT 目录（第 12 项）
		firstIAT := rvaRData + layouts[0].iatOff
		totalIAT := 0
		for _, l := range layouts {
			totalIAT += (len(l.funcs) + 1) * 8
		}
		putU32(dd[12*8:], uint32(firstIAT))
		putU32(dd[12*8+4:], uint32(totalIAT))
	}

	// Section headers @ 0x58+240 = 0x148
	sec := hdr[0x58+optSize:]
	writeSection := func(idx int, name string, vsize, vaddr, rawsize, rawptr int, chars uint32) {
		s := sec[idx*40:]
		copy(s[0:8], name)
		putU32(s[8:], uint32(vsize))
		putU32(s[12:], uint32(vaddr))
		putU32(s[16:], uint32(rawsize))
		putU32(s[20:], uint32(rawptr))
		putU32(s[24:], 0)
		putU32(s[28:], 0)
		putU16(s[32:], 0)
		putU16(s[34:], 0)
		putU32(s[36:], chars)
	}
	writeSection(0, ".text", textSize, rvaText, textRaw, rawText, 0x60000020)
	// .rdata 同时承载导入表 IAT：加载器要在 LdrProcessInitializationComplete 阶段写 IAT，
// 段必须可写；否则在字符串/导入表跨过 0x1000 边界后写 IAT 会访问只读页崩溃。
	writeSection(1, ".rdata", len(rdata), rvaRData, rdataRaw, rawRData, 0xC0000040)
	writeSection(2, ".data", len(dataSeg), rvaData, dataRaw, rawData, 0xC0000040)

	// ── 8. 写出 ──
	out := make([]byte, rawData+dataRaw)
	copy(out, hdr)
	copy(out[rawText:], c.Text)
	copy(out[rawRData:], rdata)
	copy(out[rawData:], dataSeg)

	return os.WriteFile(outPath, out, 0o755)
}
