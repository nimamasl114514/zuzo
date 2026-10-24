package object

import (
	"fmt"
	"math"
	"math/big"
	"reflect"
)

// Truthy Python 真值判断
func Truthy(o Object) bool {
	switch v := o.(type) {
	case *NoneObj:
		return false
	case *Bool:
		return v.V
	case *Int:
		return v.V != 0
	case *Float:
		return v.V != 0
	case *Str:
		return v.V != ""
	case *Bytes:
		return len(v.V) > 0
	case *List:
		return len(v.Items) > 0
	case *Tuple:
		return len(v.Items) > 0
	case *Dict:
		return len(v.Keys) > 0
	case *Set:
		return len(v.Items) > 0
	case *Range:
		return v.Len() > 0
	}
	return true
}

// TypeName 返回对象的类型名
func TypeName(o Object) string {
	if o == nil {
		return "NoneType"
	}
	return o.Type().Name
}

// AsInt 尝试取整数值（大整数超出 int64 时返回 false）
func AsInt(o Object) (int64, bool) {
	switch v := o.(type) {
	case *Int:
		if v.Big != nil {
			if v.Big.IsInt64() {
				return v.Big.Int64(), true
			}
			return 0, false
		}
		return v.V, true
	case *Bool:
		if v.V {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// AsFloat 尝试取浮点值
func AsFloat(o Object) (float64, bool) {
	switch v := o.(type) {
	case *Float:
		return v.V, true
	case *Int:
		if v.Big != nil {
			f, _ := new(big.Float).SetInt(v.Big).Float64()
			return f, true
		}
		return float64(v.V), true
	case *Bool:
		if v.V {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// IsNumber 是否为数值
func IsNumber(o Object) bool {
	switch o.(type) {
	case *Int, *Float, *Bool:
		return true
	}
	return false
}

// Equal Python == 语义
func Equal(a, b Object) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	// 两个整数：走精确比较（支持大整数）
	if ai, ok1 := a.(*Int); ok1 {
		if bi, ok2 := b.(*Int); ok2 {
			return ai.AsBig().Cmp(bi.AsBig()) == 0
		}
	}
	if ab, ok1 := a.(*Bool); ok1 {
		if bb, ok2 := b.(*Bool); ok2 {
			return ab.V == bb.V
		}
	}
	// 数值跨类型比较
	if IsNumber(a) && IsNumber(b) {
		af, _ := AsFloat(a)
		bf, _ := AsFloat(b)
		return af == bf
	}
	switch x := a.(type) {
	case *NoneObj:
		_, ok := b.(*NoneObj)
		return ok
	case *Str:
		y, ok := b.(*Str)
		return ok && x.V == y.V
	case *Bytes:
		y, ok := b.(*Bytes)
		if !ok || len(x.V) != len(y.V) {
			return false
		}
		for i := range x.V {
			if x.V[i] != y.V[i] {
				return false
			}
		}
		return true
	case *List:
		y, ok := b.(*List)
		if !ok || len(x.Items) != len(y.Items) {
			return false
		}
		for i := range x.Items {
			if !Equal(x.Items[i], y.Items[i]) {
				return false
			}
		}
		return true
	case *Tuple:
		y, ok := b.(*Tuple)
		if !ok || len(x.Items) != len(y.Items) {
			return false
		}
		for i := range x.Items {
			if !Equal(x.Items[i], y.Items[i]) {
				return false
			}
		}
		return true
	case *Dict:
		y, ok := b.(*Dict)
		if !ok || len(x.Keys) != len(y.Keys) {
			return false
		}
		for i, k := range x.Keys {
			v, ok := y.Get(k)
			if !ok || !Equal(x.Vals[i], v) {
				return false
			}
		}
		return true
	case *Set:
		y, ok := b.(*Set)
		if !ok || len(x.Items) != len(y.Items) {
			return false
		}
		for _, it := range x.Items {
			if !y.Contains(it) {
				return false
			}
		}
		return true
	case *Range:
		y, ok := b.(*Range)
		if !ok {
			return false
		}
		if x.Len() != y.Len() {
			return false
		}
		if x.Len() == 0 {
			return true
		}
		return x.Start == y.Start && x.Step == y.Step
	case *Instance:
		y, ok := b.(*Instance)
		if ok && x.Class == y.Class {
			return x == y
		}
	case *Exception:
		y, ok := b.(*Exception)
		if ok && x.Class == y.Class && len(x.Args) == len(y.Args) {
			for i := range x.Args {
				if !Equal(x.Args[i], y.Args[i]) {
					return false
				}
			}
			return true
		}
	}
	return false
}

// Compare 返回 -1/0/1；ok=false 表示不可比较
func Compare(a, b Object) (int, bool) {
	// 整数精确比较（支持大整数）
	if ai, ok := a.(*Int); ok {
		if bi, ok2 := b.(*Int); ok2 {
			return ai.AsBig().Cmp(bi.AsBig()), true
		}
	}
	if IsNumber(a) && IsNumber(b) {
		af, _ := AsFloat(a)
		bf, _ := AsFloat(b)
		if math.IsNaN(af) || math.IsNaN(bf) {
			return 0, false
		}
		if af < bf {
			return -1, true
		}
		if af > bf {
			return 1, true
		}
		return 0, true
	}
	switch x := a.(type) {
	case *Str:
		if y, ok := b.(*Str); ok {
			if x.V < y.V {
				return -1, true
			}
			if x.V > y.V {
				return 1, true
			}
			return 0, true
		}
	case *Bytes:
		if y, ok := b.(*Bytes); ok {
			n := len(x.V)
			if len(y.V) < n {
				n = len(y.V)
			}
			for i := 0; i < n; i++ {
				if x.V[i] < y.V[i] {
					return -1, true
				}
				if x.V[i] > y.V[i] {
					return 1, true
				}
			}
			if len(x.V) < len(y.V) {
				return -1, true
			}
			if len(x.V) > len(y.V) {
				return 1, true
			}
			return 0, true
		}
	case *List:
		if y, ok := b.(*List); ok {
			return compareSeq(x.Items, y.Items)
		}
	case *Tuple:
		if y, ok := b.(*Tuple); ok {
			return compareSeq(x.Items, y.Items)
		}
	}
	return 0, false
}

func compareSeq(a, b []Object) (int, bool) {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if Equal(a[i], b[i]) {
			continue
		}
		c, ok := Compare(a[i], b[i])
		if !ok {
			return 0, false
		}
		return c, true
	}
	if len(a) < len(b) {
		return -1, true
	}
	if len(a) > len(b) {
		return 1, true
	}
	return 0, true
}

// HashKey 可哈希键
type HashKey struct {
	Kind string
	I    int64
	S    string
	B    bool
}

// Hash 计算哈希键
func Hash(o Object) (HashKey, bool) {
	switch v := o.(type) {
	case *NoneObj:
		return HashKey{Kind: "none"}, true
	case *Bool:
		return HashKey{Kind: "bool", B: v.V}, true
	case *Int:
		if v.Big != nil {
			return HashKey{Kind: "bignum", S: v.Big.String()}, true
		}
		return HashKey{Kind: "num", I: v.V}, true
	case *Float:
		if v.V == math.Trunc(v.V) && !math.IsInf(v.V, 0) {
			return HashKey{Kind: "num", I: int64(v.V)}, true
		}
		return HashKey{Kind: "float", S: fmt.Sprintf("%v", v.V)}, true
	case *Str:
		return HashKey{Kind: "str", S: v.V}, true
	case *Bytes:
		return HashKey{Kind: "bytes", S: string(v.V)}, true
	case *Tuple:
		s := ""
		for _, it := range v.Items {
			k, ok := Hash(it)
			if !ok {
				return HashKey{}, false
			}
			s += fmt.Sprintf("%v|", k)
		}
		return HashKey{Kind: "tuple", S: s}, true
	case *Instance:
		return HashKey{Kind: "id", I: int64(hashPtr(v))}, true
	case *Exception:
		return HashKey{Kind: "id", I: int64(hashPtr(v))}, true
	case *TypeObj:
		return HashKey{Kind: "id", I: int64(hashPtr(v))}, true
	case *Function:
		return HashKey{Kind: "id", I: int64(hashPtr(v))}, true
	case *Builtin:
		return HashKey{Kind: "id", I: int64(hashPtr(v))}, true
	}
	return HashKey{}, false
}

func hashPtr(o interface{}) uintptr {
	v := reflect.ValueOf(o)
	if v.Kind() == reflect.Ptr && !v.IsNil() {
		return v.Pointer()
	}
	return 0
}
