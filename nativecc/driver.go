package nativecc

import (
	"fmt"
	"os"
	"path/filepath"
)

// Source 一个待编译的源文件
type Source struct {
	Name string // 逻辑文件名（用于 #include 相对解析与错误信息）
	Code string
}

// Options 编译选项
type Options struct {
	EmitAsm  bool   // 保留反汇编/中间产物（调试用）
	Verbose  bool   // 打印阶段信息
	KeepTemp bool   // 保留临时文件
	OutDir   string // 中间产物目录
}

// ParseSources 预处理并解析多个源文件，合并为单一翻译单元
func ParseSources(sources []Source) (*TranslationUnit, error) {
	merged := &TranslationUnit{}
	for _, s := range sources {
		pp := NewPreprocessor(BuiltinHeaders, nil)
		if err := pp.ProcessFile(s.Name, s.Code); err != nil {
			return nil, fmt.Errorf("[%s] 预处理失败: %w", s.Name, err)
		}
		toks := pp.Tokens()
		p := NewParser(toks, s.Name)
		tu, err := p.Parse()
		if err != nil {
			return nil, fmt.Errorf("[%s] 语法分析失败: %w", s.Name, err)
		}
		merged.Decls = append(merged.Decls, tu.Decls...)
	}
	return merged, nil
}

// DumpAST 调试：打印 AST 概要
func DumpAST(tu *TranslationUnit) string {
	out := ""
	for _, d := range tu.Decls {
		switch v := d.(type) {
		case *FuncDecl:
			params := ""
			for _, q := range v.Params {
				params += q.Ty.String() + " " + q.Name + ", "
			}
			body := "声明"
			if v.Body != nil {
				body = fmt.Sprintf("定义 %d 语句", len(v.Body.Stmts))
			}
			out += fmt.Sprintf("函数 %s(%s) -> %s [%s]\n", v.Name, params, v.Ty.Ret.String(), body)
		case *VarDecl:
			out += fmt.Sprintf("变量 %s : %s\n", v.Name, v.Ty.String())
		case *TypedefDecl:
			out += fmt.Sprintf("typedef %s = %s\n", v.Name, v.Ty.String())
		case *RecordDecl:
			if v.Ty.Tag != "" {
				out += fmt.Sprintf("%s %s { %d 字段, size=%d align=%d }\n", v.Ty.String(), v.Ty.Tag, len(v.Ty.Fields), v.Ty.Size, v.Ty.Align)
			}
		}
	}
	return out
}

// CompileToExe 编译多个源文件为 PE 可执行文件
func CompileToExe(sources []Source, outExe string, opts Options) error {
	tu, err := ParseSources(sources)
	if err != nil {
		return err
	}
	comp, err := Compile(tu)
	if err != nil {
		return err
	}
	if opts.Verbose {
		os.Stderr.WriteString(fmt.Sprintf("[nativecc] .text=%d .rdata=%d .data=%d imports=%d\n",
			len(comp.Text), len(comp.RData), len(comp.Data), len(comp.Imports)))
	}
	return WritePE(comp, outExe)
}

// LoadSource 从磁盘读源文件
func LoadSource(path string) (Source, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Source{}, err
	}
	abs, _ := filepath.Abs(path)
	return Source{Name: abs, Code: string(data)}, nil
}
