package evaluator

import (
	"encoding/json"
	"fmt"

	"zuzo/object"
)

// ==================== JSON 内置函数（封装 Go encoding/json） ====================

// json解析(s) → 哈希|数组 — 将 JSON 字符串解析为 zuzo 值
// 支持对象→哈希、数组→数组、基本类型直接映射
func builtinJsonParse(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("json解析() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("json解析() 参数必须是字符串，得到 %s", args[0].Type())
	}

	var raw interface{}
	if err := json.Unmarshal([]byte(s.Value), &raw); err != nil {
		return newError("json解析() 失败: %s", err.Error())
	}

	return jsonToZuzo(raw)
}

// json序列化(值) → 字符串 — 将 zuzo 值序列化为 JSON 字符串
func builtinJsonStringify(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("json序列化() 期望 1 个参数，得到 %d 个", len(args))
	}

	raw := zuzoToJSON(args[0])
	data, err := json.Marshal(raw)
	if err != nil {
		return newError("json序列化() 失败: %s", err.Error())
	}
	return &object.String{Value: string(data)}
}

// json格式化(值, 缩进) → 字符串 — 将 zuzo 值序列化为带缩进的 JSON 字符串
func builtinJsonFormat(args ...object.Object) object.Object {
	if len(args) < 1 || len(args) > 2 {
		return newError("json格式化() 期望 1 或 2 个参数，得到 %d 个", len(args))
	}

	raw := zuzoToJSON(args[0])
	indent := "  "
	if len(args) == 2 {
		if i, ok := args[1].(*object.String); ok {
			indent = i.Value
		}
	}

	data, err := json.MarshalIndent(raw, "", indent)
	if err != nil {
		return newError("json格式化() 失败: %s", err.Error())
	}
	return &object.String{Value: string(data)}
}

// --- 内部转换函数 ---

// jsonToZuzo 将 Go 解析出的 interface{} 转为 zuzo object.Object
func jsonToZuzo(raw interface{}) object.Object {
	switch v := raw.(type) {
	case nil:
		return NULL
	case bool:
		return nativeBoolToBooleanObject(v)
	case float64:
		// JSON 数字统一为 float64，整数则转整数
		if v == float64(int64(v)) {
			return &object.Integer{Value: int64(v)}
		}
		return &object.Float{Value: v}
	case string:
		return &object.String{Value: v}
	case []interface{}:
		elems := make([]object.Object, len(v))
		for i, e := range v {
			elems[i] = jsonToZuzo(e)
		}
		return &object.Array{Elements: elems}
	case map[string]interface{}:
		hash := &object.Hash{}
		for k, val := range v {
			key := &object.String{Value: k}
			hash.SetKey(key, jsonToZuzo(val))
		}
		return hash
	default:
		return newError("json解析() 不支持的类型: %T", raw)
	}
}

// zuzoToJSON 将 zuzo object.Object 转为 Go interface{} 供 json.Marshal 使用
func zuzoToJSON(obj object.Object) interface{} {
	if obj == nil {
		return nil
	}
	switch v := obj.(type) {
	case *object.Integer:
		return v.Value
	case *object.Float:
		return v.Value
	case *object.String:
		return v.Value
	case *object.Boolean:
		return v.Value
	case *object.Null:
		return nil
	case *object.Array:
		arr := make([]interface{}, len(v.Elements))
		for i, e := range v.Elements {
			arr[i] = zuzoToJSON(e)
		}
		return arr
	case *object.Hash:
		m := make(map[string]interface{})
		for _, hk := range v.Keys {
			pair := v.Pairs[hk]
			key, ok := pair.Key.(*object.String)
			if !ok {
				key = &object.String{Value: fmt.Sprintf("%s", pair.Key.Inspect())}
			}
			m[key.Value] = zuzoToJSON(pair.Value)
		}
		return m
	default:
		return obj.Inspect()
	}
}
