package object

// clib_call.go — C 函数指针调用器
//
// 使用 CGO 调用 C 函数指针。通过统一的 C 调用桥，将 Go 参数按类型转换后
// 传递给 C 函数指针，再将返回值转回 Go 类型。
//
// 支持 0~10 个参数，覆盖绝大多数 C 库函数。

/*
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

// 统一的调用桥：接收函数指针和参数数组（按 uint64 编码），根据类型描述符调用
// 为了简化，我们为每种参数数量生成一个调用函数

// 通用调用函数指针的桥接
// 参数通过 uint64_t 数组传递，返回值也是 uint64_t
// type_ids: 0=void 1=int 2=uint 3=long 4=ulong 5=longlong 6=ulonglong
//           7=float 8=double 9=char 10=uchar 11=short 12=ushort 13=bool 14=ptr 15=string

typedef int64_t  (*fn0_t)(void);
typedef int64_t  (*fn1_t)(int64_t);
typedef int64_t  (*fn2_t)(int64_t, int64_t);
typedef int64_t  (*fn3_t)(int64_t, int64_t, int64_t);
typedef int64_t  (*fn4_t)(int64_t, int64_t, int64_t, int64_t);
typedef int64_t  (*fn5_t)(int64_t, int64_t, int64_t, int64_t, int64_t);
typedef int64_t  (*fn6_t)(int64_t, int64_t, int64_t, int64_t, int64_t, int64_t);
typedef int64_t  (*fn7_t)(int64_t, int64_t, int64_t, int64_t, int64_t, int64_t, int64_t);
typedef int64_t  (*fn8_t)(int64_t, int64_t, int64_t, int64_t, int64_t, int64_t, int64_t, int64_t);
typedef int64_t  (*fn9_t)(int64_t, int64_t, int64_t, int64_t, int64_t, int64_t, int64_t, int64_t, int64_t);
typedef int64_t  (*fn10_t)(int64_t, int64_t, int64_t, int64_t, int64_t, int64_t, int64_t, int64_t, int64_t, int64_t);

// 浮点参数版本 (用于 float/double 参数)
typedef double (*fn1d_t)(double);
typedef double (*fn2d_t)(double, double);
typedef double (*fn3d_t)(double, double, double);
typedef double (*fn4d_t)(double, double, double, double);
typedef double (*fn5d_t)(double, double, double, double, double);
typedef double (*fn6d_t)(double, double, double, double, double, double);
typedef double (*fn7d_t)(double, double, double, double, double, double, double);
typedef double (*fn8d_t)(double, double, double, double, double, double, double, double);

// C 函数调用器
// fn: 函数指针
// nparams: 参数数量
// args: 参数值数组 (int64_t 编码)
// arg_types: 参数类型数组
// ret_type: 返回值类型
// ret_val: 输出参数，存储返回值 (int64 或 double 的位模式)
// 返回 0 成功，非 0 失败
static int zuzo_call_c(void* fn, int nparams, int64_t* args, int* arg_types, int ret_type,
                       uint64_t* ret_int, double* ret_dbl) {
    switch (nparams) {
    case 0: {
        int64_t r = ((fn0_t)fn)();
        *ret_int = (uint64_t)r;
        return 0;
    }
    case 1: {
        // 检查是否有浮点参数
        if (arg_types[0] == 7 || arg_types[0] == 8) {
            double r = ((fn1d_t)fn)(*(double*)&args[0]);
            *ret_dbl = r;
            return 0;
        }
        int64_t r = ((fn1_t)fn)(args[0]);
        *ret_int = (uint64_t)r;
        return 0;
    }
    case 2: {
        if ((arg_types[0] == 7 || arg_types[0] == 8) && (arg_types[1] == 7 || arg_types[1] == 8)) {
            double r = ((fn2d_t)fn)(*(double*)&args[0], *(double*)&args[1]);
            *ret_dbl = r;
            return 0;
        }
        int64_t r = ((fn2_t)fn)(args[0], args[1]);
        *ret_int = (uint64_t)r;
        return 0;
    }
    case 3: {
        if ((arg_types[0] == 7 || arg_types[0] == 8) && (arg_types[1] == 7 || arg_types[1] == 8) &&
            (arg_types[2] == 7 || arg_types[2] == 8)) {
            double r = ((fn3d_t)fn)(*(double*)&args[0], *(double*)&args[1], *(double*)&args[2]);
            *ret_dbl = r;
            return 0;
        }
        int64_t r = ((fn3_t)fn)(args[0], args[1], args[2]);
        *ret_int = (uint64_t)r;
        return 0;
    }
    case 4: {
        if ((arg_types[0] == 7 || arg_types[0] == 8) && (arg_types[1] == 7 || arg_types[1] == 8) &&
            (arg_types[2] == 7 || arg_types[2] == 8) && (arg_types[3] == 7 || arg_types[3] == 8)) {
            double r = ((fn4d_t)fn)(*(double*)&args[0], *(double*)&args[1], *(double*)&args[2], *(double*)&args[3]);
            *ret_dbl = r;
            return 0;
        }
        int64_t r = ((fn4_t)fn)(args[0], args[1], args[2], args[3]);
        *ret_int = (uint64_t)r;
        return 0;
    }
    case 5: {
        int64_t r = ((fn5_t)fn)(args[0], args[1], args[2], args[3], args[4]);
        *ret_int = (uint64_t)r;
        return 0;
    }
    case 6: {
        int64_t r = ((fn6_t)fn)(args[0], args[1], args[2], args[3], args[4], args[5]);
        *ret_int = (uint64_t)r;
        return 0;
    }
    case 7: {
        int64_t r = ((fn7_t)fn)(args[0], args[1], args[2], args[3], args[4], args[5], args[6]);
        *ret_int = (uint64_t)r;
        return 0;
    }
    case 8: {
        int64_t r = ((fn8_t)fn)(args[0], args[1], args[2], args[3], args[4], args[5], args[6], args[7]);
        *ret_int = (uint64_t)r;
        return 0;
    }
    case 9: {
        int64_t r = ((fn9_t)fn)(args[0], args[1], args[2], args[3], args[4], args[5], args[6], args[7], args[8]);
        *ret_int = (uint64_t)r;
        return 0;
    }
    case 10: {
        int64_t r = ((fn10_t)fn)(args[0], args[1], args[2], args[3], args[4], args[5], args[6], args[7], args[8], args[9]);
        *ret_int = (uint64_t)r;
        return 0;
    }
    default:
        return -1; // 参数太多
    }
}

// 辅助: 创建 C 字符串
static const char* zuzo_cstr(const char* s) {
    return s;
}

// 辅助: 读取 C 字符串
static const char* zuzo_read_cstr(const char* p) {
    return p;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// CallCFunc 调用 C 函数指针
// fn: 函数地址
// paramTypes: 参数类型列表
// retType: 返回值类型
// args: 参数值列表 (已按类型转换好的 int64/double 值，存为 int64)
// 返回 (retInt, retDbl, isFloatRet, error)
func CallCFunc(fn uintptr, paramTypes []CFFIType, retType CFFIType, args []int64) (uint64, float64, bool, error) {
	nparams := len(paramTypes)
	if nparams > 10 {
		return 0, 0, false, fmt.Errorf("参数数量超过上限 (最多 10 个，得到 %d 个)", nparams)
	}

	// 准备 C 参数数组
	var cArgs *C.int64_t
	var cArgTypes *C.int
	if nparams > 0 {
		cArgsBuf := make([]C.int64_t, nparams)
		cArgTypesBuf := make([]C.int, nparams)
		for i, pt := range paramTypes {
			cArgsBuf[i] = C.int64_t(args[i])
			cArgTypesBuf[i] = C.int(int(pt))
		}
		cArgs = &cArgsBuf[0]
		cArgTypes = &cArgTypesBuf[0]
	}

	var retInt C.uint64_t
	var retDbl C.double

	fnPtr := unsafe.Pointer(fn)
	status := int(C.zuzo_call_c(fnPtr, C.int(nparams), cArgs, cArgTypes, C.int(int(retType)), &retInt, &retDbl))
	if status != 0 {
		return 0, 0, false, fmt.Errorf("C 函数调用失败 (状态码: %d)", status)
	}

	isFloatRet := retType == CFloat || retType == CDouble
	return uint64(retInt), float64(retDbl), isFloatRet, nil
}

// CallCFuncWithStrings 调用可能包含 string 参数的 C 函数
// 对于 CStr 类型参数，将 Go string 转换为 C 字符串并传递指针
func CallCFuncWithStrings(fn uintptr, paramTypes []CFFIType, retType CFFIType, goArgs []interface{}) (uint64, float64, bool, error) {
	nparams := len(paramTypes)
	if nparams > 10 {
		return 0, 0, false, fmt.Errorf("参数数量超过上限")
	}
	if len(goArgs) != nparams {
		return 0, 0, false, fmt.Errorf("参数数量不匹配: 期望 %d 个，得到 %d 个", nparams, len(goArgs))
	}

	// 分配 C 字符串（需要保持引用直到调用完成）
	cStrAllocs := make([]unsafe.Pointer, 0, nparams)
	defer func() {
		for _, p := range cStrAllocs {
			C.free(p)
		}
	}()

	// 将参数转换为 int64 编码
	intArgs := make([]int64, nparams)
	for i, pt := range paramTypes {
		switch pt {
		case CStr:
			s, ok := goArgs[i].(string)
			if !ok {
				return 0, 0, false, fmt.Errorf("参数 %d 期望字符串，得到 %T", i+1, goArgs[i])
			}
			cStr := C.CString(s)
			cStrAllocs = append(cStrAllocs, unsafe.Pointer(cStr))
			intArgs[i] = int64(uintptr(unsafe.Pointer(cStr)))
		case CPtr:
			// 指针参数：接受 uintptr 或整数
			switch v := goArgs[i].(type) {
			case uintptr:
				intArgs[i] = int64(v)
			case int64:
				intArgs[i] = v
			case int:
				intArgs[i] = int64(v)
			default:
				return 0, 0, false, fmt.Errorf("参数 %d 期望指针，得到 %T", i+1, goArgs[i])
			}
		case CFloat:
			v, ok := goArgs[i].(float64)
			if !ok {
				return 0, 0, false, fmt.Errorf("参数 %d 期望浮点数，得到 %T", i+1, goArgs[i])
			}
			// 将 float32 的位模式存入 int64
			f32 := float32(v)
			intArgs[i] = int64(*(*int32)(unsafe.Pointer(&f32)))
		case CDouble:
			v, ok := goArgs[i].(float64)
			if !ok {
				return 0, 0, false, fmt.Errorf("参数 %d 期望浮点数，得到 %T", i+1, goArgs[i])
			}
			// 将 float64 的位模式存入 int64
			intArgs[i] = int64(*(*int64)(unsafe.Pointer(&v)))
		default:
			// 整数类型
			switch v := goArgs[i].(type) {
			case int64:
				intArgs[i] = v
			case int:
				intArgs[i] = int64(v)
			case float64:
				intArgs[i] = int64(v)
			case bool:
				if v {
					intArgs[i] = 1
				} else {
					intArgs[i] = 0
				}
			default:
				return 0, 0, false, fmt.Errorf("参数 %d 类型不支持: %T", i+1, goArgs[i])
			}
		}
	}

	return CallCFunc(fn, paramTypes, retType, intArgs)
}

// ReadCString 从 C 字符串指针读取 Go 字符串
func ReadCString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	return C.GoString((*C.char)(unsafe.Pointer(ptr)))
}
