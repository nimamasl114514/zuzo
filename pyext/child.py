#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""pyext/child.py —— CPython 侧协议服务端（JSON Lines over stdio）。

用途
----
zuzo 的 `.hy` 代码通过 Go 侧客户端（pyext/client.go）拉起本进程，用「一行一条 JSON」
的方式远程操作 CPython 运行时：导入模块、取/设属性、调用函数、求值、执行、转原生值。
本文件是唯一实现依据 `plan/第32轮-20261101-Python库无缝调用.md` 的「协议契约」一节。

铁律
----
1. **stdin / stdout 专供协议**：一行一条 UTF-8 JSON，`\\n` 结尾。
2. **用户代码的 `print()` 一律重定向到 stderr**，绝不污染 stdout。
3. 只用标准库（numpy / pandas 通过鸭子类型识别，不做硬依赖）。
4. 句柄表 `dict[int, object]`，ID 自增从 1 起。

启动
----
    <python> -u child.py --stdio     # 协议服务模式（默认）
    <python> child.py --selftest    # 自检，打印 SELFTEST PASS n

值编码 enc
----------
    None                     {"k":"none"}
    bool                     {"k":"bool","v":true}
    int                      {"k":"int","v":"-123"}        # 十进制字符串
    float                    {"k":"float","v":1.5}         # inf/nan -> "inf"/"nan"
    str                      {"k":"str","v":"文本"}
    bytes / bytearray        {"k":"bytes","v":"<base64>"}
    list/tuple/range/set     {"k":"list","v":[enc, ...]}
    dict                     {"k":"dict","v":[[encK,encV], ...]}
    numpy 标量 shape==()     标量 enc（item()）
    ndarray size<=200000     {"k":"list","v":[...]}         # tolist() 嵌套
    ndarray 更大             句柄
    其余对象                  {"k":"obj","id":N,"t":"类型名","r":"repr前200"}
    可调用对象                {"k":"call","id":N,"t":"类型名","r":"repr前200"}
"""

from __future__ import annotations

import base64
import builtins
import collections
import importlib
import importlib.util
import itertools
import json
import os
import platform
import sys
import traceback
import uuid

PROTO_VERSION = 1
MAX_REPR = 200            # 契约：obj/call 的 repr 截前 200 字符
MAX_TB = 8000             # 回溯上限，避免异常把 stdout 撑爆
MAX_INLINE_ARRAY = 200000  # 契约：ndarray 内联阈值，超过走句柄
DEFAULT_MAX_HANDLES = 10000  # 句柄表容量上限（LRU），可用 ZUZO_PY_MAX_HANDLES 覆盖

# ---------------------------------------------------------------------------
# stdio 隔离：先占住真实 stdout，再把 sys.stdout 换成 stderr
# ---------------------------------------------------------------------------

# 1) 持有原对象的强引用，防止 sys.stdout 被改后 TextIOWrapper 被 GC 关掉底层 buffer
# 2) 再用 fd dup 出一份独立句柄，双保险
_orig_stdout = sys.stdout
_orig_stdin = sys.stdin
try:
    _real_out = os.fdopen(os.dup(1), "wb", buffering=0)
except Exception:
    _real_out = _orig_stdout.buffer
_real_in = _orig_stdin.buffer

try:  # 让中文 print 在 GBK 控制台也不炸
    sys.stderr.reconfigure(encoding="utf-8", errors="backslashreplace")
except Exception:
    pass

sys.stdout = sys.stderr          # 用户 print() -> stderr
sys.__stdout__ = sys.stderr      # 显式 print(file=sys.__stdout__) 也走 stderr

# 给被 exec 的用户代码准备一份"看起来正常"的 sys
try:
    sys.stdout.reconfigure(line_buffering=False)
except Exception:
    pass


def emit(obj) -> None:
    """向真实 stdout 写一行协议 JSON。"""
    data = json.dumps(obj, ensure_ascii=False, separators=(",", ":"))
    _real_out.write(data.encode("utf-8", "replace") + b"\n")
    _real_out.flush()


# ---------------------------------------------------------------------------
# 错误类型
# ---------------------------------------------------------------------------

class ProtocolError(Exception):
    """请求格式/编码不合法（不是用户代码的错误）。"""


class HandleError(ProtocolError):
    """引用了不存在的句柄。"""


class UnknownOp(ProtocolError):
    """未知 op。"""


class ShutdownSignal(Exception):
    """内部信号：收到 shutdown，主循环退出。"""


# ---------------------------------------------------------------------------
# 句柄表
# ---------------------------------------------------------------------------

# 句柄表：OrderedDict 维护「最近使用序」（队首最旧、队尾最新）。
# 长跑程序反复 py导入 会不断注册新句柄，不设上限则远端对象永远存活 → 内存缓慢增长。
# 因此超过 ZUZO_PY_MAX_HANDLES（默认 10000）时按 LRU 淘汰最旧：
#   - 淘汰 = 从 _handles 里 pop → 引用计数归零 → 有 __del__ / 文件句柄的对象由 CPython 自然回收；
#   - 被淘汰的 ID 再被 Go 侧引用得到 HandleError（与「子进程重启后旧句柄失效」同一语义）。
_handles: "collections.OrderedDict[int, object]" = collections.OrderedDict()
_ids = itertools.count(1)
_evicted = 0  # 累计淘汰数（自检 / 观测用）

def max_handles() -> int:
    """当前句柄表容量上限：ZUZO_PY_MAX_HANDLES，非法 / 非正数回落默认值。"""
    raw = os.environ.get("ZUZO_PY_MAX_HANDLES")
    if raw is not None:
        try:
            n = int(str(raw).strip())
            if n > 0:
                return n
        except (TypeError, ValueError):
            pass
    return DEFAULT_MAX_HANDLES

def _evict_lru() -> None:
    """超限时从队首（最久未使用）逐个淘汰。"""
    global _evicted
    limit = max_handles()
    while len(_handles) > limit:
        _handles.popitem(last=False)
        _evicted += 1

def _touch(hid: int) -> None:
    """命中句柄 → 移到队尾（最近使用）。"""
    try:
        _handles.move_to_end(hid)
    except KeyError:
        pass


def register(obj) -> int:
    hid = next(_ids)
    _handles[hid] = obj
    _evict_lru()  # 新句柄即最近使用；超限时先淘汰最旧的
    return hid


# 句柄 id 的键名：契约写作 id，但请求信封本身也用 id（请求号），
# 因此再接受 hid/h/handle/obj_id/target 等显式名，最后才回落 id。
_HANDLE_KEYS = ("hid", "h", "handle", "obj_id", "target")


def handle_id(req: dict):
    for key in _HANDLE_KEYS:
        if key in req and req[key] is not None:
            return req[key]
    return req.get("id")


def req_id(req: dict):
    """响应里回显的请求号：优先 rid，否则 id。"""
    if "rid" in req:
        return req["rid"]
    return req.get("id")


def lookup(req: dict) -> object:
    hid = handle_id(req)
    if hid is None:
        raise ProtocolError("missing handle id")
    try:
        hid = int(hid)
    except (TypeError, ValueError):
        raise ProtocolError("handle id must be an integer, got %r" % (hid,))
    if hid == 0:  # 0 = 常驻 globals（供 getglobal / setglobal / zuzo 侧 Lookup 使用）
        return GLOBALS
    try:
        obj = _handles[hid]
    except KeyError:
        raise HandleError("stale or unknown handle id %d" % hid)
    _touch(hid)           # 每次命中刷新访问序，保证 LRU 淘汰的是真正最久未用的
    return obj


# ---------------------------------------------------------------------------
# 编码：Python -> enc
# ---------------------------------------------------------------------------

def _short_repr(v) -> str:
    try:
        s = repr(v)
    except BaseException as e:  # repr 自己炸了也不能让协议挂掉
        return "<repr failed: %s>" % type(e).__name__
    if len(s) > MAX_REPR:
        s = s[:MAX_REPR]
    return s


def _type_name(v) -> str:
    try:
        return type(v).__name__
    except BaseException:
        return "object"


def _is_numpy(v) -> bool:
    mod = getattr(type(v), "__module__", "") or ""
    return mod.split(".", 1)[0] == "numpy"


def _handle_enc(v) -> dict:
    return {
        "k": "call" if callable(v) else "obj",
        "id": register(v),
        "t": _type_name(v),
        "r": _short_repr(v),
    }


def enc(v) -> dict:
    """把一个 Python 值编码成契约里的 enc 结构。"""
    if v is None:
        return {"k": "none"}
    if isinstance(v, bool):
        return {"k": "bool", "v": bool(v)}
    if isinstance(v, int):  # bool 已先处理
        return {"k": "int", "v": str(int(v))}
    if isinstance(v, float):
        # json 不接受 inf/nan 字面量，契约要求字符串
        if v != v:
            return {"k": "float", "v": "nan"}
        if v == float("inf"):
            return {"k": "float", "v": "inf"}
        if v == float("-inf"):
            return {"k": "float", "v": "-inf"}
        return {"k": "float", "v": float(v)}
    if isinstance(v, str):
        return {"k": "str", "v": v}
    if isinstance(v, (bytes, bytearray, memoryview)):
        raw = bytes(v)
        return {"k": "bytes", "v": base64.b64encode(raw).decode("ascii")}

    if _is_numpy(v):
        shape = getattr(v, "shape", None)
        if shape == ():
            # numpy 标量（np.float64 / np.int64）→ 原生标量，用户无感知
            try:
                item = v.item()
                if item is not v:
                    return enc(item)
            except BaseException:
                pass
        # ndarray 一律给句柄：保留 .sum() / .reshape() / .dtype 等全部方法，
        # 需要原生数组时由 zuzo 侧显式 py取值(h)（契约第32轮修订：方法优先于可读性）
        return _handle_enc(v)

    if isinstance(v, (list, tuple, range, set, frozenset)):
        return {"k": "list", "v": [enc(x) for x in v]}
    if isinstance(v, dict):
        return {"k": "dict", "v": [[enc(k), enc(x)] for k, x in v.items()]}

    return _handle_enc(v)


# ---------------------------------------------------------------------------
# 解码：enc -> Python
# ---------------------------------------------------------------------------

def _float_from(v) -> float:
    if isinstance(v, str):
        key = v.strip().lower()
        table = {
            "inf": float("inf"), "+inf": float("inf"), "infinity": float("inf"),
            "+infinity": float("inf"), "-inf": float("-inf"),
            "-infinity": float("-inf"), "nan": float("nan"), "-nan": float("nan"),
        }
        if key in table:
            return table[key]
        try:
            return float(key)
        except ValueError:
            raise ProtocolError("bad float literal %r" % (v,))
    try:
        return float(v)
    except (TypeError, ValueError):
        raise ProtocolError("bad float value %r" % (v,))


def dec(e):
    """把 enc 结构还原成 Python 值。obj/call 返回句柄指向的原对象。"""
    if not isinstance(e, dict):
        raise ProtocolError("encoded value must be a JSON object, got %s"
                            % type(e).__name__)
    k = e.get("k")
    if k == "none":
        return None
    if k == "bool":
        return bool(e.get("v"))
    if k == "int":
        v = e.get("v")
        if isinstance(v, str):
            try:
                return int(v.strip(), 10)
            except ValueError:
                raise ProtocolError("bad int literal %r" % (v,))
        if isinstance(v, bool) or not isinstance(v, int):
            if isinstance(v, float) and v.is_integer():
                return int(v)
            raise ProtocolError("bad int value %r" % (v,))
        return int(v)
    if k == "float":
        return _float_from(e.get("v"))
    if k == "str":
        v = e.get("v")
        if not isinstance(v, str):
            raise ProtocolError("str value must be a JSON string")
        return v
    if k == "bytes":
        v = e.get("v") or ""
        try:
            return base64.b64decode(v.encode("ascii"), validate=False)
        except BaseException:
            raise ProtocolError("bad base64 payload")
    if k == "list":
        v = e.get("v") or []
        if not isinstance(v, list):
            raise ProtocolError("list payload must be a JSON array")
        return [dec(x) for x in v]
    if k == "dict":
        v = e.get("v") or []
        if not isinstance(v, list):
            raise ProtocolError("dict payload must be a JSON array of pairs")
        out = {}
        for pair in v:
            if not isinstance(pair, (list, tuple)) or len(pair) != 2:
                raise ProtocolError("dict entry must be [key, value]")
            key = dec(pair[0])
            try:
                out[key] = dec(pair[1])
            except TypeError:
                raise ProtocolError("unhashable dict key %r" % (key,))
        return out
    if k in ("obj", "call", "h"):  # "h"：zuzo 侧把句柄回传（{"k":"h","id":N}）
        return lookup(e)
    if k is None:
        raise ProtocolError("encoded value missing 'k'")
    raise ProtocolError("unknown value kind %r" % (k,))


# ---------------------------------------------------------------------------
# native：尽力转成"原生可读"值再 enc
# ---------------------------------------------------------------------------

_PLAIN_DEPTH = 8


def _plain(x, depth: int = 0):
    """把 DataFrame 单元格这类东西压成 JSON 友好的纯 Python 值。"""
    if x is None or isinstance(x, (bool, int, float, str)):
        return x
    if depth >= _PLAIN_DEPTH:
        return str(x)
    if isinstance(x, (bytes, bytearray, memoryview)):
        return base64.b64encode(bytes(x)).decode("ascii")
    if _is_numpy(x):
        shape = getattr(x, "shape", None)
        if shape == ():
            try:
                return _plain(x.item(), depth + 1)
            except BaseException:
                return str(x)
        try:
            return _plain(x.tolist(), depth + 1)
        except BaseException:
            return str(x)
    if isinstance(x, dict):
        return {str(k): _plain(v, depth + 1) for k, v in x.items()}
    if isinstance(x, (list, tuple, set, frozenset, range)):
        return [_plain(v, depth + 1) for v in x]
    for attr in ("isoformat",):  # datetime / date / time / Timestamp
        fn = getattr(x, attr, None)
        if callable(fn):
            try:
                return fn()
            except BaseException:
                pass
    try:
        s = repr(x)
    except BaseException:
        s = "<%s>" % type(x).__name__
    return s[:MAX_REPR]


def to_native(v):
    """尽力把对象转成原生结构（ndarray->list、DataFrame->records、Series->list ...）。"""
    if v is None or isinstance(v, (bool, int, float, str)):
        return v
    if isinstance(v, (bytes, bytearray, memoryview)):
        return bytes(v)

    if _is_numpy(v):
        shape = getattr(v, "shape", None)
        if shape == ():
            try:
                item = v.item()
                if item is not v:
                    return to_native(item)
            except BaseException:
                pass
        elif shape is not None:
            # native 是用户显式要求「转成原生值」，不再受 enc 内联阈值限制；
            # 只有转换本身失败（OOM / 不支持的 dtype）才退回句柄。
            try:
                return _plain(v.tolist())
            except BaseException:
                return v

    mod = (getattr(type(v), "__module__", "") or "")
    if mod.split(".", 1)[0] == "pandas":
        # DataFrame -> [ {col: val}, ... ]；Series -> [ ... ]
        if hasattr(v, "columns") and hasattr(v, "to_dict"):
            try:
                records = v.to_dict(orient="records")
                return [_plain(r) for r in records]
            except BaseException:
                pass
        tolist = getattr(v, "tolist", None)
        if callable(tolist):
            try:
                return _plain(tolist())
            except BaseException:
                pass

    if isinstance(v, set):
        return [_plain(x) for x in v]
    if isinstance(v, (tuple, range, frozenset)):
        return [_plain(x) for x in v]
    if isinstance(v, list):
        return [_plain(x) for x in v]
    if isinstance(v, dict):
        return {str(k): _plain(x) for k, x in v.items()}

    tolist = getattr(v, "tolist", None)  # array.array / 自定义类
    if callable(tolist) and not isinstance(v, (str, bytes)):
        try:
            return _plain(tolist())
        except BaseException:
            pass

    return v


# ---------------------------------------------------------------------------
# native 的两种模式：records（默认，丢索引）/ records+index（索引 + 记录）
# ---------------------------------------------------------------------------

NATIVE_MODES = ("records", "records+index")

def _records_with_index(v) -> dict:
    """DataFrame → {"索引": [索引值...], "记录": [{列: 值}...]}（groupby 的城市索引不再丢）。

    索引与记录都过 _plain，保证 JSON 可序列化（numpy 标量 / Timestamp 都压成原生值）。
    """
    if not (hasattr(v, "columns") and hasattr(v, "to_dict")):
        raise ProtocolError("records+index 模式需要 DataFrame 类对象，得到 %s"
                            % _type_name(v))
    try:
        raw = v.to_dict(orient="records")
    except BaseException as e:
        raise ProtocolError("records+index 取记录失败: %s: %s"
                            % (type(e).__name__, e))
    records = [_plain(r) for r in raw]
    index = []
    try:
        for key in v.index:
            index.append(_plain(key))
    except BaseException as e:
        raise ProtocolError("records+index 取索引失败: %s: %s"
                            % (type(e).__name__, e))
    return {"索引": index, "记录": records}

def to_native_mode(v, mode=None):
    """按 mode 转原生值：records（默认）/ records+index（索引 + 记录）。"""
    if mode is None or mode == "" or mode == "records":
        return to_native(v)
    if mode == "records+index":
        return _records_with_index(v)
    raise ProtocolError("unknown native mode %r（可用：records / records+index）"
                        % (mode,))

# ---------------------------------------------------------------------------
# 常驻 globals（eval / exec 共享）
# ---------------------------------------------------------------------------

SESSION = uuid.uuid4().hex

GLOBALS = {
    "__name__": "__pybridge__",
    "__doc__": None,
    "__builtins__": builtins,
    "__package__": None,
    # Go 侧 py取值(句柄, "records"/"records+index") 的通道：
    # client.go 的 Native() 只有 id 形参（本轮禁改 client.go / proto.go），
    # 因此 Go 侧把句柄放进 globals 槽，再 eval 调这个助手，与 op_native(mode=…) 同一实现。
    "__pybridge_native__": to_native_mode,
}


# ---------------------------------------------------------------------------
# 属性解析
# ---------------------------------------------------------------------------

def resolve_attr(obj, name: str):
    if not isinstance(name, str):
        raise ProtocolError("name must be a string")
    if name == "":
        return obj
    cur = obj
    for part in name.split("."):
        if not part:
            continue
        try:
            cur = getattr(cur, part)
            continue
        except AttributeError:
            pass
        # 容器兜底：dict 键 / list 下标
        if isinstance(cur, dict) and part in cur:
            cur = cur[part]
            continue
        if isinstance(cur, (list, tuple)):
            try:
                cur = cur[int(part)]
                continue
            except (ValueError, IndexError):
                pass
        raise AttributeError("'%s' object has no attribute '%s'"
                             % (_type_name(cur), part))
    return cur


# ---------------------------------------------------------------------------
# ops
# ---------------------------------------------------------------------------

def op_hello(req):
    # session 用于识别「子进程重启」：Go 侧拿它给句柄打戳，重启后旧句柄一律判失效，
    # 避免 ABA（新进程句柄表从 1 重新分配，旧 ID 指向完全不同的对象）。
    return {
        "py": platform.python_version(),
        "exe": sys.executable or "",
        "impl": platform.python_implementation(),
        "session": SESSION,
    }


def op_ping(req):
    return {"k": "str", "v": "pong"}


def op_import(req):
    name = req.get("name")
    if not isinstance(name, str) or not name:
        raise ProtocolError("import requires non-empty 'name'")
    try:
        mod = importlib.import_module(name)
    except ModuleNotFoundError:
        # pip 刚装完时常驻进程的 finder 缓存可能还看不到 → 失效缓存重试一次
        importlib.invalidate_caches()
        mod = importlib.import_module(name)
    return enc(mod)


def op_get(req):
    obj = lookup(req)
    return enc(resolve_attr(obj, req.get("name", "")))


def op_set(req):
    obj = lookup(req)
    name = req.get("name")
    if not isinstance(name, str) or not name:
        raise ProtocolError("set requires non-empty 'name'")
    value = dec(req.get("v"))
    head, _, tail = name.rpartition(".")
    target = resolve_attr(obj, head) if head else obj
    if not tail:
        raise ProtocolError("set requires an attribute name")
    try:
        setattr(target, tail, value)
    except AttributeError:
        # 属性写不进去 → 退回下标赋值（DataFrame 新列 / dict 键）
        if isinstance(target, dict):
            target[tail] = value
        else:
            target[tail] = value
    return {"k": "none"}


def op_call(req):
    fn = lookup(req)
    if not callable(fn):
        raise TypeError("%r is not callable" % (_type_name(fn),))
    args_enc = req.get("args") or []
    kw_enc = req.get("kwargs") or {}
    if not isinstance(args_enc, list):
        raise ProtocolError("'args' must be an array of enc values")
    if not isinstance(kw_enc, dict):
        raise ProtocolError("'kwargs' must be an object")
    args = [dec(a) for a in args_enc]
    kwargs = {str(k): dec(v) for k, v in kw_enc.items()}
    return enc(fn(*args, **kwargs))


def op_eval(req):
    expr = req.get("expr")
    if not isinstance(expr, str):
        raise ProtocolError("eval requires string 'expr'")
    return enc(eval(compile(expr, "<hy-eval>", "eval"), GLOBALS))


def op_exec(req):
    code = req.get("code")
    if not isinstance(code, str):
        raise ProtocolError("exec requires string 'code'")
    exec(compile(code, "<hy-exec>", "exec"), GLOBALS)
    return {"k": "none"}


def op_repr(req):
    obj = lookup(req)
    try:
        s = repr(obj)
    except BaseException as e:
        raise RuntimeError("repr failed: %s: %s" % (type(e).__name__, e))
    return {"k": "str", "v": s}


def op_native(req):
    """native op：mode 缺省/"records" 同旧行为；"records+index" 额外返回索引。"""
    obj = lookup(req)
    return enc(to_native_mode(obj, req.get("mode")))


def op_release(req):
    hid = handle_id(req)
    try:
        hid = int(hid)
    except (TypeError, ValueError):
        raise ProtocolError("release requires integer 'id'")
    if hid == 0:  # 0 = globals 伪句柄，不可释放
        raise ProtocolError("cannot release the globals pseudo-handle")
    _handles.pop(hid, None)  # 幂等：重复释放不报错
    return {"k": "none"}


def op_has(req):
    name = req.get("name")
    if not isinstance(name, str) or not name:
        raise ProtocolError("has requires non-empty 'name'")
    found = False
    try:
        found = importlib.util.find_spec(name) is not None
    except BaseException:
        found = False
    return {"k": "bool", "v": found}


def op_release_all(req):
    _handles.clear()
    return {"k": "none"}


def op_stats(req):
    return {"k": "int", "v": str(len(_handles))}


def op_shutdown(req):
    raise ShutdownSignal()


def op_getglobal(req):
    """按名字取常驻 globals 里的值（zuzo py获取 的降级通道）。"""
    name = req.get("name")
    if not isinstance(name, str) or not name:
        raise ProtocolError("getglobal requires non-empty 'name'")
    if name not in GLOBALS:
        raise NameError("name '%s' is not defined" % name)
    return enc(GLOBALS[name])


def op_setglobal(req):
    """按名字写常驻 globals（zuzo py设置 的降级通道）。"""
    name = req.get("name")
    if not isinstance(name, str) or not name:
        raise ProtocolError("setglobal requires non-empty 'name'")
    GLOBALS[name] = dec(req.get("v"))
    return {"k": "none"}


OPS = {
    "hello": op_hello,
    "ping": op_ping,
    "import": op_import,
    "get": op_get,
    "set": op_set,
    "call": op_call,
    "eval": op_eval,
    "exec": op_exec,
    "repr": op_repr,
    "native": op_native,
    "release": op_release,
    "has": op_has,
    "getglobal": op_getglobal,
    "setglobal": op_setglobal,
    "stats": op_stats,
    "shutdown": op_shutdown,
    "getglobal": op_getglobal,
    "setglobal": op_setglobal,
}


# ---------------------------------------------------------------------------
# 请求分发
# ---------------------------------------------------------------------------

def error_payload(exc: BaseException) -> dict:
    try:
        tb = "".join(traceback.format_exception(
            type(exc), exc, exc.__traceback__))[:MAX_TB]
    except BaseException:
        tb = ""
    try:
        msg = str(exc)
    except BaseException:
        msg = "<unprintable message>"
    return {"t": type(exc).__name__, "m": msg, "tb": tb}


def handle_request(req) -> tuple[dict, bool]:
    """处理一条已解析的请求，返回 (响应 dict, 是否 shutdown)。"""
    if not isinstance(req, dict):
        return {
            "id": None, "ok": False,
            "e": {"t": "ProtocolError", "m": "request must be a JSON object", "tb": ""},
        }, False
    rid = req_id(req)
    op = req.get("op")
    try:
        handler = OPS.get(op)
        if handler is None:
            raise UnknownOp("unknown op %r" % (op,))
        value = handler(req)
        return {"id": rid, "ok": True, "v": value}, False
    except ShutdownSignal:
        return {"id": rid, "ok": True, "v": {"k": "none"}}, True
    except BaseException as exc:  # 用户代码的 SystemExit 也不能弄死服务
        return {"id": rid, "ok": False, "e": error_payload(exc)}, False


def serve() -> int:
    while True:
        try:
            line = _real_in.readline()
        except KeyboardInterrupt:
            return 0
        except BaseException:
            return 1
        if not line:
            return 0
        text = line.decode("utf-8", "replace").strip()
        if not text:
            continue
        try:
            req = json.loads(text)
        except BaseException as exc:
            emit({"id": None, "ok": False,
                  "e": {"t": "ProtocolError",
                        "m": "invalid JSON: %s" % exc, "tb": ""}})
            continue
        resp, stop = handle_request(req)
        try:
            emit(resp)
        except BaseException:
            emit({"id": resp.get("id"), "ok": False,
                  "e": {"t": "EncodeError",
                        "m": "response is not JSON-serializable", "tb": ""}})
        if stop:
            return 0


# ---------------------------------------------------------------------------
# 自检
# ---------------------------------------------------------------------------

def _selftest() -> int:
    checks: list[tuple[str, bool, str]] = []

    def check(name: str, cond: bool, detail: str = "") -> None:
        checks.append((name, bool(cond), detail))

    def call(op: str, **kw) -> dict:
        req = {"id": len(checks) + 1, "op": op}
        req.update(kw)
        resp, _ = handle_request(req)
        try:  # 每条响应都必须可 JSON 序列化
            json.dumps(resp, ensure_ascii=False)
        except BaseException as exc:
            check("%s:json" % op, False, str(exc))
        return resp

    def out(resp: dict) -> dict:
        return resp.get("v") or {}

    # 1. hello
    r = call("hello")
    v = out(r)
    check("hello.ok", r.get("ok") is True and bool(v.get("py"))
          and bool(v.get("exe")) and v.get("impl") == "CPython", str(r)[:160])

    # 2. ping
    r = call("ping")
    check("ping.pong", out(r) == {"k": "str", "v": "pong"}, str(r)[:160])

    # 3. import json
    r = call("import", name="json")
    v = out(r)
    json_handle = v.get("id")
    check("import.json", r.get("ok") is True and v.get("k") == "obj"
          and isinstance(json_handle, int) and v.get("t") == "module", str(r)[:160])

    # 4. get json.dumps -> callable handle
    r = call("get", hid=json_handle, name="dumps")
    v = out(r)
    dumps_handle = v.get("id")
    check("get.dumps", r.get("ok") is True and v.get("k") == "call"
          and isinstance(dumps_handle, int), str(r)[:160])

    # 5. call json.dumps("x")
    r = call("call", hid=dumps_handle, args=[{"k": "str", "v": "x"}], kwargs={})
    check("call.dumps", out(r) == {"k": "str", "v": '"x"'}, str(r)[:160])

    # 6. eval 整数
    r = call("eval", expr="1+2")
    check("eval.int", out(r) == {"k": "int", "v": "3"}, str(r)[:160])

    # 7. eval 大整数（十进制字符串，不丢精度）
    r = call("eval", expr="10**30")
    check("eval.bigint", out(r) == {"k": "int", "v": str(10 ** 30)}, str(r)[:160])

    # 8. eval 浮点 / 字符串 / bytes / none
    r = call("eval", expr="1.5+1")
    check("eval.float", out(r).get("k") == "float"
          and abs(out(r).get("v") - 2.5) < 1e-12, str(r)[:160])
    r = call("eval", expr="'abc'")
    check("eval.str", out(r) == {"k": "str", "v": "abc"}, str(r)[:160])
    r = call("eval", expr="b'hi'")
    check("eval.bytes", out(r) == {"k": "bytes", "v": "aGk="}, str(r)[:160])
    r = call("eval", expr="None")
    check("eval.none", out(r) == {"k": "none"}, str(r)[:160])

    # 9. exec 写常驻 globals，eval 读回
    r = call("exec", code="x = 41")
    ok_exec = r.get("ok") is True and out(r) == {"k": "none"}
    r = call("eval", expr="x+1")
    check("exec.globals", ok_exec and out(r) == {"k": "int", "v": "42"},
          str(r)[:160])

    # 10. dict 编码
    r = call("eval", expr="{'a': 1, 'b': [True, None]}")
    v = out(r)
    ok_dict = (v.get("k") == "dict" and isinstance(v.get("v"), list)
               and len(v["v"]) == 2
               and v["v"][0][0] == {"k": "str", "v": "a"}
               and v["v"][0][1] == {"k": "int", "v": "1"})
    check("eval.dict", ok_dict, str(r)[:200])

    # 11. native：array.array -> list
    call("exec", code="import array")
    r = call("eval", expr="array.array('i', [1, 2, 3])")
    v = out(r)
    arr_handle = v.get("id")
    ok_eval_arr = r.get("ok") is True and v.get("k") == "obj"
    r = call("native", hid=arr_handle)
    check("native.array", ok_eval_arr and out(r) == {
        "k": "list", "v": [{"k": "int", "v": "1"},
                           {"k": "int", "v": "2"},
                           {"k": "int", "v": "3"}]}, str(r)[:200])

    # 12. native：numpy（有则测，无则跳过计过）
    r = call("has", name="numpy")
    if out(r).get("v") is True:
        r = call("exec", code="import numpy as _np")
        r = call("eval", expr="_np.arange(4)")
        v = out(r)
        if v.get("k") == "list":
            check("native.numpy", v.get("v") and len(v["v"]) == 4, str(r)[:200])
        else:
            r = call("native", hid=v.get("id"))
            got = out(r).get("v") or []
            check("native.numpy", len(got) == 4
                  and got[0] == {"k": "int", "v": "0"}, str(r)[:200])
        # 大数组走句柄
        r = call("eval", expr="_np.zeros(200001)")
        check("enc.bigarray_handle", out(r).get("k") == "obj", str(r)[:160])
    else:
        check("native.numpy", True, "numpy 未安装，跳过")

    # 13. release 后句柄失效
    r = call("release", hid=arr_handle)
    ok_rel = r.get("ok") is True and out(r) == {"k": "none"}
    r = call("get", hid=arr_handle, name="__class__")
    check("release.stale", ok_rel and r.get("ok") is False
          and (r.get("e") or {}).get("t") == "HandleError", str(r)[:160])

    # 14. 错误路径：不存在的模块
    r = call("import", name="zzz_no_such_module_20261101")
    e = r.get("e") or {}
    check("error.import", r.get("ok") is False
          and e.get("t") == "ModuleNotFoundError"
          and "zzz_no_such_module_20261101" in (e.get("m") or "")
          and "Traceback" in (e.get("tb") or ""), str(r)[:200])

    # 15. 错误路径：未知 op / 非法 JSON 结构
    r = call("no_such_op")
    check("error.unknown_op", r.get("ok") is False
          and (r.get("e") or {}).get("t") == "UnknownOp", str(r)[:160])
    resp, _ = handle_request({"id": 999, "op": "eval", "expr": "("})
    check("error.eval_syntax", resp.get("ok") is False
          and (resp.get("e") or {}).get("t") == "SyntaxError", str(resp)[:160])

    # 16. 常驻 globals 读写 op（Go 侧 Lookup / SetName 依赖）
    r = call("setglobal", name="gg", v={"k": "int", "v": "7"})
    ok_sg = r.get("ok") is True
    r = call("getglobal", name="gg")
    check("global.rw", ok_sg and out(r) == {"k": "int", "v": "7"}, str(r)[:160])
    r = call("getglobal", name="zzz_missing_name")
    check("global.missing", r.get("ok") is False
          and (r.get("e") or {}).get("t") in ("NameError", "KeyError"), str(r)[:160])

    # 17. zuzo 回传句柄 {"k":"h","id":N}
    r = call("eval", expr="len")
    len_handle = out(r).get("id")
    r = call("eval", expr="__import__('array').array('i', [1, 2, 3])")
    arr2 = out(r).get("id")
    r = call("call", hid=len_handle, args=[{"k": "h", "id": arr2}], kwargs={})
    check("dec.handle_h", out(r) == {"k": "int", "v": "3"}, str(r)[:160])

    # 18. 响应 id 回显：带 hid 时保留请求号
    resp, _ = handle_request({"id": 4242, "op": "repr", "hid": json_handle})
    check("echo.req_id", resp.get("id") == 4242 and resp.get("ok") is True,
          str(resp)[:160])

    # 19. has 探测
    r = call("has", name="json")
    ok_has_json = out(r) == {"k": "bool", "v": True}
    r = call("has", name="zzz_no_such_module_20261101")
    ok_has_no = out(r) == {"k": "bool", "v": False}
    check("has.spec", ok_has_json and ok_has_no, "")

    # 20. native 的 records+index：DataFrame groupby 的索引不能丢（有 pandas 才测）
    r = call("has", name="pandas")
    if out(r).get("v") is True:
        call("exec", code="import pandas as _pd")
        call("exec", code=("_df_gi = _pd.DataFrame({'城市': ['北京','上海','上海'], "
                             "'销量': [3, 5, 2]}).groupby('城市').sum()"))
        r = call("eval", expr="_df_gi")
        gi_handle = out(r).get("id")
        # 默认（无 mode）/ "records"：旧行为，只有记录
        r = call("native", hid=gi_handle)
        # 注意：pandas groupby 默认 sort=True，索引按城市名字典序（上海 < 北京）
        check("native.records_default", out(r) == {
            "k": "list",
            "v": [{"k": "dict", "v": [[{"k": "str", "v": "销量"},
                                         {"k": "int", "v": "7"}]]},
                  {"k": "dict", "v": [[{"k": "str", "v": "销量"},
                                         {"k": "int", "v": "3"}]]}]}, str(r)[:300])
        r = call("native", hid=gi_handle, mode="records+index")
        v = out(r)
        got = {}
        ok_shape = v.get("k") == "dict" and isinstance(v.get("v"), list)
        if ok_shape:
            for pair in v["v"]:
                got[dec(pair[0])] = dec(pair[1])
        check("native.records_index",
              ok_shape and [k for k in got] == ["索引", "记录"]
              and got.get("索引") == ["上海", "北京"]
              and got.get("记录") == [{"销量": 7}, {"销量": 3}], str(r)[:400])
        # 未知 mode 要报 ProtocolError，不能静默降级
        r = call("native", hid=gi_handle, mode="records+index+xx")
        check("native.bad_mode", r.get("ok") is False
              and (r.get("e") or {}).get("t") == "ProtocolError", str(r)[:200])
        # Go 侧通道：globals 助手 __pybridge_native__（eval 路径）
        r = call("eval", expr="__pybridge_native__(_df_gi, 'records+index')")
        v = out(r)
        got2 = {}
        if isinstance(v.get("v"), list):
            for pair in v["v"]:
                got2[dec(pair[0])] = dec(pair[1])
        check("native.eval_channel", got2.get("索引") == ["上海", "北京"]
              and got2.get("记录") == [{"销量": 7}, {"销量": 3}], str(r)[:400])
    else:
        check("native.records_index", True, "pandas 未安装，跳过")
        check("native.eval_channel", True, "pandas 未安装，跳过")

    # 21. LRU：超限释放最旧句柄（ZUZO_PY_MAX_HANDLES=3 注册 5 个只剩 3 个）
    _saved_limit = os.environ.get("ZUZO_PY_MAX_HANDLES")
    _saved_handles = list(_handles.items())
    os.environ["ZUZO_PY_MAX_HANDLES"] = "3"
    _handles.clear()
    lru_ids = [register(object()) for _ in range(5)]
    ok_cap = (len(_handles) == 3 and list(_handles.keys()) == lru_ids[2:]
              and _evicted >= 2)
    stale_ok = False
    try:
        lookup({"hid": lru_ids[0]})
    except HandleError:
        stale_ok = True
    check("lru.evict", ok_cap and stale_ok,
          "len=%d ids=%r" % (len(_handles), list(_handles.keys())))

    # 22. LRU：访问过的句柄变成最近使用，不会被下一次超限淘汰
    _handles.clear()
    a_id = register("a")
    b_id = register("b")
    c_id = register("c")
    _ = lookup({"hid": a_id})   # 访问 a → a 变最新（b 成了最旧）
    d_id = register("d")          # 超限 → 淘汰 b
    check("lru.touch", list(_handles.keys()) == [c_id, a_id, d_id]
          and b_id not in _handles and a_id in _handles,
          "ids=%r" % (list(_handles.keys()),))

    # 恢复现场（后面的 case 不再依赖句柄表）
    _handles.clear()
    _handles.update(_saved_handles)
    if _saved_limit is None:
        os.environ.pop("ZUZO_PY_MAX_HANDLES", None)
    else:
        os.environ["ZUZO_PY_MAX_HANDLES"] = _saved_limit

    # 输出
    passed = 0
    for name, ok, detail in checks:
        if ok:
            passed += 1
            line = "  ok   %s" % name
        else:
            line = "  FAIL %s  %s" % (name, detail)
        _real_out.write(line.encode("utf-8", "replace") + b"\n")
    _real_out.flush()
    total = len(checks)
    if passed == total:
        _real_out.write(("SELFTEST PASS %d\n" % passed).encode("ascii"))
        _real_out.flush()
        return 0
    _real_out.write(("SELFTEST FAIL %d/%d\n" % (passed, total)).encode("ascii"))
    _real_out.flush()
    return 1


def main(argv: list[str]) -> int:
    if "--selftest" in argv:
        return _selftest()
    return serve()


if __name__ == "__main__":
    try:
        sys.exit(main(sys.argv[1:]))
    except KeyboardInterrupt:
        sys.exit(0)
    except BrokenPipeError:
        sys.exit(0)
