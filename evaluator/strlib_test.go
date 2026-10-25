package evaluator

// strlib_test.go — 字符串标准库（`文本` 命名空间）单元测试
//
// 覆盖口径：每个方法至少一条正常路径 + 关键方法补边界/错误路径；
// 语义基准 = CPython str 方法集 + Go strings 独有项 + 中文友好扩展。
// 直接调用 BuiltinFunction（绕过解析器），另有 TestStringLibNamespace 做端到端注入验证。

import (
	"strings"
	"testing"

	"zuzo/object"
)

// ---------- 助手 ----------

func o(v any) object.Object {
	switch x := v.(type) {
	case string:
		return &object.String{Value: x}
	case int:
		return &object.Integer{Value: int64(x)}
	case int64:
		return &object.Integer{Value: x}
	case bool:
		return nativeBoolToBooleanObject(x)
	case []string:
		return strArray(x)
	case []object.Object:
		return &object.Array{Elements: x}
	}
	panic("unsupported test arg type")
}

func os_(ss ...string) []object.Object {
	out := make([]object.Object, len(ss))
	for i, s := range ss {
		out[i] = &object.String{Value: s}
	}
	return out
}

type strCase struct {
	name string
	fn   object.BuiltinFunction
	args []object.Object
	want any // string / int64 / bool
}

func runStrCases(t *testing.T, cases []strCase) {
	t.Helper()
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := c.fn(c.args...)
			if e, ok := got.(*object.Error); ok {
				t.Fatalf("unexpected error: %s", e.Message)
			}
			switch want := c.want.(type) {
			case string:
				gs, ok := got.(*object.String)
				if !ok {
					t.Fatalf("want string, got %s", got.Type())
				}
				if gs.Value != want {
					t.Fatalf("want %q, got %q", want, gs.Value)
				}
			case int64:
				gi, ok := got.(*object.Integer)
				if !ok {
					t.Fatalf("want int, got %s", got.Type())
				}
				if gi.Value != want {
					t.Fatalf("want %d, got %d", want, gi.Value)
				}
			case bool:
				gb, ok := got.(*object.Boolean)
				if !ok {
					t.Fatalf("want bool, got %s", got.Type())
				}
				if gb.Value != want {
					t.Fatalf("want %v, got %v", want, gb.Value)
				}
			default:
				t.Fatalf("unsupported want type %T", c.want)
			}
		})
	}
}

// ---------- 大小写 ----------

func TestStrLibCase(t *testing.T) {
	runStrCases(t, []strCase{
		{"大写", strUpper, os_("Hello你"), "HELLO你"},
		{"小写", strLower, os_("Hello"), "hello"},
		{"首字母大写", strCapitalize, os_("hello WORLD"), "Hello world"},
		{"首字母大写-空串", strCapitalize, os_(""), ""},
		{"标题化", strTitle, os_("hello world"), "Hello World"},
		{"标题化-撇号分词", strTitle, os_("they're"), "They'Re"},
		{"标题化-数字分词", strTitle, os_("abc123def"), "Abc123Def"},
		{"大小写互换", strSwapCase, os_("AbC中"), "aBc中"},
		{"大小写折叠", strCasefold, os_("Straße"), "strasse"},
		{"忽略大小写相等", strEqualFold, []object.Object{o("AbC"), o("aBc")}, true},
		{"忽略大小写相等-否", strEqualFold, []object.Object{o("ab"), o("abc")}, false},
		{"比较-小", strCompare, []object.Object{o("a"), o("b")}, int64(-1)},
		{"比较-等", strCompare, []object.Object{o("a"), o("a")}, int64(0)},
		{"比较-大", strCompare, []object.Object{o("b"), o("a")}, int64(1)},
	})
}

// ---------- 裁剪 / 前后缀 ----------

func TestStrLibTrim(t *testing.T) {
	runStrCases(t, []strCase{
		{"去空白", strTrimSpace, os_("  hi\t\n"), "hi"},
		{"裁剪", strTrim, []object.Object{o("xxhiyy"), o("xy")}, "hi"},
		{"左裁剪", strTrimLeft, []object.Object{o("xxhi"), o("x")}, "hi"},
		{"右裁剪", strTrimRight, []object.Object{o("hixx"), o("x")}, "hi"},
		{"去左空白", strTrimLeftSpace, os_("  hi"), "hi"},
		{"去右空白", strTrimRightSpace, os_("hi  "), "hi"},
		{"去前缀", strTrimPrefix, []object.Object{o("prefix-body"), o("prefix-")}, "body"},
		{"去前缀-未命中", strTrimPrefix, []object.Object{o("body"), o("x-")}, "body"},
		{"去后缀", strTrimSuffix, []object.Object{o("a.txt"), o(".txt")}, "a"},
		{"前缀", strHasPrefix, []object.Object{o("hello"), o("he")}, true},
		{"后缀", strHasSuffix, []object.Object{o("hello"), o("lo")}, true},
		{"前缀任一", strHasPrefixAny, []object.Object{o("hello"), o([]string{"xx", "he"})}, true},
		{"前缀任一-否", strHasPrefixAny, []object.Object{o("hello"), o([]string{"xx", "yy"})}, false},
		{"后缀任一", strHasSuffixAny, []object.Object{o("hello"), o([]string{"xx", "lo"})}, true},
	})
}

// ---------- 查找 / 计数 / 替换 ----------

func TestStrLibSearch(t *testing.T) {
	runStrCases(t, []strCase{
		{"查找", strIndex, []object.Object{o("hello"), o("ll")}, int64(2)},
		{"查找-未命中", strIndex, []object.Object{o("hello"), o("zz")}, int64(-1)},
		{"查找-中文下标", strIndex, []object.Object{o("你好ab"), o("a")}, int64(2)},
		{"反向查找", strLastIndex, []object.Object{o("a.b.c"), o(".")}, int64(3)},
		{"查找任一", strIndexAny, []object.Object{o("hello"), o("lo")}, int64(2)},
		{"反向查找任一", strLastIndexAny, []object.Object{o("hello"), o("lo")}, int64(4)},
		{"查找字符", strIndexRune, []object.Object{o("hello"), o("l")}, int64(2)},
		{"包含", strContains, []object.Object{o("hello"), o("ell")}, true},
		{"包含任一", strContainsAny, []object.Object{o("hello"), o("xyz")}, false},
		{"计数", strCount, []object.Object{o("aaa"), o("a")}, int64(3)},
		{"替换", strReplace, []object.Object{o("aaa"), o("a"), o("b")}, "bbb"},
		{"替换次", strReplaceN, []object.Object{o("aaa"), o("a"), o("b"), o(2)}, "bba"},
		{"替换次-全部", strReplaceN, []object.Object{o("aaa"), o("a"), o("b"), o(-1)}, "bbb"},
	})
}

func TestStrLibReplaceMulti(t *testing.T) {
	h := &object.Hash{}
	h.SetKey(o("a"), o("1"))
	h.SetKey(o("b"), o("2"))
	h.SetKey(o("c"), o("3"))
	got := strReplaceMulti(o("a+b=c"), h)
	gs, ok := got.(*object.String)
	if !ok || gs.Value != "1+2=3" {
		t.Fatalf("批量替换 want 1+2=3, got %v", got.Inspect())
	}

	m := &object.Hash{}
	m.SetKey(o("a"), o("1"))
	m.SetKey(o("b"), o("2"))
	got = strTranslate(o("abc"), m)
	if gs, ok := got.(*object.String); !ok || gs.Value != "12c" {
		t.Fatalf("替换字符 want 12c, got %v", got.Inspect())
	}
}

// ---------- 分割 / 连接 / 位置分割 ----------

func TestStrLibSplit(t *testing.T) {
	check := func(name string, got object.Object, want []string) {
		t.Helper()
		arr, ok := got.(*object.Array)
		if !ok {
			t.Fatalf("%s: want array, got %s", name, got.Type())
		}
		if len(arr.Elements) != len(want) {
			t.Fatalf("%s: want %d elems %v, got %d: %s", name, len(want), want, len(arr.Elements), got.Inspect())
		}
		for i, el := range arr.Elements {
			gs, ok := el.(*object.String)
			if !ok || gs.Value != want[i] {
				t.Fatalf("%s: elem %d want %q, got %v", name, i, want[i], el.Inspect())
			}
		}
	}

	check("分割", strSplit(o("a,b,c"), o(",")), []string{"a", "b", "c"})
	check("分割-空分隔", strSplit(o("abc"), o("")), []string{"a", "b", "c"})
	check("分割次", strSplitN(o("a,b,c"), o(","), o(2)), []string{"a", "b,c"})
	check("反向分割", strRSplit(o("a,b,c"), o(",")), []string{"a", "b", "c"})
	check("反向分割次", strRSplitN(o("a,b,c"), o(","), o(2)), []string{"a,b", "c"})
	check("反向分割次-3", strRSplitN(o("a,b,c,d"), o(","), o(3)), []string{"a,b", "c", "d"})
	check("按行分割", strSplitLines(o("a\nb\r\nc")), []string{"a", "b", "c"})
	check("按行分割-末尾换行", strSplitLines(o("a\n")), []string{"a"})
	check("分词", strFields(o("a  b\tc")), []string{"a", "b", "c"})
	check("分词按", strFieldsFunc(o("a,b;c"), o(",;")), []string{"a", "b", "c"})
	check("位置分割", strPartition(o("a=b"), o("=")), []string{"a", "=", "b"})
	check("位置分割-未命中", strPartition(o("abc"), o("=")), []string{"abc", "", ""})
	check("反向位置分割", strRPartition(o("a.b.c"), o(".")), []string{"a.b", ".", "c"})
	check("反向位置分割-未命中", strRPartition(o("abc"), o("=")), []string{"", "", "abc"})
}

// ---------- 对齐 / 填充 ----------

func TestStrLibAlign(t *testing.T) {
	runStrCases(t, []strCase{
		{"左对齐", strLJust, []object.Object{o("hi"), o(6), o("-")}, "hi----"},
		{"右对齐", strRJust, []object.Object{o("hi"), o(6), o("-")}, "----hi"},
		{"居中-偶数", strCenter, []object.Object{o("hi"), o(6), o("-")}, "--hi--"},
		{"居中-奇数", strCenter, []object.Object{o("hi"), o(7), o("-")}, "---hi--"},
		{"左对齐-已够宽", strLJust, []object.Object{o("hello"), o(3)}, "hello"},
		{"零填充", strZFill, []object.Object{o("42"), o(5)}, "00042"},
		{"零填充-负数", strZFill, []object.Object{o("-42"), o(5)}, "-0042"},
		{"零填充-已够宽", strZFill, []object.Object{o("abc"), o(2)}, "abc"},
		{"重复", strRepeat, []object.Object{o("ab"), o(3)}, "ababab"},
		{"重复-零次", strRepeat, []object.Object{o("ab"), o(0)}, ""},
		{"展开制表符", strExpandTabs, []object.Object{o("a\tb"), o(4)}, "a   b"},
		{"展开制表符-默认8", strExpandTabs, os_("a\tb"), "a       b"},
	})
}

// ---------- 判定 ----------

func TestStrLibClassify(t *testing.T) {
	runStrCases(t, []strCase{
		{"是数字", strIsDigit, os_("123"), true},
		{"是数字-空", strIsDigit, os_(""), false},
		{"是数字-字母", strIsDigit, os_("12a"), false},
		{"是十进制", strIsDecimal, os_("123"), true},
		{"是数值", strIsNumeric, os_("123"), true},
		{"是字母", strIsAlpha, os_("abc中文"), true},
		{"是字母数字", strIsAlnum, os_("a1中"), true},
		{"是空白", strIsSpace, os_(" \t\n"), true},
		{"是空白-否", strIsSpace, os_(" a"), false},
		{"是大写", strIsUpper, os_("ABC中"), true},
		{"是大写-混合", strIsUpper, os_("ABc"), false},
		{"是大写-无大小写", strIsUpper, os_("中文"), false},
		{"是小写", strIsLower, os_("abc中"), true},
		{"是标题", strIsTitle, os_("Hello World"), true},
		{"是标题-否", strIsTitle, os_("Hello world"), false},
		{"是标题-空", strIsTitle, os_(""), false},
		{"是可打印", strIsPrintable, os_("abc 中"), true},
		{"是可打印-空", strIsPrintable, os_(""), true},
		{"是可打印-控制符", strIsPrintable, os_("a\x00b"), false},
		{"是标识符", strIsIdentifier, os_("a1_中"), true},
		{"是标识符-数字开头", strIsIdentifier, os_("1a"), false},
		{"是标识符-空", strIsIdentifier, os_(""), false},
		{"是ASCII", strIsASCII, os_("abc"), true},
		{"是ASCII-空", strIsASCII, os_(""), true},
		{"是ASCII-中文", strIsASCII, os_("a中"), false},
	})
}

// ---------- 子串 / 反转 ----------

func TestStrLibSub(t *testing.T) {
	runStrCases(t, []strCase{
		{"取字符", strCharAt, []object.Object{o("hello"), o(1)}, "e"},
		{"取字符-中文", strCharAt, []object.Object{o("你好"), o(1)}, "好"},
		{"子串", strSubstr, []object.Object{o("hello"), o(1), o(4)}, "ell"},
		{"子串-负索引", strSubstr, []object.Object{o("hello"), o(-3), o(-1)}, "ll"},
		{"子串-越界截断", strSubstr, []object.Object{o("hello"), o(3), o(100)}, "lo"},
		{"子串-起大于止", strSubstr, []object.Object{o("hello"), o(4), o(2)}, ""},
		{"子串-省略止", strSubstr, []object.Object{o("hello"), o(2)}, "llo"},
		{"子串-中文", strSubstr, []object.Object{o("你好世界"), o(1), o(3)}, "好世"},
		{"取左", strLeft, []object.Object{o("hello"), o(2)}, "he"},
		{"取左-超长", strLeft, []object.Object{o("hi"), o(9)}, "hi"},
		{"取右", strRight, []object.Object{o("hello"), o(2)}, "lo"},
		{"取右-超长", strRight, []object.Object{o("hi"), o(9)}, "hi"},
		{"反转", strReverse, os_("你好abc"), "cba好你"},
	})
}

// ---------- 中文友好 ----------

func TestStrLibZh(t *testing.T) {
	runStrCases(t, []strCase{
		{"显示宽度", strDisplayWidth, os_("你好ab"), int64(6)},
		{"显示宽度-全角标点", strDisplayWidth, os_("，。"), int64(4)},
		{"显示宽度-空", strDisplayWidth, os_(""), int64(0)},
		{"按宽度截断", strTruncateWidth, []object.Object{o("你好世界abc"), o(6), o("…")}, "你好…"},
		{"按宽度截断-未超", strTruncateWidth, []object.Object{o("你好"), o(6), o("…")}, "你好"},
		{"按宽度填充-左", strPadWidth, []object.Object{o("你好"), o(8), o("-"), o("左")}, "你好----"},
		{"按宽度填充-右", strPadWidth, []object.Object{o("你好"), o(8), o("-"), o("右")}, "----你好"},
		{"按宽度填充-中", strPadWidth, []object.Object{o("你好"), o(8), o("-"), o("中")}, "--你好--"},
		{"中文数字-百", strParseChineseNumber, os_("一百二十三"), int64(123)},
		{"中文数字-十二", strParseChineseNumber, os_("十二"), int64(12)},
		{"中文数字-两万三千", strParseChineseNumber, os_("两万三千"), int64(23000)},
		{"中文数字-负", strParseChineseNumber, os_("负一百"), int64(-100)},
		{"中文数字-零", strParseChineseNumber, os_("零"), int64(0)},
		{"中文数字串-0", strChineseNumber, []object.Object{o(0)}, "零"},
		{"中文数字串-12", strChineseNumber, []object.Object{o(12)}, "十二"},
		{"中文数字串-123", strChineseNumber, []object.Object{o(123)}, "一百二十三"},
		{"中文数字串-110", strChineseNumber, []object.Object{o(110)}, "一百一十"},
		{"中文数字串-10086", strChineseNumber, []object.Object{o(10086)}, "一万零八十六"},
		{"中文数字串-亿", strChineseNumber, []object.Object{o(100000000)}, "一亿"},
		{"中文数字串-亿零一", strChineseNumber, []object.Object{o(100000001)}, "一亿零一"},
		{"中文数字串-负", strChineseNumber, []object.Object{o(-5)}, "负五"},
		{"包含中文", strContainsHan, os_("hello你好"), true},
		{"包含中文-否", strContainsHan, os_("hello"), false},
	})
}

// ---------- 错误路径 ----------

func TestStrLibErrors(t *testing.T) {
	wantErr := func(name string, got object.Object, substr string) {
		t.Helper()
		e, ok := got.(*object.Error)
		if !ok {
			t.Fatalf("%s: want error, got %v", name, got.Inspect())
		}
		if !strings.Contains(e.Message, substr) {
			t.Fatalf("%s: want error containing %q, got %q", name, substr, e.Message)
		}
	}
	wantErr("大写-无参", strUpper(), "期望 1 个参数")
	wantErr("大写-类型", strUpper(o(1)), "参数必须是字符串")
	wantErr("子串-缺参", strSubstr(o("a")), "期望 2-3 个参数")
	wantErr("重复-负数", strRepeat(o("a"), o(-1)), "不能为负数")
	wantErr("查找字符-多字符", strIndexRune(o("abc"), o("ab")), "单个字符")
	wantErr("前缀任一-元素类型", strHasPrefixAny(o("a"), o([]object.Object{o(1)})), "必须是字符串")
	wantErr("批量替换-值类型", strReplaceMulti(o("a"), func() *object.Hash {
		h := &object.Hash{}
		h.SetKey(o("a"), o(1))
		return h
	}()), "值必须是字符串")
}

// ---------- 命名空间注入（端到端） ----------

func TestStringLibNamespace(t *testing.T) {
	env := NewEnvironment()
	mod, ok := env.Get("文本")
	if !ok {
		t.Fatal("文本 未注入全局环境")
	}
	m, ok := mod.(*object.Module)
	if !ok {
		t.Fatalf("文本 类型 = %s，期望 模块", mod.Type())
	}
	if m.Name != "文本" {
		t.Fatalf("模块名 = %q", m.Name)
	}
	if n := StringLibMethodCount(); n < 90 {
		t.Fatalf("命名空间成员数 = %d，期望 >= 90", n)
	}
	// 关键成员存在性抽查
	for _, name := range []string{"大写", "子串", "按宽度截断", "中文数字", "base64编码", "正则匹配", "转文本"} {
		if _, ok := m.Env.Get(name); !ok {
			t.Fatalf("命名空间缺少成员 %s", name)
		}
	}
	// 旧全局名仍可用（零破坏）
	for _, name := range []string{"大写", "替换", "分割", "取字符", "格式化"} {
		if _, ok := env.Get(name); !ok {
			t.Fatalf("旧全局名 %s 丢失", name)
		}
	}
}
