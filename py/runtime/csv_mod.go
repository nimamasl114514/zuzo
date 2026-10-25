package runtime

// csv_mod.go — csv / _csv 的 Go 简版实现（第14轮）
//
// 触发点：第三方库的 import 链上有 `import csv`（CPython 的 csv.py 首行依赖 C 模块 _csv）。
// zuzo 不需要完整 CSV 语义（requests/urllib3 不直接使用），只要「模块可导入 + 基本读写可用」。
// 因此这里同时注册 `csv` 与 `_csv` 两个名字，避免 csv.py 再被拉起。

import (
	"strings"

	"zuzo/py/object"
)

func init() {
	builtinModuleInits["csv"] = modCSV
	builtinModuleInits["_csv"] = modCSV
}

func modCSV(ip *Interp) *object.Module {
	m := newModule("csv")
	m.Dict["QUOTE_MINIMAL"] = &object.Int{V: 0}
	m.Dict["QUOTE_ALL"] = &object.Int{V: 1}
	m.Dict["QUOTE_NONNUMERIC"] = &object.Int{V: 2}
	m.Dict["QUOTE_NONE"] = &object.Int{V: 3}
	m.Dict["QUOTE_STRINGS"] = &object.Int{V: 4}
	m.Dict["QUOTE_NOTNULL"] = &object.Int{V: 5}

	errCls := &object.TypeObj{Name: "Error", Bases: []*object.TypeObj{ip.excType("Exception")}, IsExc: true, Dict: map[string]object.Object{}}
	m.Dict["Error"] = errCls

	// reader：把可迭代的行按逗号切分（支持双引号包裹）
	m.Dict["reader"] = bi("reader", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		var rows []object.Object
		if len(a) > 0 {
			for _, line := range ip.ToSlice(a[0]) {
				s := strings.TrimRight(line.Str(), "\r\n")
				fields := splitCSVLine(s)
				items := make([]object.Object, len(fields))
				for i, f := range fields {
					items[i] = &object.Str{V: f}
				}
				rows = append(rows, &object.List{Items: items})
			}
		}
		return &object.Iterator{Items: rows}, nil
	})

	writeRow := func(dst *object.Instance, row object.Object) {
		var sb strings.Builder
		for i, cell := range ip.ToSlice(row) {
			if i > 0 {
				sb.WriteString(",")
			}
			s := cell.Str()
			if strings.ContainsAny(s, ",\"\n\r") {
				sb.WriteString("\"")
				sb.WriteString(strings.ReplaceAll(s, "\"", "\"\""))
				sb.WriteString("\"")
			} else {
				sb.WriteString(s)
			}
		}
		sb.WriteString("\r\n")
		// 写到底层文件对象（支持 .write）
		if w, ok := dst.Dict["_f"]; ok {
			fn := ip.GetAttr(w, "write")
			ip.Call(fn, []object.Object{&object.Str{V: sb.String()}}, nil)
		}
	}

	writerCls := &object.TypeObj{Name: "writer", Dict: map[string]object.Object{}}
	writerCls.Dict["writerow"] = bi("writerow", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			return object.None, nil
		}
		if dst, ok := a[0].(*object.Instance); ok {
			writeRow(dst, a[1])
		}
		return object.None, nil
	})
	writerCls.Dict["writerows"] = bi("writerows", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			return object.None, nil
		}
		if dst, ok := a[0].(*object.Instance); ok {
			for _, row := range ip.ToSlice(a[1]) {
				writeRow(dst, row)
			}
		}
		return object.None, nil
	})
	m.Dict["writer"] = bi("writer", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := &object.Instance{Class: writerCls, Dict: map[string]object.Object{}}
		if len(a) > 0 {
			inst.Dict["_f"] = a[0]
		}
		return inst, nil
	})

	// DictReader / DictWriter：最小实现（按表头映射）
	drCls := &object.TypeObj{Name: "DictReader", Dict: map[string]object.Object{}}
	m.Dict["DictReader"] = drCls
	dwCls := &object.TypeObj{Name: "DictWriter", Dict: map[string]object.Object{}}
	dwCls.Dict["writerow"] = bi("writerow", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	m.Dict["DictWriter"] = dwCls
	m.Dict["register_dialect"] = bi("register_dialect", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	m.Dict["unregister_dialect"] = m.Dict["register_dialect"]
	m.Dict["get_dialect"] = bi("get_dialect", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Instance{Class: &object.TypeObj{Name: "Dialect", Dict: map[string]object.Object{}}, Dict: map[string]object.Object{}}, nil
	})
	m.Dict["list_dialects"] = bi("list_dialects", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.List{Items: []object.Object{&object.Str{V: "excel"}, &object.Str{V: "excel-tab"}, &object.Str{V: "unix"}}}, nil
	})
	m.Dict["field_size_limit"] = bi("field_size_limit", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Int{V: 131072}, nil
	})
	m.Dict["Sniffer"] = &object.TypeObj{Name: "Sniffer", Dict: map[string]object.Object{}}
	m.Dict["Dialect"] = &object.TypeObj{Name: "Dialect", Dict: map[string]object.Object{}}
	m.Dict["excel"] = &object.Str{V: "excel"}
	m.Dict["excel_tab"] = &object.Str{V: "excel-tab"}
	m.Dict["unix_dialect"] = &object.Str{V: "unix"}
	return m
}

// splitCSVLine 最简 CSV 行切分（支持双引号与转义双引号）
func splitCSVLine(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			if inQuote && i+1 < len(s) && s[i+1] == '"' {
				cur.WriteByte('"')
				i++
			} else {
				inQuote = !inQuote
			}
		case c == ',' && !inQuote:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	out = append(out, cur.String())
	return out
}
