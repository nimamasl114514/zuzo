# ctypes 原生 FFI：加载系统 C 库并调用（与 CPython ctypes 同形）
import ctypes

lib = ctypes.CDLL("msvcrt.dll")

lib.sqrt.argtypes = [ctypes.c_double]
lib.sqrt.restype = ctypes.c_double
print("sqrt:", lib.sqrt(144.0))

lib.abs.argtypes = [ctypes.c_int]
lib.abs.restype = ctypes.c_int
print("abs:", lib.abs(-42))

lib.pow.argtypes = [ctypes.c_double, ctypes.c_double]
lib.pow.restype = ctypes.c_double
print("pow:", lib.pow(2.0, 10.0))

lib.strlen.argtypes = [ctypes.c_char_p]
lib.strlen.restype = ctypes.c_int
print("strlen:", lib.strlen(b"hello"))

print("sizeof int:", ctypes.sizeof(ctypes.c_int))
print("sizeof double:", ctypes.sizeof(ctypes.c_double))
