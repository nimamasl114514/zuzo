package pyext

// proto.go — 与 CPython 子进程之间的值编码（JSON Lines）
//
// 契约见 plan/第32轮-20261101-Python库无缝调用.md「协议契约」：
//   {"k":"none"} {"k":"bool","v":..} {"k":"int","v":"-123"} {"k":"float","v":1.5}
//   {"k":"str","v":".."} {"k":"bytes","v":"<base64>"} {"k":"list","v":[enc..]}
//   {"k":"dict","v":[[enc,enc]..]} {"k":"obj","id":N,"t":"..","r":".."}
//   {"k":"call","id":N,"t":"..","r":".."}   {"k":"h","id":N}（zuzo 侧句柄回传）

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"zuzo/object"
)

// wireVal 子进程返回值的外层结构
type wireVal struct {
	K  string          `json:"k"`
	V  json.RawMessage `json:"v,omitempty"`
	ID int64           `json:"id,omitempty"`
	T  string          `json:"t,omitempty"`
	R  string          `json:"r,omitempty"`
}

// ==================== Python → zuzo ====================

// decodeVal 把子进程返回的 enc 解码成 zuzo 对象
func decodeVal(raw json.RawMessage, session string) (object.Object, error) {
	var w wireVal
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("pyext: 协议值解析失败: %v", err)
	}
	switch w.K {
	case "none":
		return &object.Null{}, nil
	case "bool":
		var b bool
		if err := json.Unmarshal(w.V, &b); err != nil {
			return nil, err
		}
		return &object.Boolean{Value: b}, nil
	case "int":
		var s string
		if err := json.Unmarshal(w.V, &s); err != nil {
			// 容忍直接给数字的实现
			var n int64
			if e2 := json.Unmarshal(w.V, &n); e2 == nil {
				return &object.Integer{Value: n}, nil
			}
			return nil, err
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			// 超出 int64：降级为字符串，避免静默丢精度
			return &object.String{Value: s}, nil
		}
		return &object.Integer{Value: n}, nil
	case "float":
		var s string
		if err := json.Unmarshal(w.V, &s); err == nil {
			switch s {
			case "inf", "+inf":
				return &object.Float{Value: math.Inf(1)}, nil
			case "-inf":
				return &object.Float{Value: math.Inf(-1)}, nil
			case "nan":
				return &object.Float{Value: math.NaN()}, nil
			}
			f, _ := strconv.ParseFloat(s, 64)
			return &object.Float{Value: f}, nil
		}
		var f float64
		if err := json.Unmarshal(w.V, &f); err != nil {
			return nil, err
		}
		return &object.Float{Value: f}, nil
	case "str":
		var s string
		if err := json.Unmarshal(w.V, &s); err != nil {
			return nil, err
		}
		return &object.String{Value: s}, nil
	case "bytes":
		var s string
		if err := json.Unmarshal(w.V, &s); err != nil {
			return nil, err
		}
		// zuzo 无 bytes 类型：以 base64 文本承载（py取值 可再转）
		return &object.String{Value: s}, nil
	case "list":
		var items []json.RawMessage
		if err := json.Unmarshal(w.V, &items); err != nil {
			return nil, err
		}
		arr := &object.Array{Elements: make([]object.Object, 0, len(items))}
		for _, it := range items {
			o, err := decodeVal(it, session)
			if err != nil {
				return nil, err
			}
			arr.Elements = append(arr.Elements, o)
		}
		return arr, nil
	case "dict":
		var pairs [][]json.RawMessage
		if err := json.Unmarshal(w.V, &pairs); err != nil {
			return nil, err
		}
		h := &object.Hash{Pairs: map[object.HashKey]object.HashPair{}}
		for _, pr := range pairs {
			if len(pr) != 2 {
				continue
			}
			k, err := decodeVal(pr[0], session)
			if err != nil {
				return nil, err
			}
			v, err := decodeVal(pr[1], session)
			if err != nil {
				return nil, err
			}
			hk, ok := k.(object.Hashable)
			if !ok {
				k = &object.String{Value: k.Inspect()}
				hk = k.(object.Hashable)
			}
			if _, exists := h.Pairs[hk.HashKey()]; !exists {
				h.Keys = append(h.Keys, hk.HashKey())
			}
			h.Pairs[hk.HashKey()] = object.HashPair{Key: k, Value: v}
		}
		return h, nil
	case "obj", "call":
		return &object.PyObj{ID: w.ID, Typ: w.T, Repr: w.R, Callable: w.K == "call", Bridge: "pyext", Session: session}, nil
	case "":
		// 兼容：实现方直接返回裸 JSON（无 k 标记）
		return decodeRawJSON(raw)
	default:
		return nil, fmt.Errorf("pyext: 未知值类型 %q", w.K)
	}
}

// decodeRawJSON 兜底：把裸 JSON 直接映射成 zuzo 值
func decodeRawJSON(raw json.RawMessage) (object.Object, error) {
	var v any
	dec := json.NewDecoder(bytesReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return fromGo(v), nil
}

func fromGo(v any) object.Object {
	switch x := v.(type) {
	case nil:
		return &object.Null{}
	case bool:
		return &object.Boolean{Value: x}
	case string:
		return &object.String{Value: x}
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return &object.Integer{Value: n}
		}
		f, _ := x.Float64()
		return &object.Float{Value: f}
	case float64:
		return &object.Float{Value: x}
	case []any:
		arr := &object.Array{Elements: make([]object.Object, 0, len(x))}
		for _, e := range x {
			arr.Elements = append(arr.Elements, fromGo(e))
		}
		return arr
	case map[string]any:
		h := &object.Hash{Pairs: map[object.HashKey]object.HashPair{}}
		for k, val := range x {
			ko := &object.String{Value: k}
			h.SetKey(ko, fromGo(val))
		}
		return h
	}
	return &object.String{Value: fmt.Sprint(v)}
}

// ==================== zuzo → Python ====================

// encodeObj 把 zuzo 对象编码成 enc
func encodeObj(o object.Object) (map[string]any, error) {
	if o == nil {
		return map[string]any{"k": "none"}, nil
	}
	switch v := o.(type) {
	case *object.Integer:
		return map[string]any{"k": "int", "v": strconv.FormatInt(v.Value, 10)}, nil
	case *object.Float:
		f := v.Value
		switch {
		case math.IsInf(f, 1):
			return map[string]any{"k": "float", "v": "inf"}, nil
		case math.IsInf(f, -1):
			return map[string]any{"k": "float", "v": "-inf"}, nil
		case math.IsNaN(f):
			return map[string]any{"k": "float", "v": "nan"}, nil
		}
		return map[string]any{"k": "float", "v": f}, nil
	case *object.String:
		return map[string]any{"k": "str", "v": v.Value}, nil
	case *object.Boolean:
		return map[string]any{"k": "bool", "v": v.Value}, nil
	case *object.Null:
		return map[string]any{"k": "none"}, nil
	case *object.Array:
		items := make([]any, 0, len(v.Elements))
		for _, e := range v.Elements {
			enc, err := encodeObj(e)
			if err != nil {
				return nil, err
			}
			items = append(items, enc)
		}
		return map[string]any{"k": "list", "v": items}, nil
	case *object.Hash:
		pairs := make([][2]any, 0, len(v.Keys))
		for _, hk := range v.Keys {
			pair, ok := v.Pairs[hk]
			if !ok {
				continue
			}
			ke, err := encodeObj(pair.Key)
			if err != nil {
				return nil, err
			}
			ve, err := encodeObj(pair.Value)
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, [2]any{ke, ve})
		}
		return map[string]any{"k": "dict", "v": pairs}, nil
	case *object.PyObj:
		return map[string]any{"k": "h", "id": v.ID}, nil
	default:
		return map[string]any{"k": "str", "v": o.Inspect()}, nil
	}
}

// bytesReader 小工具：避免为兜底解析引入 bytes 包的额外名字
func bytesReader(b []byte) *jsonReader { return &jsonReader{b: b} }

type jsonReader struct {
	b   []byte
	off int
}

func (r *jsonReader) Read(p []byte) (int, error) {
	if r.off >= len(r.b) {
		return 0, errEOF
	}
	n := copy(p, r.b[r.off:])
	r.off += n
	return n, nil
}

type eofErr struct{}

func (eofErr) Error() string { return "EOF" }

var errEOF = eofErr{}
