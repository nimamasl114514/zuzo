/* zuzo_rt.c —— zuzo 语言 C 运行时（第21轮 · C 后端）
 *
 * 语义对齐对象：evaluator/evaluator.go 的 evalInfixExpression / isTruthy /
 *               builtinPrint / builtinLen / builtinJoin / evalArrayIndexExpression
 */
#include "zuzo_rt.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>

#ifdef _WIN32
#include <fcntl.h>
#include <io.h>
#endif

/* 解释器（Go）输出 LF；C 运行时在 Windows 默认文本模式会把 \n 转成 \r\n。
 * 统一为二进制模式，保证逐字节对拍一致。 */
void z_rt_init(void) {
#ifdef _WIN32
    _setmode(_fileno(stdout), _O_BINARY);
    _setmode(_fileno(stderr), _O_BINARY);
#endif
}

/* ══════════════════════════════════════════════════════════
 * 内部工具
 * ══════════════════════════════════════════════════════════ */

static void* z_alloc(size_t n) {
    void* p = malloc(n);
    if (!p) {
        fputs("zuzo: 内存分配失败\n", stderr);
        exit(1);
    }
    return p;
}

static void* z_realloc(void* p, size_t n) {
    void* q = realloc(p, n);
    if (!q) {
        fputs("zuzo: 内存分配失败\n", stderr);
        exit(1);
    }
    return q;
}

/* UTF-8 码点计数（长度() 对字符串的口径：rune 数，不是字节数） */
static int64_t z_utf8_len(const char* s, int64_t bytes) {
    int64_t n = 0;
    for (int64_t i = 0; i < bytes; ) {
        unsigned char c = (unsigned char)s[i];
        if (c < 0x80) i += 1;
        else if ((c >> 5) == 0x06) i += 2;
        else if ((c >> 4) == 0x0E) i += 3;
        else if ((c >> 3) == 0x1E) i += 4;
        else i += 1;
        n++;
    }
    return n;
}

/* ══════════════════════════════════════════════════════════
 * 诊断
 * ══════════════════════════════════════════════════════════ */

ZTryFrame* z_try_top = NULL;
ZVal z_error_value;

ZVal z_error_take(void) { return z_error_value; }

void z_die(const char* code, const char* msg) {
    char buf[1024];
    if (code && code[0]) snprintf(buf, sizeof(buf), "错误 %s: %s", code, msg);
    else                 snprintf(buf, sizeof(buf), "错误: %s", msg);
    if (z_try_top != NULL) {
        /* 有 `执行/遇到` 帧：抛成错误对象并跳转，不终止进程 */
        z_error_value = z_str(buf);
        longjmp(z_try_top->buf, 1);
    }
    fputs(buf, stderr);
    fputc('\n', stderr);
    exit(1);
}

void z_check_arith(int overflow, const char* op) {
    if (overflow) {
        char buf[128];
        snprintf(buf, sizeof(buf), "整数溢出：%s 结果超出 int64 表示范围", op);
        z_die("E210", buf);
    }
}

void z_check_div(int64_t divisor) {
    if (divisor == 0) z_die("E211", "除零错误");
}

/* ══════════════════════════════════════════════════════════
 * 构造器
 * ══════════════════════════════════════════════════════════ */

ZVal z_null(void) {
    ZVal v; v.kind = ZK_NULL; v.as.i = 0; return v;
}

ZVal z_int(int64_t x) {
    ZVal v; v.kind = ZK_INT; v.as.i = x; return v;
}

ZVal z_float(double x) {
    ZVal v; v.kind = ZK_FLOAT; v.as.f = x; return v;
}

ZVal z_bool(int x) {
    ZVal v; v.kind = ZK_BOOL; v.as.b = x ? 1 : 0; return v;
}

ZVal z_str_take(char* heap, int64_t n) {
    ZStr* s = (ZStr*)z_alloc(sizeof(ZStr));
    s->len = n;
    s->data = heap;
    ZVal v; v.kind = ZK_STR; v.as.s = s; return v;
}

ZVal z_str_len(const char* src, int64_t n) {
    char* buf = (char*)z_alloc((size_t)n + 1);
    if (n > 0 && src) memcpy(buf, src, (size_t)n);
    buf[n] = '\0';
    return z_str_take(buf, n);
}

ZVal z_str(const char* src) {
    if (!src) return z_str_len("", 0);
    return z_str_len(src, (int64_t)strlen(src));
}

/* ══════════════════════════════════════════════════════════
 * 判定
 * ══════════════════════════════════════════════════════════ */

int z_is_null(ZVal v) { return v.kind == ZK_NULL; }

const char* z_type_name(ZVal v) {
    switch (v.kind) {
        case ZK_NULL:  return "空";
        case ZK_INT:   return "整数";
        case ZK_FLOAT: return "浮点数";
        case ZK_STR:   return "字符串";
        case ZK_BOOL:  return "布尔";
        case ZK_ARRAY: return "数组";
        case ZK_HASH:  return "哈希";
        case ZK_FUNC:  return "函数";
    }
    return "未知";
}

/* 与 evaluator.isTruthy 完全一致：空/假/0/0.0/""/空数组/空哈希 = 假 */
int z_truthy(ZVal v) {
    switch (v.kind) {
        case ZK_NULL:  return 0;
        case ZK_BOOL:  return v.as.b != 0;
        case ZK_INT:   return v.as.i != 0;
        case ZK_FLOAT: return v.as.f != 0.0;
        case ZK_STR:   return v.as.s->len != 0;
        case ZK_ARRAY: return v.as.a->len > 0;
        case ZK_HASH:  return v.as.h->len > 0;
        default:       return 1;
    }
}

/* ══════════════════════════════════════════════════════════
 * Inspect（打印口径）
 * ══════════════════════════════════════════════════════════ */

typedef struct { char* data; int64_t len; int64_t cap; } ZBuf;

static void buf_init(ZBuf* b) { b->cap = 64; b->len = 0; b->data = (char*)z_alloc(64); b->data[0] = 0; }

static void buf_reserve(ZBuf* b, int64_t need) {
    if (b->len + need + 1 <= b->cap) return;
    while (b->len + need + 1 > b->cap) b->cap *= 2;
    b->data = (char*)z_realloc(b->data, (size_t)b->cap);
}

static void buf_puts(ZBuf* b, const char* s) {
    int64_t n = (int64_t)strlen(s);
    buf_reserve(b, n);
    memcpy(b->data + b->len, s, (size_t)n);
    b->len += n;
    b->data[b->len] = 0;
}

static void buf_putn(ZBuf* b, const char* s, int64_t n) {
    buf_reserve(b, n);
    if (n > 0) memcpy(b->data + b->len, s, (size_t)n);
    b->len += n;
    b->data[b->len] = 0;
}

static void buf_putc(ZBuf* b, char c) {
    buf_reserve(b, 1);
    b->data[b->len++] = c;
    b->data[b->len] = 0;
}

/* Go strconv.FormatFloat(x, 'f', -1, 64) 的等价实现：
 * 最短往返十进制表示，不用指数。用 %.17g 后修剪，再验证往返。 */
static void z_format_float(double x, char* out, size_t cap) {
    if (isnan(x)) { snprintf(out, cap, "NaN"); return; }
    if (isinf(x)) { snprintf(out, cap, x > 0 ? "+Inf" : "-Inf"); return; }
    for (int prec = 1; prec <= 17; prec++) {
        snprintf(out, cap, "%.*g", prec, x);
        double back = strtod(out, NULL);
        if (back == x) return;
    }
    snprintf(out, cap, "%.17g", x);
}

static void z_inspect(ZBuf* b, ZVal v);

static void z_inspect_str(ZBuf* b, const char* s) {
    buf_puts(b, s);
}

static void z_inspect(ZBuf* b, ZVal v) {
    char tmp[64];
    switch (v.kind) {
        case ZK_NULL:  buf_puts(b, "空"); break;
        case ZK_INT:
            snprintf(tmp, sizeof(tmp), "%lld", (long long)v.as.i);
            buf_puts(b, tmp); break;
        case ZK_FLOAT:
            z_format_float(v.as.f, tmp, sizeof(tmp));
            buf_puts(b, tmp); break;
        case ZK_BOOL:  buf_puts(b, v.as.b ? "真" : "假"); break;
        case ZK_STR:   z_inspect_str(b, v.as.s->data); break;
        case ZK_ARRAY: {
            buf_putc(b, '[');
            for (int64_t i = 0; i < v.as.a->len; i++) {
                if (i) buf_puts(b, ", ");
                z_inspect(b, v.as.a->items[i]);
            }
            buf_putc(b, ']');
            break;
        }
        case ZK_HASH: {
            buf_putc(b, '{');
            for (int64_t i = 0; i < v.as.h->len; i++) {
                if (i) buf_puts(b, ", ");
                z_inspect(b, v.as.h->entries[i].key);
                buf_puts(b, ": ");
                z_inspect(b, v.as.h->entries[i].val);
            }
            buf_putc(b, '}');
            break;
        }
        case ZK_FUNC:
            buf_puts(b, "函数");
            break;
    }
}

/* ══════════════════════════════════════════════════════════
 * 打印
 * ══════════════════════════════════════════════════════════ */

void z_print(int argc, ZVal* argv) {
    ZBuf b; buf_init(&b);
    for (int i = 0; i < argc; i++) {
        if (i) buf_putc(&b, ' ');
        z_inspect(&b, argv[i]);
    }
    fwrite(b.data, 1, (size_t)b.len, stdout);
    fputc('\n', stdout);
    free(b.data);
}

void z_println(void) { fputc('\n', stdout); }

/* ══════════════════════════════════════════════════════════
 * 数值提升
 * ══════════════════════════════════════════════════════════ */

static double z_as_float(ZVal v) {
    if (v.kind == ZK_INT) return (double)v.as.i;
    if (v.kind == ZK_FLOAT) return v.as.f;
    z_die("E202", "期望数值类型");
    return 0;
}

static int z_is_num(ZVal v) { return v.kind == ZK_INT || v.kind == ZK_FLOAT; }

/* ══════════════════════════════════════════════════════════
 * 算术（整数溢出按 E210 口径）
 * ══════════════════════════════════════════════════════════ */

static int add_overflow(int64_t a, int64_t b, int64_t* r) {
    if (__builtin_add_overflow(a, b, r)) return 1;
    return 0;
}

ZVal z_add(ZVal a, ZVal b) {
    if (a.kind == ZK_INT && b.kind == ZK_INT) {
        int64_t r;
        if (add_overflow(a.as.i, b.as.i, &r)) z_die("E210", "整数溢出：+ 结果超出 int64 表示范围");
        return z_int(r);
    }
    if (z_is_num(a) && z_is_num(b)) return z_float(z_as_float(a) + z_as_float(b));
    if (a.kind == ZK_STR && b.kind == ZK_STR) {
        int64_t n = a.as.s->len + b.as.s->len;
        char* p = (char*)z_alloc((size_t)n + 1);
        memcpy(p, a.as.s->data, (size_t)a.as.s->len);
        memcpy(p + a.as.s->len, b.as.s->data, (size_t)b.as.s->len);
        p[n] = 0;
        return z_str_take(p, n);
    }
    z_die("E203", "运算符 '+' 不支持类型");
    return z_null();
}

ZVal z_sub(ZVal a, ZVal b) {
    if (a.kind == ZK_INT && b.kind == ZK_INT) {
        int64_t r;
        if (__builtin_sub_overflow(a.as.i, b.as.i, &r)) z_die("E210", "整数溢出：- 结果超出 int64 表示范围");
        return z_int(r);
    }
    if (z_is_num(a) && z_is_num(b)) return z_float(z_as_float(a) - z_as_float(b));
    z_die("E203", "运算符 '-' 不支持类型");
    return z_null();
}

ZVal z_mul(ZVal a, ZVal b) {
    if (a.kind == ZK_INT && b.kind == ZK_INT) {
        int64_t r;
        if (__builtin_mul_overflow(a.as.i, b.as.i, &r)) z_die("E210", "整数溢出：* 结果超出 int64 表示范围");
        return z_int(r);
    }
    if (z_is_num(a) && z_is_num(b)) return z_float(z_as_float(a) * z_as_float(b));
    z_die("E203", "运算符 '*' 不支持类型");
    return z_null();
}

ZVal z_div(ZVal a, ZVal b) {
    if (a.kind == ZK_INT && b.kind == ZK_INT) {
        if (b.as.i == 0) z_die("E211", "除零错误");
        if (a.as.i == INT64_MIN && b.as.i == -1) z_die("E210", "整数溢出");
        return z_int(a.as.i / b.as.i);
    }
    if (z_is_num(a) && z_is_num(b)) {
        double y = z_as_float(b);
        if (y == 0.0) z_die("E211", "除零错误");
        return z_float(z_as_float(a) / y);
    }
    z_die("E203", "运算符 '/' 不支持类型");
    return z_null();
}

ZVal z_mod(ZVal a, ZVal b) {
    if (a.kind == ZK_INT && b.kind == ZK_INT) {
        if (b.as.i == 0) z_die("E212", "取模零错误");
        if (a.as.i == INT64_MIN && b.as.i == -1) z_die("E210", "整数溢出");
        return z_int(a.as.i % b.as.i);
    }
    if (z_is_num(a) && z_is_num(b)) {
        double y = z_as_float(b);
        if (y == 0.0) z_die("E212", "取模零错误");
        return z_float(fmod(z_as_float(a), y));
    }
    z_die("E203", "运算符 '%' 不支持类型");
    return z_null();
}

ZVal z_neg(ZVal a) {
    if (a.kind == ZK_INT) {
        if (a.as.i == INT64_MIN) z_die("E210", "整数溢出");
        return z_int(-a.as.i);
    }
    if (a.kind == ZK_FLOAT) return z_float(-a.as.f);
    z_die("E203", "一元 '-' 不支持类型");
    return z_null();
}

ZVal z_not(ZVal a) { return z_bool(!z_truthy(a)); }

/* ══════════════════════════════════════════════════════════
 * 比较
 * ══════════════════════════════════════════════════════════ */

static int z_str_cmp(ZStr* a, ZStr* b) {
    int64_t n = a->len < b->len ? a->len : b->len;
    int c = memcmp(a->data, b->data, (size_t)n);
    if (c != 0) return c;
    if (a->len == b->len) return 0;
    return a->len < b->len ? -1 : 1;
}

static int z_cmp(ZVal a, ZVal b) {
    if (z_is_num(a) && z_is_num(b)) {
        double x = z_as_float(a), y = z_as_float(b);
        if (x < y) return -1;
        if (x > y) return 1;
        return 0;
    }
    if (a.kind == ZK_STR && b.kind == ZK_STR) return z_str_cmp(a.as.s, b.as.s);
    z_die("E203", "比较运算不支持类型");
    return 0;
}

ZVal z_lt(ZVal a, ZVal b) { return z_bool(z_cmp(a, b) < 0); }
ZVal z_le(ZVal a, ZVal b) { return z_bool(z_cmp(a, b) <= 0); }
ZVal z_gt(ZVal a, ZVal b) { return z_bool(z_cmp(a, b) > 0); }
ZVal z_ge(ZVal a, ZVal b) { return z_bool(z_cmp(a, b) >= 0); }

/* ==：数值跨 int/float 比较，字符串按内容，其余按类型+值；跨大类恒不等 */
ZVal z_eq(ZVal a, ZVal b) {
    if (z_is_num(a) && z_is_num(b)) return z_bool(z_as_float(a) == z_as_float(b));
    if (a.kind != b.kind) return z_bool(0);
    switch (a.kind) {
        case ZK_NULL:  return z_bool(1);
        case ZK_BOOL:  return z_bool(a.as.b == b.as.b);
        case ZK_STR:   return z_bool(z_str_cmp(a.as.s, b.as.s) == 0);
        case ZK_INT:   return z_bool(a.as.i == b.as.i);
        case ZK_FLOAT: return z_bool(a.as.f == b.as.f);
        case ZK_ARRAY:
        case ZK_HASH:
        case ZK_FUNC:  return z_bool(a.as.a == b.as.a);
    }
    return z_bool(0);
}

ZVal z_ne(ZVal a, ZVal b) {
    ZVal r = z_eq(a, b);
    return z_bool(!r.as.b);
}

/* ══════════════════════════════════════════════════════════
 * 数组
 * ══════════════════════════════════════════════════════════ */

static ZArr* z_arr_new(int64_t cap) {
    ZArr* a = (ZArr*)z_alloc(sizeof(ZArr));
    a->len = 0;
    a->cap = cap > 0 ? cap : 4;
    a->items = (ZVal*)z_alloc(sizeof(ZVal) * (size_t)a->cap);
    return a;
}

ZVal z_array(void) {
    ZVal v; v.kind = ZK_ARRAY; v.as.a = z_arr_new(4); return v;
}

ZVal z_array_with(int64_t n, ZVal* items) {
    ZArr* a = z_arr_new(n > 0 ? n : 1);
    for (int64_t i = 0; i < n; i++) a->items[i] = items[i];
    a->len = n;
    ZVal v; v.kind = ZK_ARRAY; v.as.a = a; return v;
}

void z_array_push(ZVal arr, ZVal x) {
    ZArr* a = arr.as.a;
    if (a->len == a->cap) {
        a->cap *= 2;
        a->items = (ZVal*)z_realloc(a->items, sizeof(ZVal) * (size_t)a->cap);
    }
    a->items[a->len++] = x;
}

/* ══════════════════════════════════════════════════════════
 * 哈希（插入顺序保序 · 线性查找；数据量大时第23轮换开放寻址）
 * ══════════════════════════════════════════════════════════ */

ZVal z_hash(void) {
    ZHash* h = (ZHash*)z_alloc(sizeof(ZHash));
    h->len = 0;
    h->cap = 4;
    h->entries = (ZHashEntry*)z_alloc(sizeof(ZHashEntry) * (size_t)h->cap);
    ZVal v; v.kind = ZK_HASH; v.as.h = h; return v;
}

static int z_key_eq(ZVal a, ZVal b) {
    if (a.kind != b.kind) {
        /* 数值键跨 int/float 视为同键（与解释器 HashKey 类型区分不同，这里取宽松口径） */
        if (z_is_num(a) && z_is_num(b)) return z_as_float(a) == z_as_float(b);
        return 0;
    }
    switch (a.kind) {
        case ZK_INT:   return a.as.i == b.as.i;
        case ZK_FLOAT: return a.as.f == b.as.f;
        case ZK_BOOL:  return a.as.b == b.as.b;
        case ZK_STR:   return z_str_cmp(a.as.s, b.as.s) == 0;
        default:       return 0;
    }
}

static int64_t z_hash_find(ZHash* h, ZVal k) {
    for (int64_t i = 0; i < h->len; i++)
        if (z_key_eq(h->entries[i].key, k)) return i;
    return -1;
}

void z_hash_set(ZVal hv, ZVal k, ZVal v) {
    ZHash* h = hv.as.h;
    int64_t i = z_hash_find(h, k);
    if (i >= 0) { h->entries[i].val = v; return; }
    if (h->len == h->cap) {
        h->cap *= 2;
        h->entries = (ZHashEntry*)z_realloc(h->entries, sizeof(ZHashEntry) * (size_t)h->cap);
    }
    h->entries[h->len].key = k;
    h->entries[h->len].val = v;
    h->entries[h->len].used = 1;
    h->len++;
}

ZVal z_hash_get(ZVal hv, ZVal k) {
    ZHash* h = hv.as.h;
    int64_t i = z_hash_find(h, k);
    if (i < 0) return z_null();
    return h->entries[i].val;
}

/* ══════════════════════════════════════════════════════════
 * 下标
 * ══════════════════════════════════════════════════════════ */

ZVal z_index_get(ZVal obj, ZVal idx) {
    if (obj.kind == ZK_ARRAY && idx.kind == ZK_INT) {
        if (idx.as.i < 0 || idx.as.i >= obj.as.a->len) return z_null();
        return obj.as.a->items[idx.as.i];
    }
    if (obj.kind == ZK_HASH) return z_hash_get(obj, idx);
    z_die("E205", "不支持对该类型使用索引操作");
    return z_null();
}

void z_index_set(ZVal obj, ZVal idx, ZVal v) {
    if (obj.kind == ZK_ARRAY && idx.kind == ZK_INT) {
        if (idx.as.i < 0 || idx.as.i >= obj.as.a->len) z_die("E206", "数组下标越界");
        obj.as.a->items[idx.as.i] = v;
        return;
    }
    if (obj.kind == ZK_HASH) { z_hash_set(obj, idx, v); return; }
    z_die("E205", "不支持对该类型使用索引赋值");
}

/* ══════════════════════════════════════════════════════════
 * 内置函数
 * ══════════════════════════════════════════════════════════ */

ZVal z_len(ZVal v) {
    if (v.kind == ZK_STR)   return z_int(z_utf8_len(v.as.s->data, v.as.s->len));
    if (v.kind == ZK_ARRAY) return z_int(v.as.a->len);
    z_die("E207", "长度() 不支持该类型");
    return z_null();
}

ZVal z_to_str(ZVal v) {
    ZBuf b; buf_init(&b);
    z_inspect(&b, v);
    ZVal r = z_str_take(b.data, b.len);
    return r;
}

ZVal z_to_int(ZVal v) {
    if (v.kind == ZK_INT)   return v;
    if (v.kind == ZK_FLOAT) return z_int((int64_t)v.as.f);
    if (v.kind == ZK_STR)   return z_int(strtoll(v.as.s->data, NULL, 10));
    if (v.kind == ZK_BOOL)  return z_int(v.as.b ? 1 : 0);
    z_die("E208", "整数() 不支持该类型");
    return z_null();
}

ZVal z_to_float(ZVal v) {
    if (v.kind == ZK_FLOAT) return v;
    if (v.kind == ZK_INT)   return z_float((double)v.as.i);
    if (v.kind == ZK_STR)   return z_float(strtod(v.as.s->data, NULL));
    z_die("E209", "浮点数() 不支持该类型");
    return z_null();
}

ZVal z_type_of(ZVal v) { return z_str(z_type_name(v)); }

ZVal z_join(ZVal arr, ZVal sep) {
    if (arr.kind != ZK_ARRAY) z_die("E213", "连接() 第一个参数必须是数组");
    ZBuf b; buf_init(&b);
    for (int64_t i = 0; i < arr.as.a->len; i++) {
        if (i && sep.kind == ZK_STR) buf_putn(&b, sep.as.s->data, sep.as.s->len);
        ZVal s = z_to_str(arr.as.a->items[i]);
        buf_putn(&b, s.as.s->data, s.as.s->len);
    }
    return z_str_take(b.data, b.len);
}

/* ══════════════════════════════════════════════════════════
 * 闭包与函数值
 * ══════════════════════════════════════════════════════════ */

ZBox* z_box(ZVal v) {
    ZBox* b = (ZBox*)z_alloc(sizeof(ZBox));
    b->v = v;
    return b;
}

void* z_new_env(size_t n) {
    if (n < sizeof(void*)) n = sizeof(void*);
    return z_alloc(n);
}

ZVal z_closure(ZFnPtr fn, void* env, const char* name) {
    ZFunc* f = (ZFunc*)z_alloc(sizeof(ZFunc));
    f->fn = fn;
    f->env = env;
    f->name = name;
    ZVal v; v.kind = ZK_FUNC; v.as.fn = f; return v;
}

void* z_fn_env(ZVal f) {
    if (f.kind != ZK_FUNC) return NULL;
    return f.as.fn->env;
}

ZVal z_call(ZVal f, int argc, ZVal* argv) {
    if (f.kind != ZK_FUNC) z_die("E215", "调用目标不是函数");
    if (!f.as.fn->fn) z_die("E215", "函数指针为空");
    return f.as.fn->fn(argc, argv, f.as.fn->env);
}

/* f-string 拼接：各段按 Inspect 口径转字符串后连接 */
ZVal z_concat(int n, ZVal* parts) {
    ZBuf b; buf_init(&b);
    for (int i = 0; i < n; i++) {
        ZVal s = z_to_str(parts[i]);
        buf_putn(&b, s.as.s->data, s.as.s->len);
    }
    return z_str_take(b.data, b.len);
}

ZVal z_contains(ZVal container, ZVal v) {
    if (container.kind == ZK_ARRAY) {
        for (int64_t i = 0; i < container.as.a->len; i++)
            if (z_truthy(z_eq(container.as.a->items[i], v))) return z_bool(1);
        return z_bool(0);
    }
    if (container.kind == ZK_STR && v.kind == ZK_STR)
        return z_bool(strstr(container.as.s->data, v.as.s->data) != NULL);
    if (container.kind == ZK_HASH) return z_bool(z_hash_find(container.as.h, v) >= 0);
    z_die("E214", "包含() 不支持该类型");
    return z_null();
}

/* ══════════════════════════════════════════════════════════
 * 文本命名空间（核心子集 · ASCII 语义，UTF-8 字节安全）
 * ══════════════════════════════════════════════════════════ */

static ZStr* zs_arg(ZVal v, const char* fn) {
    if (v.kind != ZK_STR) {
        char buf[128];
        snprintf(buf, sizeof(buf), "%s() 参数必须是字符串", fn);
        z_die("E216", buf);
    }
    return v.as.s;
}

static int64_t zs_byte_to_rune_index(const char* s, int64_t bytes, int64_t bytePos) {
    return z_utf8_len(s, bytePos);
}

ZVal zs_upper(ZVal v) {
    ZStr* s = zs_arg(v, "大写");
    char* p = (char*)z_alloc((size_t)s->len + 1);
    for (int64_t i = 0; i < s->len; i++) {
        char c = s->data[i];
        p[i] = (c >= 'a' && c <= 'z') ? (char)(c - 32) : c;
    }
    p[s->len] = 0;
    return z_str_take(p, s->len);
}

ZVal zs_lower(ZVal v) {
    ZStr* s = zs_arg(v, "小写");
    char* p = (char*)z_alloc((size_t)s->len + 1);
    for (int64_t i = 0; i < s->len; i++) {
        char c = s->data[i];
        p[i] = (c >= 'A' && c <= 'Z') ? (char)(c + 32) : c;
    }
    p[s->len] = 0;
    return z_str_take(p, s->len);
}

static int zs_is_space(char c) {
    return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f';
}

ZVal zs_trim(ZVal v) {
    ZStr* s = zs_arg(v, "去空白");
    int64_t a = 0, b = s->len;
    while (a < b && zs_is_space(s->data[a])) a++;
    while (b > a && zs_is_space(s->data[b - 1])) b--;
    return z_str_len(s->data + a, b - a);
}

ZVal zs_replace(ZVal v, ZVal oldv, ZVal newv) {
    ZStr* s = zs_arg(v, "替换");
    ZStr* o = zs_arg(oldv, "替换");
    ZStr* n = zs_arg(newv, "替换");
    if (o->len == 0) return v;
    ZBuf b; buf_init(&b);
    int64_t i = 0;
    while (i < s->len) {
        if (i + o->len <= s->len && memcmp(s->data + i, o->data, (size_t)o->len) == 0) {
            buf_putn(&b, n->data, n->len);
            i += o->len;
        } else {
            buf_putc(&b, s->data[i]);
            i++;
        }
    }
    return z_str_take(b.data, b.len);
}

ZVal zs_split(ZVal v, ZVal sepv) {
    ZStr* s = zs_arg(v, "分割");
    ZStr* sep = zs_arg(sepv, "分割");
    ZVal out = z_array();
    if (sep->len == 0) {
        /* 空分隔符：逐字符（按 UTF-8 码点）切 */
        int64_t i = 0;
        while (i < s->len) {
            int64_t w = 1;
            unsigned char c = (unsigned char)s->data[i];
            if (c >= 0xF0) w = 4; else if (c >= 0xE0) w = 3; else if (c >= 0xC0) w = 2;
            z_array_push(out, z_str_len(s->data + i, w));
            i += w;
        }
        return out;
    }
    int64_t start = 0, i = 0;
    while (i < s->len) {
        if (i + sep->len <= s->len && memcmp(s->data + i, sep->data, (size_t)sep->len) == 0) {
            z_array_push(out, z_str_len(s->data + start, i - start));
            i += sep->len;
            start = i;
        } else {
            i++;
        }
    }
    z_array_push(out, z_str_len(s->data + start, s->len - start));
    return out;
}

ZVal zs_join_arr(ZVal arr, ZVal sepv) { return z_join(arr, sepv); }

ZVal zs_repeat(ZVal v, ZVal nv) {
    ZStr* s = zs_arg(v, "重复");
    int64_t n = (nv.kind == ZK_INT) ? nv.as.i : 0;
    if (n <= 0) return z_str_len("", 0);
    int64_t total = s->len * n;
    char* p = (char*)z_alloc((size_t)total + 1);
    for (int64_t k = 0; k < n; k++) memcpy(p + k * s->len, s->data, (size_t)s->len);
    p[total] = 0;
    return z_str_take(p, total);
}

ZVal zs_find(ZVal v, ZVal subv) {
    ZStr* s = zs_arg(v, "查找");
    ZStr* sub = zs_arg(subv, "查找");
    if (sub->len == 0) return z_int(0);
    for (int64_t i = 0; i + sub->len <= s->len; i++) {
        if (memcmp(s->data + i, sub->data, (size_t)sub->len) == 0)
            return z_int(zs_byte_to_rune_index(s->data, s->len, i));
    }
    return z_int(-1);
}

ZVal zs_contains(ZVal v, ZVal subv) {
    ZVal r = zs_find(v, subv);
    return z_bool(r.as.i >= 0);
}

ZVal zs_starts(ZVal v, ZVal pv) {
    ZStr* s = zs_arg(v, "前缀");
    ZStr* p = zs_arg(pv, "前缀");
    if (p->len > s->len) return z_bool(0);
    return z_bool(memcmp(s->data, p->data, (size_t)p->len) == 0);
}

ZVal zs_ends(ZVal v, ZVal pv) {
    ZStr* s = zs_arg(v, "后缀");
    ZStr* p = zs_arg(pv, "后缀");
    if (p->len > s->len) return z_bool(0);
    return z_bool(memcmp(s->data + (s->len - p->len), p->data, (size_t)p->len) == 0);
}

ZVal zs_compare(ZVal a, ZVal b) {
    ZStr* x = zs_arg(a, "比较");
    ZStr* y = zs_arg(b, "比较");
    return z_int(z_str_cmp(x, y) < 0 ? -1 : (z_str_cmp(x, y) > 0 ? 1 : 0));
}

ZVal zs_substr(ZVal v, ZVal startv, ZVal stopv) {
    ZStr* s = zs_arg(v, "子串");
    int64_t runes = z_utf8_len(s->data, s->len);
    int64_t start = (startv.kind == ZK_INT) ? startv.as.i : 0;
    int64_t stop = (stopv.kind == ZK_INT) ? stopv.as.i : runes;
    if (start < 0) start = 0;
    if (stop > runes) stop = runes;
    if (start >= stop) return z_str_len("", 0);
    /* rune 索引 → 字节索引 */
    int64_t byteStart = -1, byteStop = -1, ri = 0, bi = 0;
    if (start == 0) byteStart = 0;
    while (bi < s->len && (byteStart < 0 || byteStop < 0)) {
        if (ri == start) byteStart = bi;
        if (ri == stop) { byteStop = bi; break; }
        unsigned char c = (unsigned char)s->data[bi];
        int64_t w = 1;
        if (c >= 0xF0) w = 4; else if (c >= 0xE0) w = 3; else if (c >= 0xC0) w = 2;
        bi += w; ri++;
    }
    if (byteStart < 0) byteStart = s->len;
    if (byteStop < 0) byteStop = s->len;
    return z_str_len(s->data + byteStart, byteStop - byteStart);
}

ZVal zs_lines(ZVal v) {
    ZStr* s = zs_arg(v, "按行分割");
    ZVal out = z_array();
    int64_t start = 0, i = 0;
    while (i < s->len) {
        if (s->data[i] == '\n' || s->data[i] == '\r') {
            z_array_push(out, z_str_len(s->data + start, i - start));
            if (s->data[i] == '\r' && i + 1 < s->len && s->data[i + 1] == '\n') i++;
            i++;
            start = i;
        } else {
            i++;
        }
    }
    if (start < s->len) z_array_push(out, z_str_len(s->data + start, s->len - start));
    return out;
}
