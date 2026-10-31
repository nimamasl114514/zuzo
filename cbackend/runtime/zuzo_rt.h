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

/* ── 输出缓冲（Inspect 公开 API）── */
typedef struct { char* data; int64_t len; int64_t cap; } ZBuf;
void buf_init(ZBuf* b);
void z_inspect(ZBuf* b, ZVal v);

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

/* ── 数组/哈希内置 ─────────────────────────────────── */
ZVal z_pop(ZVal arr);
ZVal z_first(ZVal arr);
ZVal z_last(ZVal arr);
ZVal z_keys(ZVal h);
ZVal z_get(ZVal h, ZVal k, ZVal defv, int hasDef);

/* ── 数学内置 ─────────────────────────────────────── */
ZVal z_abs(ZVal v);
ZVal z_max2(ZVal a, ZVal b);
ZVal z_min2(ZVal a, ZVal b);
ZVal z_ceil(ZVal v);
ZVal z_floor(ZVal v);
ZVal z_round(ZVal v);
ZVal z_pow(ZVal a, ZVal b);
ZVal z_sqrt(ZVal v);
ZVal z_sin(ZVal v);
ZVal z_cos(ZVal v);
ZVal z_log(ZVal v);
ZVal z_rand(void);
ZVal z_rand_int(ZVal lo, ZVal hi);

/* ── 字符串全局 ───────────────────────────────────── */
ZVal z_count_sub(ZVal v, ZVal sub);
ZVal z_char_at(ZVal v, ZVal idx);
ZVal z_fields(ZVal v);
ZVal z_trim_chars(ZVal v, ZVal set);

/* ── JSON ─────────────────────────────────────────── */
ZVal z_json_parse(ZVal s);
ZVal z_json_stringify(ZVal v);

/* ── 文件 I/O ─────────────────────────────────────── */
ZVal z_read_file(ZVal p);
ZVal z_write_file(ZVal p, ZVal c);
ZVal z_append_file(ZVal p, ZVal c);
ZVal z_file_exists(ZVal p);
ZVal z_create_file(ZVal p);
ZVal z_delete_file(ZVal p);
ZVal z_rename_file(ZVal a, ZVal b);

/* ── 编码 ─────────────────────────────────────────── */
ZVal z_base64_encode(ZVal v);
ZVal z_base64_decode(ZVal v);
ZVal z_hex_encode(ZVal v);
ZVal z_hex_decode(ZVal v);
ZVal z_url_encode(ZVal v);
ZVal z_url_decode(ZVal v);

/* ── 时间 ─────────────────────────────────────────── */
ZVal z_unix_time(void);
ZVal z_unix_milli(void);
ZVal z_now(void);
ZVal z_sleep_ms(ZVal v);

/* ── 正则 ─────────────────────────────────────────── */
ZVal z_regex_test(ZVal s, ZVal p);
ZVal z_regex_match(ZVal s, ZVal p);
ZVal z_regex_find_all(ZVal s, ZVal p);
ZVal z_regex_replace(ZVal s, ZVal p, ZVal r);
ZVal z_regex_split(ZVal s, ZVal p);
ZVal z_regex_extract(ZVal s, ZVal p);

/* ── 文本扩展（ASCII 语义）────────────────────────── */
ZVal zs_capitalize(ZVal v);
ZVal zs_title(ZVal v);
ZVal zs_swapcase(ZVal v);
ZVal zs_lstrip(ZVal v);
ZVal zs_rstrip(ZVal v);
ZVal zs_remove_prefix(ZVal v, ZVal p);
ZVal zs_remove_suffix(ZVal v, ZVal p);
ZVal zs_replace_n(ZVal v, ZVal o, ZVal n, ZVal cnt);
ZVal zs_split_n(ZVal v, ZVal sep, ZVal n);
ZVal zs_ljust(ZVal v, ZVal w, ZVal f);
ZVal zs_rjust(ZVal v, ZVal w, ZVal f);
ZVal zs_center(ZVal v, ZVal w, ZVal f);

/* ── 摘要 ─────────────────────────────────────────── */
ZVal z_md5(ZVal v);
ZVal z_sha1(ZVal v);
ZVal z_sha256(ZVal v);

/* ── 格式化 / 目录 / 环境 ─────────────────────────── */
ZVal z_format(int argc, ZVal* argv);
ZVal z_read_dir(ZVal p);
ZVal z_getenv(ZVal p);

/* ── 诊断 ─────────────────────────────────────────────── */
void z_die(const char* code, const char* msg);
void z_check_arith(int overflow, const char* op);
void z_check_div(int64_t divisor);

#ifdef __cplusplus
}
#endif

#endif /* ZUZO_RT_H */
