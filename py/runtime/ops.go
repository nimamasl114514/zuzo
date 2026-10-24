package runtime

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"zuzo/py/object"
)

// ==================== 真值与相等 ====================

// Eq Python 相等比较（含 __eq__ 分派）
func (ip *Interp) Eq(a, b object.Object) bool {
	if inst, ok := a.(*object.Instance); ok {
		if m, ok := ip.findDunder(inst.Class, "__eq__"); ok {
			r := ip.Call(ip.bindMethod(inst, m), []object.Object{b}, nil)
			if br, ok := r.(*object.Bool); ok {
				return br.V
			}
			return object.Truthy(r)
		}
	}
	if inst, ok := b.(*object.Instance); ok {
		if m, ok := ip.findDunder(inst.Class, "__eq__"); ok {
			r := ip.Call(ip.bindMethod(inst, m), []object.Object{a}, nil)
			if br, ok := r.(*object.Bool); ok {
				return br.V
			}
			return object.Truthy(r)
		}
	}
	return object.Equal(a, b)
}

// IsIdentical is 运算
func (ip *Interp) IsIdentical(a, b object.Object) bool {
	if a == b {
		return true
	}
	// None / True / False / 小整数 / 字符串驻留（简化：值相等即视为同一对象）
	if a == nil || b == nil {
		return false
	}
	if _, ok := a.(*object.NoneObj); ok {
		_, ok2 := b.(*object.NoneObj)
		return ok2
	}
	if x, ok := a.(*object.Bool); ok {
		if y, ok2 := b.(*object.Bool); ok2 {
			return x.V == y.V
		}
	}
	if x, ok := a.(*object.Int); ok {
		if y, ok2 := b.(*object.Int); ok2 {
			return x.V == y.V
		}
	}
	if x, ok := a.(*object.Str); ok {
		if y, ok2 := b.(*object.Str); ok2 {
			return x.V == y.V
		}
	}
	return false
}

// Cmp 三路比较（走 __eq__ / __lt__ 等魔术方法）
func (ip *Interp) Cmp(a, b object.Object) (int, bool) {
	if inst, ok := a.(*object.Instance); ok {
		if ip.Eq(a, b) {
			return 0, true
		}
		if m, found := ip.findDunder(inst.Class, "__lt__"); found {
			if object.Truthy(ip.Call(ip.bindMethod(inst, m), []object.Object{b}, nil)) {
				return -1, true
			}
		}
		if m, found := ip.findDunder(inst.Class, "__gt__"); found {
			if object.Truthy(ip.Call(ip.bindMethod(inst, m), []object.Object{b}, nil)) {
				return 1, true
			}
		}
		return 0, false
	}
	if inst, ok := b.(*object.Instance); ok {
		if m, found := ip.findDunder(inst.Class, "__gt__"); found {
			if object.Truthy(ip.Call(ip.bindMethod(inst, m), []object.Object{a}, nil)) {
				return -1, true
			}
		}
		return 0, false
	}
	return object.Compare(a, b)
}

// Contains in 运算
func (ip *Interp) Contains(container, item object.Object) bool {
	switch c := container.(type) {
	case *object.Str:
		s, ok := item.(*object.Str)
		if !ok {
			ip.TypeError("'in <string>' requires string as left operand, not %s", object.TypeName(item))
		}
		return strings.Contains(c.V, s.V)
	case *object.Bytes:
		switch it := item.(type) {
		case *object.Bytes:
			return strings.Contains(string(c.V), string(it.V))
		case *object.Int:
			for _, by := range c.V {
				if int64(by) == it.V {
					return true
				}
			}
			return false
		}
		ip.TypeError("a bytes-like object is required")
	case *object.List:
		for _, it := range c.Items {
			if ip.Eq(it, item) {
				return true
			}
		}
		return false
	case *object.Tuple:
		for _, it := range c.Items {
			if ip.Eq(it, item) {
				return true
			}
		}
		return false
	case *object.Set:
		return c.Contains(item)
	case *object.Dict:
		return c.Index(item) >= 0
	case *object.Range:
		n, ok := object.AsInt(item)
		if !ok {
			return false
		}
		if c.Step > 0 {
			return n >= c.Start && n < c.Stop && (n-c.Start)%c.Step == 0
		}
		if c.Step < 0 {
			return n <= c.Start && n > c.Stop && (c.Start-n)%(-c.Step) == 0
		}
		return false
	case *object.Instance:
		if m, ok := ip.findDunder(c.Class, "__contains__"); ok {
			r := ip.Call(ip.bindMethod(c, m), []object.Object{item}, nil)
			return object.Truthy(r)
		}
		// 退化到迭代
		it := ip.Iter(container)
		for {
			v, ok := ip.IterNext(it)
			if !ok {
				break
			}
			if ip.Eq(v, item) {
				return true
			}
		}
		return false
	}
	ip.TypeError("argument of type '%s' is not iterable", object.TypeName(container))
	return false
}

// ==================== 二元运算 ====================

var dunderNames = map[string]string{
	"+": "__add__", "-": "__sub__", "*": "__mul__", "/": "__truediv__",
	"//": "__floordiv__", "%": "__mod__", "**": "__pow__",
	"<<": "__lshift__", ">>": "__rshift__", "&": "__and__", "|": "__or__", "^": "__xor__",
	"@": "__matmul__",
}

var rdunderNames = map[string]string{
	"+": "__radd__", "-": "__rsub__", "*": "__rmul__", "/": "__rtruediv__",
	"//": "__rfloordiv__", "%": "__rmod__", "**": "__rpow__",
	"<<": "__rlshift__", ">>": "__rrshift__", "&": "__rand__", "|": "__ror__", "^": "__rxor__",
}

// BinOp 二元运算
func (ip *Interp) BinOp(op string, a, b object.Object) object.Object {
	// 用户类型优先走 dunder
	if inst, ok := a.(*object.Instance); ok {
		if _, builtinLeft := a.(*object.Int); !builtinLeft {
			if m, found := ip.findDunder(inst.Class, dunderNames[op]); found {
				r := ip.Call(ip.bindMethod(inst, m), []object.Object{b}, nil)
				if _, notImpl := r.(*object.NoneObj); !notImpl {
					return r
				}
			}
		}
	}
	if inst, ok := b.(*object.Instance); ok {
		if m, found := ip.findDunder(inst.Class, rdunderNames[op]); found {
			return ip.Call(ip.bindMethod(inst, m), []object.Object{a}, nil)
		}
	}

	switch op {
	case "+":
		return ip.opAdd(a, b)
	case "-":
		return ip.opSub(a, b)
	case "*":
		return ip.opMul(a, b)
	case "/":
		return ip.opDiv(a, b)
	case "//":
		return ip.opFloorDiv(a, b)
	case "%":
		return ip.opMod(a, b)
	case "**":
		return ip.opPow(a, b)
	case "&":
		return ip.opBit("&", a, b)
	case "|":
		return ip.opBit("|", a, b)
	case "^":
		return ip.opBit("^", a, b)
	case "<<":
		return ip.opShift("<<", a, b)
	case ">>":
		return ip.opShift(">>", a, b)
	}
	ip.TypeError("unsupported operand type(s) for %s: '%s' and '%s'", op, object.TypeName(a), object.TypeName(b))
	return object.None
}

// ==================== 整数快路径 + 大整数回退 ====================

func addI64(a, b int64) (int64, bool) {
	r := a + b
	if (b > 0 && r < a) || (b < 0 && r > a) {
		return 0, false
	}
	return r, true
}

func subI64(a, b int64) (int64, bool) {
	r := a - b
	if (b < 0 && r < a) || (b > 0 && r > a) {
		return 0, false
	}
	return r, true
}

func mulI64(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	r := a * b
	if r/b != a {
		return 0, false
	}
	return r, true
}

func intAdd(a, b *object.Int) *object.Int {
	if a.IsSmall() && b.IsSmall() {
		if r, ok := addI64(a.V, b.V); ok {
			return &object.Int{V: r}
		}
	}
	return object.NewBigInt(new(big.Int).Add(a.AsBig(), b.AsBig()))
}

func intSub(a, b *object.Int) *object.Int {
	if a.IsSmall() && b.IsSmall() {
		if r, ok := subI64(a.V, b.V); ok {
			return &object.Int{V: r}
		}
	}
	return object.NewBigInt(new(big.Int).Sub(a.AsBig(), b.AsBig()))
}

func intMul(a, b *object.Int) *object.Int {
	if a.IsSmall() && b.IsSmall() {
		if r, ok := mulI64(a.V, b.V); ok {
			return &object.Int{V: r}
		}
	}
	return object.NewBigInt(new(big.Int).Mul(a.AsBig(), b.AsBig()))
}

func (ip *Interp) isFloatOp(a, b object.Object) bool {
	_, a1 := a.(*object.Float)
	_, b1 := b.(*object.Float)
	return a1 || b1
}

func (ip *Interp) opAdd(a, b object.Object) object.Object {
	// 快路径：int OP int（循环计数器/累加最常见，先于所有类型分发）
	if ai, ok := a.(*object.Int); ok {
		if bi, ok2 := b.(*object.Int); ok2 {
			return intAdd(ai, bi)
		}
	}
	if ip.isFloatOp(a, b) {
		af, ok1 := object.AsFloat(a)
		bf, ok2 := object.AsFloat(b)
		if ok1 && ok2 {
			return &object.Float{V: af + bf}
		}
	}
	if x, ok := a.(*object.Str); ok {
		if y, ok2 := b.(*object.Str); ok2 {
			return &object.Str{V: x.V + y.V}
		}
		ip.TypeError("can only concatenate str (not \"%s\") to str", object.TypeName(b))
	}
	if x, ok := a.(*object.Bytes); ok {
		if y, ok2 := b.(*object.Bytes); ok2 {
			n := make([]byte, 0, len(x.V)+len(y.V))
			n = append(n, x.V...)
			n = append(n, y.V...)
			return &object.Bytes{V: n}
		}
	}
	if x, ok := a.(*object.List); ok {
		if y, ok2 := b.(*object.List); ok2 {
			items := make([]object.Object, 0, len(x.Items)+len(y.Items))
			items = append(items, x.Items...)
			items = append(items, y.Items...)
			return &object.List{Items: items}
		}
	}
	if x, ok := a.(*object.Tuple); ok {
		if y, ok2 := b.(*object.Tuple); ok2 {
			items := make([]object.Object, 0, len(x.Items)+len(y.Items))
			items = append(items, x.Items...)
			items = append(items, y.Items...)
			return &object.Tuple{Items: items}
		}
	}
	if ai, ok := a.(*object.Int); ok {
		if bi, ok2 := b.(*object.Int); ok2 {
			return intAdd(ai, bi)
		}
		if bb, ok3 := b.(*object.Bool); ok3 {
			v := int64(0)
			if bb.V {
				v = 1
			}
			return intAdd(ai, &object.Int{V: v})
		}
	}
	if ab, ok := a.(*object.Bool); ok {
		v := int64(0)
		if ab.V {
			v = 1
		}
		if bi, ok2 := b.(*object.Int); ok2 {
			return intAdd(&object.Int{V: v}, bi)
		}
		if bb, ok3 := b.(*object.Bool); ok3 {
			w := int64(0)
			if bb.V {
				w = 1
			}
			return &object.Int{V: v + w}
		}
	}
	ip.TypeError("unsupported operand type(s) for +: '%s' and '%s'", object.TypeName(a), object.TypeName(b))
	return object.None
}

func (ip *Interp) opSub(a, b object.Object) object.Object {
	// 快路径：int OP int（循环计数器/累加最常见，先于所有类型分发）
	if ai, ok := a.(*object.Int); ok {
		if bi, ok2 := b.(*object.Int); ok2 {
			return intSub(ai, bi)
		}
	}
	if ip.isFloatOp(a, b) {
		af, ok1 := object.AsFloat(a)
		bf, ok2 := object.AsFloat(b)
		if ok1 && ok2 {
			return &object.Float{V: af - bf}
		}
	}
	if x, ok := a.(*object.Set); ok {
		if y, ok2 := b.(*object.Set); ok2 {
			out := &object.Set{}
			for _, it := range x.Items {
				if !y.Contains(it) {
					out.Add(it)
				}
			}
			return out
		}
	}
	if ai, ok := toIntObj(a); ok {
		if bi, ok2 := toIntObj(b); ok2 {
			return intSub(ai, bi)
		}
	}
	ip.TypeError("unsupported operand type(s) for -: '%s' and '%s'", object.TypeName(a), object.TypeName(b))
	return object.None
}

func (ip *Interp) opMul(a, b object.Object) object.Object {
	// 快路径：int OP int（循环计数器/累加最常见，先于所有类型分发）
	if ai, ok := a.(*object.Int); ok {
		if bi, ok2 := b.(*object.Int); ok2 {
			return intMul(ai, bi)
		}
	}
	if x, ok := a.(*object.Str); ok {
		if n, ok2 := object.AsInt(b); ok2 {
			return &object.Str{V: repeatString(x.V, n)}
		}
	}
	if x, ok := a.(*object.List); ok {
		if n, ok2 := object.AsInt(b); ok2 {
			items := make([]object.Object, 0, len(x.Items)*int(n))
			for i := int64(0); i < n; i++ {
				items = append(items, x.Items...)
			}
			return &object.List{Items: items}
		}
	}
	if x, ok := a.(*object.Tuple); ok {
		if n, ok2 := object.AsInt(b); ok2 {
			items := make([]object.Object, 0, len(x.Items)*int(n))
			for i := int64(0); i < n; i++ {
				items = append(items, x.Items...)
			}
			return &object.Tuple{Items: items}
		}
	}
	// 反向（int * str）
	if _, ok := a.(*object.Int); ok {
		switch b.(type) {
		case *object.Str, *object.List, *object.Tuple:
			return ip.opMul(b, a)
		}
	}
	if ip.isFloatOp(a, b) {
		af, ok1 := object.AsFloat(a)
		bf, ok2 := object.AsFloat(b)
		if ok1 && ok2 {
			return &object.Float{V: af * bf}
		}
	}
	if ai, ok := toIntObj(a); ok {
		if bi, ok2 := toIntObj(b); ok2 {
			return intMul(ai, bi)
		}
	}
	ip.TypeError("unsupported operand type(s) for *: '%s' and '%s'", object.TypeName(a), object.TypeName(b))
	return object.None
}

func repeatString(s string, n int64) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, int(n))
}

func (ip *Interp) opDiv(a, b object.Object) object.Object {
	af, ok1 := object.AsFloat(a)
	bf, ok2 := object.AsFloat(b)
	if ok1 && ok2 {
		if bf == 0 {
			ip.ZeroDivErr("division by zero")
		}
		return &object.Float{V: af / bf}
	}
	ip.TypeError("unsupported operand type(s) for /: '%s' and '%s'", object.TypeName(a), object.TypeName(b))
	return object.None
}

func (ip *Interp) opFloorDiv(a, b object.Object) object.Object {
	if ip.isFloatOp(a, b) {
		af, ok1 := object.AsFloat(a)
		bf, ok2 := object.AsFloat(b)
		if ok1 && ok2 {
			if bf == 0 {
				ip.ZeroDivErr("float floor division by zero")
			}
			return &object.Float{V: math.Floor(af / bf)}
		}
	}
	ao1, ok1 := toIntObj(a)
	bo1, ok2 := toIntObj(b)
	if ok1 && ok2 {
		if bo1.AsBig().Sign() == 0 {
			ip.ZeroDivErr("integer division or modulo by zero")
		}
		if ao1.IsSmall() && bo1.IsSmall() {
			ai, bi := ao1.V, bo1.V
			q := ai / bi
			if (ai%bi != 0) && ((ai < 0) != (bi < 0)) {
				q--
			}
			return &object.Int{V: q}
		}
		q := new(big.Int)
		m := new(big.Int)
		q.QuoRem(ao1.AsBig(), bo1.AsBig(), m)
		if m.Sign() != 0 && ((ao1.AsBig().Sign() < 0) != (bo1.AsBig().Sign() < 0)) {
			q.Sub(q, big.NewInt(1))
		}
		return object.NewBigInt(q)
	}
	ip.TypeError("unsupported operand type(s) for //: '%s' and '%s'", object.TypeName(a), object.TypeName(b))
	return object.None
}

func (ip *Interp) opMod(a, b object.Object) object.Object {
	// 字符串格式化
	if s, ok := a.(*object.Str); ok {
		return ip.formatPercent(s.V, b)
	}
	if ip.isFloatOp(a, b) {
		af, ok1 := object.AsFloat(a)
		bf, ok2 := object.AsFloat(b)
		if ok1 && ok2 {
			if bf == 0 {
				ip.ZeroDivErr("float modulo")
			}
			r := math.Mod(af, bf)
			if r != 0 && ((bf < 0) != (r < 0)) {
				r += bf
			}
			return &object.Float{V: r}
		}
	}
	ao1, ok1 := toIntObj(a)
	bo1, ok2 := toIntObj(b)
	if ok1 && ok2 {
		if bo1.AsBig().Sign() == 0 {
			ip.ZeroDivErr("integer division or modulo by zero")
		}
		if ao1.IsSmall() && bo1.IsSmall() {
			ai, bi := ao1.V, bo1.V
			r := ai % bi
			if r != 0 && ((bi < 0) != (r < 0)) {
				r += bi
			}
			return &object.Int{V: r}
		}
		r := new(big.Int).Rem(ao1.AsBig(), bo1.AsBig())
		if r.Sign() != 0 && ((bo1.AsBig().Sign() < 0) != (r.Sign() < 0)) {
			r.Add(r, bo1.AsBig())
		}
		return object.NewBigInt(r)
	}
	ip.TypeError("unsupported operand type(s) for %%: '%s' and '%s'", object.TypeName(a), object.TypeName(b))
	return object.None
}

func (ip *Interp) opPow(a, b object.Object) object.Object {
	if ai, ok := toIntObj(a); ok {
		if bi, ok2 := toIntObj(b); ok2 && bi.AsBig().Sign() >= 0 {
			if bi.IsSmall() && bi.V <= 1000000 {
				if ai.IsSmall() {
					result := int64(1)
					base := ai.V
					exp := bi.V
					overflow := false
					for exp > 0 {
						if exp&1 == 1 {
							if mulOverflow(result, base) {
								overflow = true
								break
							}
							result *= base
						}
						exp >>= 1
						if exp > 0 {
							if mulOverflow(base, base) {
								overflow = true
								break
							}
							base *= base
						}
					}
					if !overflow {
						return &object.Int{V: result}
					}
				}
				r := new(big.Int).Exp(ai.AsBig(), big.NewInt(bi.V), nil)
				return object.NewBigInt(r)
			}
		}
	}
	af, ok1 := object.AsFloat(a)
	bf, ok2 := object.AsFloat(b)
	if ok1 && ok2 {
		r := math.Pow(af, bf)
		if math.IsNaN(r) && !math.IsNaN(af) && !math.IsNaN(bf) {
			ip.raise(ip.excType("ValueError"), "math domain error")
		}
		return &object.Float{V: r}
	}
	ip.TypeError("unsupported operand type(s) for **: '%s' and '%s'", object.TypeName(a), object.TypeName(b))
	return object.None
}

func mulOverflow(a, b int64) bool {
	if a == 0 || b == 0 {
		return false
	}
	r := a * b
	return r/b != a
}

func (ip *Interp) opBit(op string, a, b object.Object) object.Object {
	// 集合运算
	if x, ok := a.(*object.Set); ok {
		if y, ok2 := b.(*object.Set); ok2 {
			switch op {
			case "&":
				out := &object.Set{}
				for _, it := range x.Items {
					if y.Contains(it) {
						out.Add(it)
					}
				}
				return out
			case "|":
				out := &object.Set{}
				for _, it := range x.Items {
					out.Add(it)
				}
				for _, it := range y.Items {
					out.Add(it)
				}
				return out
			case "^":
				out := &object.Set{}
				for _, it := range x.Items {
					if !y.Contains(it) {
						out.Add(it)
					}
				}
				for _, it := range y.Items {
					if !x.Contains(it) {
						out.Add(it)
					}
				}
				return out
			}
		}
	}
	// 字典合并
	if x, ok := a.(*object.Dict); ok && op == "|" {
		if y, ok2 := b.(*object.Dict); ok2 {
			out := x.Copy()
			for i, k := range y.Keys {
				out.Set(k, y.Vals[i])
			}
			return out
		}
	}
	ao1, ok1 := toIntObj(a)
	bo1, ok2 := toIntObj(b)
	if ok1 && ok2 {
		if ao1.IsSmall() && bo1.IsSmall() {
			ai, bi := ao1.V, bo1.V
			switch op {
			case "&":
				return &object.Int{V: ai & bi}
			case "|":
				return &object.Int{V: ai | bi}
			case "^":
				return &object.Int{V: ai ^ bi}
			}
		}
		r := new(big.Int)
		switch op {
		case "&":
			r.And(ao1.AsBig(), bo1.AsBig())
		case "|":
			r.Or(ao1.AsBig(), bo1.AsBig())
		case "^":
			r.Xor(ao1.AsBig(), bo1.AsBig())
		}
		return object.NewBigInt(r)
	}
	ip.TypeError("unsupported operand type(s) for %s: '%s' and '%s'", op, object.TypeName(a), object.TypeName(b))
	return object.None
}

func (ip *Interp) opShift(op string, a, b object.Object) object.Object {
	ao1, ok1 := toIntObj(a)
	bi, ok2 := object.AsInt(b)
	if ok1 && ok2 {
		if bi < 0 {
			ip.ValueError("negative shift count")
		}
		if ao1.IsSmall() && bi < 63 {
			if op == "<<" {
				r := ao1.V << uint(bi)
				if r>>uint(bi) == ao1.V {
					return &object.Int{V: r}
				}
			} else {
				return &object.Int{V: ao1.V >> uint(bi)}
			}
		}
		r := new(big.Int)
		if op == "<<" {
			r.Lsh(ao1.AsBig(), uint(bi))
		} else {
			r.Rsh(ao1.AsBig(), uint(bi))
		}
		return object.NewBigInt(r)
	}
	ip.TypeError("unsupported operand type(s) for %s: '%s' and '%s'", op, object.TypeName(a), object.TypeName(b))
	return object.None
}

// UnaryOp 一元运算
func (ip *Interp) UnaryOp(op string, v object.Object) object.Object {
	switch op {
	case "not":
		return object.GetBool(!object.Truthy(v))
	case "-":
		if inst, ok := v.(*object.Instance); ok {
			if m, found := ip.findDunder(inst.Class, "__neg__"); found {
				return ip.Call(ip.bindMethod(inst, m), nil, nil)
			}
		}
		if io, ok := toIntObj(v); ok {
			return object.NewBigInt(new(big.Int).Neg(io.AsBig()))
		}
		if f, ok := object.AsFloat(v); ok {
			return &object.Float{V: -f}
		}
	case "+":
		if object.IsNumber(v) {
			return v
		}
	case "~":
		if io, ok := toIntObj(v); ok {
			return object.NewBigInt(new(big.Int).Not(io.AsBig()))
		}
	}
	ip.TypeError("bad operand type for unary %s: '%s'", op, object.TypeName(v))
	return object.None
}

// toIntObj 把 Int/Bool 统一为 *object.Int
func toIntObj(o object.Object) (*object.Int, bool) {
	switch v := o.(type) {
	case *object.Int:
		return v, true
	case *object.Bool:
		if v.V {
			return &object.Int{V: 1}, true
		}
		return &object.Int{V: 0}, true
	}
	return nil, false
}

// ==================== repr / str ====================

// Repr 调用 repr()
func (ip *Interp) Repr(o object.Object) string {
	if o == nil {
		return "None"
	}
	if inst, ok := o.(*object.Instance); ok {
		if m, found := ip.findDunder(inst.Class, "__repr__"); found {
			r := ip.Call(ip.bindMethod(inst, m), nil, nil)
			if s, ok := r.(*object.Str); ok {
				return s.V
			}
		}
	}
	if exc, ok := o.(*object.Exception); ok {
		if m, found := ip.findDunder(exc.Class, "__repr__"); found {
			r := ip.Call(ip.bindMethod(exc, m), nil, nil)
			if s, ok := r.(*object.Str); ok {
				return s.V
			}
		}
	}
	return o.Repr()
}

// StrOf 调用 str()
func (ip *Interp) StrOf(o object.Object) string {
	if o == nil {
		return "None"
	}
	if inst, ok := o.(*object.Instance); ok {
		if m, found := ip.findDunder(inst.Class, "__str__"); found {
			r := ip.Call(ip.bindMethod(inst, m), nil, nil)
			if s, ok := r.(*object.Str); ok {
				return s.V
			}
		}
		if m, found := ip.findDunder(inst.Class, "__repr__"); found {
			r := ip.Call(ip.bindMethod(inst, m), nil, nil)
			if s, ok := r.(*object.Str); ok {
				return s.V
			}
		}
	}
	if exc, ok := o.(*object.Exception); ok {
		if _, found := ip.findDunder(exc.Class, "__str__"); !found {
			return exc.Str()
		}
		if m, found := ip.findDunder(exc.Class, "__str__"); found {
			r := ip.Call(ip.bindMethod(exc, m), nil, nil)
			if s, ok := r.(*object.Str); ok {
				return s.V
			}
		}
	}
	return o.Str()
}

// ==================== % 字符串格式化 ====================

func (ip *Interp) formatPercent(format string, arg object.Object) object.Object {
	var args []object.Object
	if t, ok := arg.(*object.Tuple); ok {
		args = t.Items
	} else {
		args = []object.Object{arg}
	}
	var sb strings.Builder
	ai := 0
	rs := []rune(format)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if c != '%' {
			sb.WriteRune(c)
			continue
		}
		if i+1 >= len(rs) {
			sb.WriteRune(c)
			break
		}
		i++
		// 跳过映射键
		if rs[i] == '(' {
			depth := 1
			for i < len(rs) && depth > 0 {
				i++
				if i < len(rs) && rs[i] == '(' {
					depth++
				}
				if i < len(rs) && rs[i] == ')' {
					depth--
				}
			}
			if i < len(rs) {
				i++
			}
		}
		// 标志与宽度
		start := i
		for i < len(rs) && strings.ContainsRune("-+ #0", rs[i]) {
			i++
		}
		for i < len(rs) && (rs[i] >= '0' && rs[i] <= '9' || rs[i] == '.') {
			i++
		}
		verb := rs[i]
		spec := string(rs[start:i])
		_ = spec
		if verb == '%' {
			sb.WriteRune('%')
			continue
		}
		if ai >= len(args) {
			ip.TypeError("not enough arguments for format string")
		}
		v := args[ai]
		ai++
		switch verb {
		case 's':
			sb.WriteString(ip.StrOf(v))
		case 'r':
			sb.WriteString(ip.Repr(v))
		case 'd', 'i':
			n, ok := object.AsInt(v)
			if !ok {
				ip.TypeError("%%%c format: a real number is required", verb)
			}
			sb.WriteString(strconv.FormatInt(n, 10))
		case 'f':
			f, _ := object.AsFloat(v)
			prec := 6
			if p := strings.Index(spec, "."); p >= 0 {
				fmt.Sscanf(spec[p:], ".%d", &prec)
			}
			sb.WriteString(strconv.FormatFloat(f, 'f', prec, 64))
		case 'e':
			f, _ := object.AsFloat(v)
			sb.WriteString(strconv.FormatFloat(f, 'e', 6, 64))
		case 'g':
			f, _ := object.AsFloat(v)
			sb.WriteString(strconv.FormatFloat(f, 'g', -1, 64))
		case 'x':
			n, _ := object.AsInt(v)
			sb.WriteString(strconv.FormatInt(n, 16))
		case 'X':
			n, _ := object.AsInt(v)
			sb.WriteString(strings.ToUpper(strconv.FormatInt(n, 16)))
		case 'o':
			n, _ := object.AsInt(v)
			sb.WriteString(strconv.FormatInt(n, 8))
		case 'c':
			if n, ok := object.AsInt(v); ok {
				sb.WriteRune(rune(n))
			} else if s, ok := v.(*object.Str); ok {
				sb.WriteString(s.V)
			}
		default:
			sb.WriteString(ip.StrOf(v))
		}
	}
	return &object.Str{V: sb.String()}
}

// ==================== format() 说明符 ====================

// FormatValue 按 Python format spec 格式化
func (ip *Interp) FormatValue(v object.Object, spec string) string {
	if spec == "" {
		return ip.StrOf(v)
	}
	// 解析 [[fill]align][sign][#][0][width][,][.precision][type]
	s := []rune(spec)
	idx := 0
	fill := rune(' ')
	align := rune(0)
	if len(s) >= 2 && strings.ContainsRune("<>=^", s[1]) {
		fill = s[0]
		align = s[1]
		idx = 2
	} else if len(s) >= 1 && strings.ContainsRune("<>=^", s[0]) {
		align = s[0]
		idx = 1
	}
	sign := rune(0)
	if idx < len(s) && strings.ContainsRune("+- ", s[idx]) {
		sign = s[idx]
		idx++
	}
	if idx < len(s) && s[idx] == '#' {
		idx++
	}
	zero := false
	if idx < len(s) && s[idx] == '0' {
		zero = true
		idx++
	}
	width := 0
	for idx < len(s) && s[idx] >= '0' && s[idx] <= '9' {
		width = width*10 + int(s[idx]-'0')
		idx++
	}
	comma := false
	underscore := false
	if idx < len(s) && s[idx] == ',' {
		comma = true
		idx++
	} else if idx < len(s) && s[idx] == '_' {
		underscore = true
		idx++
	}
	prec := -1
	if idx < len(s) && s[idx] == '.' {
		idx++
		prec = 0
		for idx < len(s) && s[idx] >= '0' && s[idx] <= '9' {
			prec = prec*10 + int(s[idx]-'0')
			idx++
		}
	}
	typ := byte(0)
	if idx < len(s) {
		typ = byte(s[idx])
	}
	if zero && align == 0 {
		fill = '0'
		align = '='
	}

	var body string
	switch obj := v.(type) {
	case *object.Int:
		n := obj.V
		nf, _ := object.AsFloat(obj)
		switch typ {
		case 'd', 0:
			body = obj.Repr()
		case 'x':
			body = bigText(obj, 16)
		case 'X':
			body = strings.ToUpper(bigText(obj, 16))
		case 'o':
			body = bigText(obj, 8)
		case 'b':
			body = bigText(obj, 2)
		case 'f', 'F':
			body = strconv.FormatFloat(nf, 'f', maxInt(prec, 6), 64)
		case 'e':
			body = strconv.FormatFloat(nf, 'e', maxInt(prec, 6), 64)
		case 'g':
			body = strconv.FormatFloat(nf, 'g', -1, 64)
		case 'c':
			body = string(rune(n))
		case '%':
			body = strconv.FormatFloat(nf*100, 'f', maxInt(prec, 6), 64) + "%"
		default:
			body = obj.Repr()
		}
		if comma {
			body = addThousands(body)
		} else if underscore {
			body = addUnderscores(body)
		}
		if sign == '+' && n >= 0 {
			body = "+" + body
		} else if sign == ' ' && n >= 0 {
			body = " " + body
		}
	case *object.Float:
		f := obj.V
		switch typ {
		case 'f', 'F':
			body = strconv.FormatFloat(f, 'f', maxInt(prec, 6), 64)
		case 'e':
			body = strconv.FormatFloat(f, 'e', maxInt(prec, 6), 64)
		case 'E':
			body = strings.ToUpper(strconv.FormatFloat(f, 'e', maxInt(prec, 6), 64))
		case 'g':
			p := prec
			if p < 0 {
				p = -1
			}
			body = strconv.FormatFloat(f, 'g', p, 64)
		case '%':
			body = strconv.FormatFloat(f*100, 'f', maxInt(prec, 6), 64) + "%"
		case 'd':
			body = strconv.FormatInt(int64(f), 10)
		default:
			if prec >= 0 {
				body = strconv.FormatFloat(f, 'f', prec, 64)
			} else {
				body = strconv.FormatFloat(f, 'g', -1, 64)
			}
		}
		if comma {
			body = addThousands(body)
		} else if underscore {
			body = addUnderscores(body)
		}
		if sign == '+' && f >= 0 {
			body = "+" + body
		}
	case *object.Str:
		body = obj.V
		if prec >= 0 {
			rs := []rune(body)
			if len(rs) > prec {
				body = string(rs[:prec])
			}
		}
	default:
		body = ip.StrOf(v)
	}
	if width > len([]rune(body)) {
		pad := width - len([]rune(body))
		switch align {
		case '<':
			body = body + strings.Repeat(string(fill), pad)
		case '^':
			l := pad / 2
			body = strings.Repeat(string(fill), l) + body + strings.Repeat(string(fill), pad-l)
		case '=':
			// 符号后填充
			if len(body) > 0 && (body[0] == '-' || body[0] == '+' || body[0] == ' ') {
				body = string(body[0]) + strings.Repeat(string(fill), pad) + body[1:]
			} else {
				body = strings.Repeat(string(fill), pad) + body
			}
		default:
			body = strings.Repeat(string(fill), pad) + body
		}
	}
	return body
}

// bigText 整数按进制输出（支持大整数）
func bigText(o *object.Int, base int) string {
	if o.IsSmall() && base == 10 {
		return strconv.FormatInt(o.V, 10)
	}
	if o.IsSmall() && o.V >= 0 {
		return strconv.FormatInt(o.V, base)
	}
	if o.IsSmall() {
		return "-" + new(big.Int).Abs(o.AsBig()).Text(base)
	}
	if o.AsBig().Sign() < 0 {
		return "-" + new(big.Int).Abs(o.AsBig()).Text(base)
	}
	return o.AsBig().Text(base)
}

func maxInt(a, b int) int {
	if a < 0 {
		return b
	}
	return a
}

// addUnderscores 与 addThousands 同构，但用下划线分组（CPython 的 `_` 千分位）
func addUnderscores(s string) string {
	return strings.ReplaceAll(addThousands(s), ",", "_")
}

func addThousands(s string) string {
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	intPart := s
	frac := ""
	if i := strings.Index(s, "."); i >= 0 {
		intPart = s[:i]
		frac = s[i:]
	}
	var sb strings.Builder
	n := len(intPart)
	for i, c := range intPart {
		if i > 0 && (n-i)%3 == 0 {
			sb.WriteByte(',')
		}
		sb.WriteRune(c)
	}
	out := sb.String() + frac
	if neg {
		out = "-" + out
	}
	return out
}
