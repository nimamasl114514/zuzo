/* zuzo_rt.h —— zuzo 语言 C 运行时（第21轮 · C 后端）
 *
 * 设计口径：
 *  - 值模型 = tagged union（ZVal），与解释器 object.Object 的动态类型语义一致；
 *  - 堆对象（字符串/数组/哈希/函数）本轮不做回收，程序退出即释放（第23轮再加 RC）；
 *  - 所有 API 前缀 z_，避免与宿主 C 代码符号冲突。
 */
#ifndef ZUZO_RT_H
#define ZUZO_RT_H

#include <stdint.h>
#include <stddef.h>
#include <setjmp.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef enum {
    ZK_NULL = 0,
    ZK_INT,
    ZK_FLOAT,
    ZK_STR,
    ZK_BOOL,
    ZK_ARRAY,
    ZK_HASH,
    ZK_FUNC
} ZKind;

/* 前置声明（ZVal 只持有它们的指针，故不完整类型即可） */
typedef struct ZVal   ZVal;
typedef struct ZStr   ZStr;
typedef struct ZArr   ZArr;
typedef struct ZHash  ZHash;
typedef struct ZFunc  ZFunc;

typedef ZVal (*ZFnPtr)(int argc, ZVal* argv, void* env);

/* 值：16 字节（kind + union） */
struct ZVal {
    ZKind kind;
    union {
        int64_t i;
        double  f;
        ZStr*   s;
        int     b;
        ZArr*   a;
        ZHash*  h;
        ZFunc*  fn;
    } as;
};

struct ZStr {
    int64_t len;   /* 字节数 */
    char*   data;  /* UTF-8，NUL 结尾 */
};

struct ZArr {
    int64_t len;
    int64_t cap;
    ZVal*   items;
};

typedef struct ZHashEntry {
    ZVal key;
    ZVal val;
    int  used;
} ZHashEntry;

struct ZHash {
    int64_t     len;
    int64_t     cap;
    ZHashEntry* entries;
};

struct ZFunc {
    ZFnPtr      fn;
    void*       env;
    const char* name;
};

/* 闭包捕获的堆变量（共享可变状态） */
typedef struct ZBox {
    ZVal v;
} ZBox;

/* ── 构造器 ───────────────────────────────────────────── */
ZVal z_null(void);
ZVal z_int(int64_t v);
ZVal z_float(double v);
ZVal z_bool(int v);
ZVal z_str(const char* s);
ZVal z_str_len(const char* s, int64_t n);
ZVal z_str_take(char* heap, int64_t n);

/* ── 判定 ─────────────────────────────────────────────── */
int  z_truthy(ZVal v);
int  z_is_null(ZVal v);
const char* z_type_name(ZVal v);

/* ── 算术与比较（语义对齐 evaluator）──────────────────── */
ZVal z_add(ZVal a, ZVal b);
ZVal z_sub(ZVal a, ZVal b);
ZVal z_mul(ZVal a, ZVal b);
ZVal z_div(ZVal a, ZVal b);
ZVal z_mod(ZVal a, ZVal b);
ZVal z_neg(ZVal a);
ZVal z_not(ZVal a);

ZVal z_lt(ZVal a, ZVal b);
ZVal z_le(ZVal a, ZVal b);
ZVal z_gt(ZVal a, ZVal b);
ZVal z_ge(ZVal a, ZVal b);
ZVal z_eq(ZVal a, ZVal b);
ZVal z_ne(ZVal a, ZVal b);

/* ── 打印（与 object.Inspect() 口径一致）─────────────── */
void z_print(int argc, ZVal* argv);
void z_println(void);

/* ── 数组 ─────────────────────────────────────────────── */
ZVal z_array(void);
ZVal z_array_with(int64_t n, ZVal* items);
void z_array_push(ZVal arr, ZVal v);
ZVal z_index_get(ZVal obj, ZVal idx);
void z_index_set(ZVal obj, ZVal idx, ZVal v);

/* ── 哈希 ─────────────────────────────────────────────── */
ZVal z_hash(void);
void z_hash_set(ZVal h, ZVal k, ZVal v);
ZVal z_hash_get(ZVal h, ZVal k);

/* ── 内置函数 ─────────────────────────────────────────── */
ZVal z_len(ZVal v);
ZVal z_to_str(ZVal v);
ZVal z_to_int(ZVal v);
ZVal z_to_float(ZVal v);
ZVal z_type_of(ZVal v);
ZVal z_join(ZVal arr, ZVal sep);
ZVal z_concat(int n, ZVal* parts);
ZVal z_contains(ZVal container, ZVal v);

/* ── 闭包与函数值 ─────────────────────────────── */
ZBox* z_box(ZVal v);
void* z_new_env(size_t n);
ZVal  z_closure(ZFnPtr fn, void* env, const char* name);
void* z_fn_env(ZVal f);
ZVal  z_call(ZVal f, int argc, ZVal* argv);

/* ── 运行时初始化（Windows 下 stdout 置二进制模式，对齐解释器 LF）── */
void z_rt_init(void);

/* ── 错误捕获（执行/遇到）───────────────────────────── */
typedef struct ZTryFrame {
    jmp_buf buf;
    struct ZTryFrame* prev;
} ZTryFrame;

extern ZTryFrame* z_try_top;
extern ZVal z_error_value;
ZVal z_error_take(void);

/* ── 文本命名空间（第22轮核心子集）─────────────── */
ZVal zs_upper(ZVal s);
ZVal zs_lower(ZVal s);
ZVal zs_trim(ZVal s);
ZVal zs_replace(ZVal s, ZVal oldv, ZVal newv);
ZVal zs_split(ZVal s, ZVal sep);
ZVal zs_join_arr(ZVal arr, ZVal sep);
ZVal zs_repeat(ZVal s, ZVal n);
ZVal zs_find(ZVal s, ZVal sub);
ZVal zs_contains(ZVal s, ZVal sub);
ZVal zs_starts(ZVal s, ZVal prefix);
ZVal zs_ends(ZVal s, ZVal suffix);
ZVal zs_compare(ZVal a, ZVal b);
ZVal zs_substr(ZVal s, ZVal start, ZVal stop);
ZVal zs_lines(ZVal s);

/* ── 诊断 ─────────────────────────────────────────────── */
void z_die(const char* code, const char* msg);
void z_check_arith(int overflow, const char* op);
void z_check_div(int64_t divisor);

#ifdef __cplusplus
}
#endif

#endif /* ZUZO_RT_H */
