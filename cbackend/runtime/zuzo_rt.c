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
#include <time.h>

#ifdef _WIN32
#include <fcntl.h>
#include <io.h>
#include <windows.h>
#endif

/* 解释器（Go）输出 LF；C 运行时在 Windows 默认文本模式会把 \n 转成 \r\n。
 * 统一为二进制模式，保证逐字节对拍一致。 */
/* 严格模式开关：默认与解释器一致（越界/缺键返回 空）；
   编译时加 --strict 会调用 z_set_strict(1)，改为直接报错中止。 */
int z_strict_mode = 0;
void z_set_strict(int v) { z_strict_mode = v; }

void z_rt_init(void) {
    z_gc_init();
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
/* ══════════════════════════════════════════════════════════
 * 内存回收：精确根集 mark-sweep（第27轮）
 *   - 对象链：z_obj_new 统一挂链，sweep 一次线性扫描；
 *   - 根集：生成代码在安全点显式传「当前作用域 ZVal 变量地址表 + ZBox* 表 + env」；
 *   - 触发：仅 z_gc_safepoint()，且 live_bytes >= threshold 时；
 *   - 阈值：初始 4MB，回收后 max(4MB, 2 * live)。
 * ══════════════════════════════════════════════════════════ */

static ZObj*  z_gc_head = NULL;
static size_t z_gc_live_bytes = 0;
static size_t z_gc_total_bytes = 0;
static size_t z_gc_threshold = (size_t)4 << 20;
static long   z_gc_collections = 0;
static long   z_gc_objects = 0;
static int    z_gc_on = 1;

static ZObj** z_mark_stack = NULL;
static size_t z_mark_cap = 0;
static size_t z_mark_len = 0;

static void z_mark_push(ZObj* o);
static void z_mark_val(ZVal v);

static void* z_obj_new(uint32_t kind, size_t size) {
    ZObj* o = (ZObj*)malloc(size);
    if (!o) {
        fputs("zuzo: 内存分配失败\n", stderr);
        exit(1);
    }
    /* 逐字段清零：不依赖 memset（nativecc 对多参外部调用的传参回归验证用） */
    {
        unsigned char* p = (unsigned char*)o;
        size_t k;
        for (k = 0; k < size; k++) p[k] = 0;
    }
    o->next = z_gc_head;
    o->kind = kind;
    o->mark = 0;
    o->size = size;
    z_gc_head = o;
    z_gc_live_bytes += size;
    z_gc_total_bytes += size;
    z_gc_objects++;
    return o;
}

static void z_mark_push(ZObj* o) {
    if (!o || o->mark) return;
    o->mark = 1;
    if (z_mark_len == z_mark_cap) {
        size_t cap = z_mark_cap ? z_mark_cap * 2 : 256;
        ZObj** s = (ZObj**)realloc(z_mark_stack, cap * sizeof(ZObj*));
        if (!s) {
            fputs("zuzo: 内存分配失败\n", stderr);
            exit(1);
        }
        z_mark_stack = s;
        z_mark_cap = cap;
    }
    z_mark_stack[z_mark_len++] = o;
}

static void z_mark_val(ZVal v) {
    switch (v.kind) {
    case ZK_STR:   z_mark_push((ZObj*)v.as.s); break;
    case ZK_ARRAY: z_mark_push((ZObj*)v.as.a); break;
    case ZK_HASH:  z_mark_push((ZObj*)v.as.h); break;
    case ZK_FUNC:  z_mark_push((ZObj*)v.as.fn); break;
    default: break;
    }
}

static void z_mark_children(ZObj* o) {
    switch (o->kind) {
    case ZOK_ARR: {
        ZArr* a = (ZArr*)o;
        for (int64_t i = 0; i < a->len; i++) z_mark_val(a->items[i]);
        break;
    }
    case ZOK_HASH: {
        ZHash* h = (ZHash*)o;
        for (int64_t i = 0; i < h->len; i++) {
            z_mark_val(h->entries[i].key);
            z_mark_val(h->entries[i].val);
        }
        break;
    }
    case ZOK_BOX:
        z_mark_val(((ZBox*)o)->v);
        break;
    case ZOK_FUNC: {
        ZFunc* f = (ZFunc*)o;
        if (f->env) z_mark_push((ZObj*)((ZEnvHead*)f->env - 1));
        break;
    }
    case ZOK_ENV: {
        ZEnvHead* hd = (ZEnvHead*)o;
        if (hd->box_off) {
            char* base = (char*)(hd + 1);
            for (int i = 0; i < hd->nbox; i++) {
                ZBox* b = *(ZBox**)(base + hd->box_off[i]);
                if (b) z_mark_push((ZObj*)b);
            }
        }
        break;
    }
    default: break;
    }
}

static void z_gc_sweep(void) {
    ZObj** pp = &z_gc_head;
    size_t live = 0;
    while (*pp) {
        ZObj* o = *pp;
        if (o->mark) {
            o->mark = 0;
            live += o->size;
            pp = &o->next;
            continue;
        }
        *pp = o->next;
        if (o->kind == ZOK_STR) free(((ZStr*)o)->data);
        else if (o->kind == ZOK_ARR) free(((ZArr*)o)->items);
        else if (o->kind == ZOK_HASH) free(((ZHash*)o)->entries);
        else if (o->kind == ZOK_ENV) free(((ZEnvHead*)o)->box_off);
        /* ZOK_FUNC/ZOK_BOX/ZOK_ENV 无附属缓冲 */
        free(o);
        z_gc_objects--;
    }
    z_gc_live_bytes = live;
    {
        size_t floor_bytes = (size_t)4 << 20;
        size_t next = live * 2;
        z_gc_threshold = next > floor_bytes ? next : floor_bytes;
    }
}

static void z_gc_run(int nvals, ZVal** vals, int nboxes, ZBox** boxes, void* env) {
    z_mark_len = 0;
    for (int i = 0; i < nvals; i++) if (vals[i]) z_mark_val(*vals[i]);
    for (int i = 0; i < nboxes; i++) if (boxes[i]) z_mark_push((ZObj*)boxes[i]);
    if (env) z_mark_push((ZObj*)((ZEnvHead*)env - 1));
    z_mark_val(z_error_value);            /* 执行/遇到 传递中的错误值 */
    while (z_mark_len > 0) {
        ZObj* o = z_mark_stack[--z_mark_len];
        z_mark_children(o);
    }
    z_gc_sweep();
    z_gc_collections++;
}

void z_gc_init(void) {
    const char* off = getenv("ZUZO_GC");
    if (off && (off[0] == '0' || off[0] == 'o' || off[0] == 'O')) z_gc_on = 0;
}

void z_gc_set_enabled(int on) { z_gc_on = on ? 1 : 0; }
int  z_gc_enabled(void) { return z_gc_on; }

void z_gc_set_threshold(size_t bytes) {
    z_gc_threshold = bytes ? bytes : ((size_t)4 << 20);
}

void z_gc_collect_now(void) {
    /* 无根集的强制回收（仅测试夹具/泄漏判定用；生产路径只走 safepoint） */
    z_gc_run(0, NULL, 0, NULL, NULL);
}

void z_gc_stats(size_t* live_bytes, size_t* total_bytes, long* collections, long* objects) {
    if (live_bytes) *live_bytes = z_gc_live_bytes;
    if (total_bytes) *total_bytes = z_gc_total_bytes;
    if (collections) *collections = z_gc_collections;
    if (objects) *objects = z_gc_objects;
}

void z_gc_safepoint(int nvals, ZVal** vals, int nboxes, ZBox** boxes, void* env) {
    if (!z_gc_on) return;
    if (z_gc_live_bytes < z_gc_threshold) return;
    z_gc_run(nvals, vals, nboxes, boxes, env);
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
    ZStr* s = (ZStr*)z_obj_new(ZOK_STR, sizeof(ZStr));
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

/* ZBuf 定义已公开到 zuzo_rt.h */

void buf_init(ZBuf* b) { b->cap = 64; b->len = 0; b->data = (char*)z_alloc(64); b->data[0] = 0; }

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

void z_inspect(ZBuf* b, ZVal v);

static void z_inspect_str(ZBuf* b, const char* s) {
    buf_puts(b, s);
}

void z_inspect(ZBuf* b, ZVal v) {
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
    ZArr* a = (ZArr*)z_obj_new(ZOK_ARR, sizeof(ZArr));
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
    ZHash* h = (ZHash*)z_obj_new(ZOK_HASH, sizeof(ZHash));
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
    if (i < 0) {
        if (z_strict_mode) z_die("E209", "哈希键不存在");
        return z_null();
    }
    return h->entries[i].val;
}

/* ══════════════════════════════════════════════════════════
 * 下标
 * ══════════════════════════════════════════════════════════ */

/* 按 UTF-8 码点下标取单个字符（负下标从尾部计），返回新字符串 */
static ZVal z_str_char_at(ZVal obj, int64_t target) {
    const char* s = obj.as.s->data;
    int64_t bytes = obj.as.s->len;
    int64_t n = z_utf8_len(s, bytes);
    if (target < 0) target += n;
    if (target < 0 || target >= n) {
        if (z_strict_mode) z_die("E207", "字符串下标越界");
        return z_null();
    }
    int64_t rune = 0, i = 0;
    while (i < bytes && rune < target) {
        unsigned char c = (unsigned char)s[i];
        if (c < 0x80) i += 1;
        else if ((c >> 5) == 0x06) i += 2;
        else if ((c >> 4) == 0x0E) i += 3;
        else if ((c >> 3) == 0x1E) i += 4;
        else i += 1;
        rune++;
    }
    unsigned char c = (unsigned char)s[i];
    int64_t len = 1;
    if (c >= 0x80) {
        if ((c >> 5) == 0x06) len = 2;
        else if ((c >> 4) == 0x0E) len = 3;
        else if ((c >> 3) == 0x1E) len = 4;
    }
    char* buf = (char*)z_alloc((size_t)len + 1);
    memcpy(buf, s + i, (size_t)len);
    buf[len] = '\0';
    return z_str_take(buf, len);
}

ZVal z_index_get(ZVal obj, ZVal idx) {
    if (obj.kind == ZK_ARRAY && idx.kind == ZK_INT) {
        /* 负下标从尾部计（与解释器一致） */
        int64_t i = idx.as.i;
        if (i < 0) i += obj.as.a->len;
        if (i < 0 || i >= obj.as.a->len) {
            if (z_strict_mode) z_die("E207", "数组下标越界");
            return z_null();
        }
        return obj.as.a->items[i];
    }
    if (obj.kind == ZK_STR && idx.kind == ZK_INT) return z_str_char_at(obj, idx.as.i);
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
    ZBox* b = (ZBox*)z_obj_new(ZOK_BOX, sizeof(ZBox));
    b->v = v;
    return b;
}

void* z_new_env(size_t n) {
    return z_new_env_ex(0, NULL, n);
}

void* z_new_env_ex(int nbox, const size_t* offs, size_t n) {
    ZEnvHead* hd;
    if (n < sizeof(void*)) n = sizeof(void*);
    hd = (ZEnvHead*)z_obj_new(ZOK_ENV, sizeof(ZEnvHead) + n);
    hd->nbox = nbox > 0 ? nbox : 0;
    hd->box_off = NULL;
    if (hd->nbox > 0 && offs) {
        int i;
        hd->box_off = (size_t*)z_alloc(sizeof(size_t) * (size_t)hd->nbox);
        /* 不用 memcpy：nativecc 对外部多参调用（memset/memcpy）的传参尚不可靠 */
        for (i = 0; i < hd->nbox; i++) hd->box_off[i] = offs[i];
    }
    return (void*)(hd + 1);
}

ZVal z_closure(ZFnPtr fn, void* env, const char* name) {
    ZFunc* f = (ZFunc*)z_obj_new(ZOK_FUNC, sizeof(ZFunc));
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

/* ══════════════════════════════════════════════════════════
 * 数组 / 哈希内置
 * ══════════════════════════════════════════════════════════ */

ZVal z_pop(ZVal arr) {
    if (arr.kind != ZK_ARRAY) z_die("E217", "弹出() 参数必须是数组");
    ZArr* a = arr.as.a;
    if (a->len == 0) return z_null();
    return a->items[--a->len];
}

ZVal z_first(ZVal arr) {
    if (arr.kind != ZK_ARRAY) z_die("E217", "首项() 参数必须是数组");
    if (arr.as.a->len == 0) return z_null();
    return arr.as.a->items[0];
}

ZVal z_last(ZVal arr) {
    if (arr.kind != ZK_ARRAY) z_die("E217", "末项() 参数必须是数组");
    if (arr.as.a->len == 0) return z_null();
    return arr.as.a->items[arr.as.a->len - 1];
}

ZVal z_keys(ZVal hv) {
    if (hv.kind != ZK_HASH) z_die("E218", "键() 参数必须是哈希");
    ZVal out = z_array();
    for (int64_t i = 0; i < hv.as.h->len; i++)
        z_array_push(out, hv.as.h->entries[i].key);
    return out;
}

ZVal z_get(ZVal hv, ZVal k, ZVal defv, int hasDef) {
    if (hv.kind != ZK_HASH) z_die("E218", "取值() 第一个参数必须是哈希");
    int64_t i = z_hash_find(hv.as.h, k);
    if (i >= 0) return hv.as.h->entries[i].val;
    return hasDef ? defv : z_null();
}

/* ══════════════════════════════════════════════════════════
 * 数学内置
 * ══════════════════════════════════════════════════════════ */

static double z_num(ZVal v, const char* fn) {
    if (v.kind == ZK_INT) return (double)v.as.i;
    if (v.kind == ZK_FLOAT) return v.as.f;
    char buf[128];
    snprintf(buf, sizeof(buf), "%s() 参数必须是数值", fn);
    z_die("E219", buf);
    return 0;
}

ZVal z_abs(ZVal v) {
    if (v.kind == ZK_INT)  return z_int(v.as.i < 0 ? -v.as.i : v.as.i);
    if (v.kind == ZK_FLOAT) return z_float(fabs(v.as.f));
    z_die("E219", "绝对值() 参数必须是数值");
    return z_null();
}

ZVal z_max2(ZVal a, ZVal b) {
    if (a.kind == ZK_INT && b.kind == ZK_INT) return z_int(a.as.i > b.as.i ? a.as.i : b.as.i);
    double x = z_num(a, "最大值"), y = z_num(b, "最大值");
    return z_float(x > y ? x : y);
}

ZVal z_min2(ZVal a, ZVal b) {
    if (a.kind == ZK_INT && b.kind == ZK_INT) return z_int(a.as.i < b.as.i ? a.as.i : b.as.i);
    double x = z_num(a, "最小值"), y = z_num(b, "最小值");
    return z_float(x < y ? x : y);
}

ZVal z_ceil(ZVal v)  { return z_float(ceil(z_num(v, "向上取整"))); }
ZVal z_floor(ZVal v) { return z_float(floor(z_num(v, "向下取整"))); }
ZVal z_round(ZVal v) { return z_float(round(z_num(v, "四舍五入"))); }
ZVal z_pow(ZVal a, ZVal b) { return z_float(pow(z_num(a, "幂"), z_num(b, "幂"))); }
ZVal z_sqrt(ZVal v)  { return z_float(sqrt(z_num(v, "平方根"))); }
ZVal z_sin(ZVal v)   { return z_float(sin(z_num(v, "正弦"))); }
ZVal z_cos(ZVal v)   { return z_float(cos(z_num(v, "余弦"))); }
ZVal z_log(ZVal v)   { return z_float(log(z_num(v, "对数"))); }

ZVal z_rand(void) {
    static int seeded = 0;
    if (!seeded) { srand((unsigned)time(NULL)); seeded = 1; }
    return z_int((int64_t)(rand() % 1000000));
}

ZVal z_rand_int(ZVal lo, ZVal hi) {
    static int seeded = 0;
    if (!seeded) { srand((unsigned)time(NULL)); seeded = 1; }
    int64_t a = (lo.kind == ZK_INT) ? lo.as.i : 0;
    int64_t b = (hi.kind == ZK_INT) ? hi.as.i : 0;
    if (b <= a) return z_int(a);
    return z_int(a + (int64_t)(rand() % (int)(b - a + 1)));
}

/* ══════════════════════════════════════════════════════════
 * 字符串全局函数（复用 zs_* 实现）
 * ══════════════════════════════════════════════════════════ */

ZVal z_count_sub(ZVal v, ZVal subv) {
    ZStr* s = zs_arg(v, "计数");
    ZStr* sub = zs_arg(subv, "计数");
    if (sub->len == 0) return z_int(0);
    int64_t n = 0;
    for (int64_t i = 0; i + sub->len <= s->len; ) {
        if (memcmp(s->data + i, sub->data, (size_t)sub->len) == 0) { n++; i += sub->len; }
        else i++;
    }
    return z_int(n);
}

ZVal z_char_at(ZVal v, ZVal idxv) {
    ZStr* s = zs_arg(v, "取字符");
    int64_t runes = z_utf8_len(s->data, s->len);
    int64_t idx = (idxv.kind == ZK_INT) ? idxv.as.i : 0;
    if (idx < 0) idx += runes;
    if (idx < 0 || idx >= runes) return z_str_len("", 0);
    int64_t ri = 0, bi = 0;
    while (bi < s->len) {
        unsigned char c = (unsigned char)s->data[bi];
        int64_t w = 1;
        if (c >= 0xF0) w = 4; else if (c >= 0xE0) w = 3; else if (c >= 0xC0) w = 2;
        if (ri == idx) return z_str_len(s->data + bi, w);
        bi += w; ri++;
    }
    return z_str_len("", 0);
}

ZVal z_fields(ZVal v) {
    ZStr* s = zs_arg(v, "分词");
    ZVal out = z_array();
    int64_t i = 0;
    while (i < s->len) {
        while (i < s->len && zs_is_space(s->data[i])) i++;
        if (i >= s->len) break;
        int64_t start = i;
        while (i < s->len && !zs_is_space(s->data[i])) i++;
        z_array_push(out, z_str_len(s->data + start, i - start));
    }
    return out;
}

ZVal z_trim_chars(ZVal v, ZVal setv) {
    ZStr* s = zs_arg(v, "裁剪");
    ZStr* set = zs_arg(setv, "裁剪");
    int64_t a = 0, b = s->len;
    while (a < b && memchr(set->data, s->data[a], (size_t)set->len) != NULL) a++;
    while (b > a && memchr(set->data, s->data[b - 1], (size_t)set->len) != NULL) b--;
    return z_str_len(s->data + a, b - a);
}

/* ══════════════════════════════════════════════════════════
 * JSON（解析 + 序列化）
 * ══════════════════════════════════════════════════════════ */

typedef struct { const char* p; const char* end; int err; } ZJsonCur;

static void zj_ws(ZJsonCur* c) {
    while (c->p < c->end && (*c->p == ' ' || *c->p == '\t' || *c->p == '\n' || *c->p == '\r')) c->p++;
}

static ZVal zj_value(ZJsonCur* c);

static ZVal zj_string(ZJsonCur* c) {
    if (c->p >= c->end || *c->p != '"') { c->err = 1; return z_null(); }
    c->p++;
    ZBuf b; buf_init(&b);
    while (c->p < c->end && *c->p != '"') {
        char ch = *c->p++;
        if (ch == '\\' && c->p < c->end) {
            char e = *c->p++;
            switch (e) {
                case 'n': buf_putc(&b, '\n'); break;
                case 't': buf_putc(&b, '\t'); break;
                case 'r': buf_putc(&b, '\r'); break;
                case 'b': buf_putc(&b, '\b'); break;
                case 'f': buf_putc(&b, '\f'); break;
                case '"': buf_putc(&b, '"'); break;
                case '\\': buf_putc(&b, '\\'); break;
                case '/': buf_putc(&b, '/'); break;
                case 'u': {
                    unsigned cp = 0;
                    for (int i = 0; i < 4 && c->p < c->end; i++) {
                        char h = *c->p++;
                        cp <<= 4;
                        if (h >= '0' && h <= '9') cp |= (unsigned)(h - '0');
                        else if (h >= 'a' && h <= 'f') cp |= (unsigned)(h - 'a' + 10);
                        else if (h >= 'A' && h <= 'F') cp |= (unsigned)(h - 'A' + 10);
                    }
                    if (cp < 0x80) buf_putc(&b, (char)cp);
                    else if (cp < 0x800) { buf_putc(&b, (char)(0xC0 | (cp >> 6))); buf_putc(&b, (char)(0x80 | (cp & 0x3F))); }
                    else { buf_putc(&b, (char)(0xE0 | (cp >> 12))); buf_putc(&b, (char)(0x80 | ((cp >> 6) & 0x3F))); buf_putc(&b, (char)(0x80 | (cp & 0x3F))); }
                    break;
                }
                default: buf_putc(&b, e); break;
            }
        } else {
            buf_putc(&b, ch);
        }
    }
    if (c->p < c->end) c->p++; else c->err = 1;
    return z_str_take(b.data, b.len);
}

static ZVal zj_array(ZJsonCur* c) {
    c->p++; /* [ */
    ZVal out = z_array();
    zj_ws(c);
    if (c->p < c->end && *c->p == ']') { c->p++; return out; }
    while (c->p < c->end) {
        ZVal v = zj_value(c);
        if (c->err) return out;
        z_array_push(out, v);
        zj_ws(c);
        if (c->p < c->end && *c->p == ',') { c->p++; continue; }
        if (c->p < c->end && *c->p == ']') { c->p++; break; }
        c->err = 1; break;
    }
    return out;
}

static ZVal zj_object(ZJsonCur* c) {
    c->p++; /* { */
    ZVal out = z_hash();
    zj_ws(c);
    if (c->p < c->end && *c->p == '}') { c->p++; return out; }
    while (c->p < c->end) {
        zj_ws(c);
        ZVal k = zj_string(c);
        if (c->err) return out;
        zj_ws(c);
        if (c->p >= c->end || *c->p != ':') { c->err = 1; return out; }
        c->p++;
        ZVal v = zj_value(c);
        if (c->err) return out;
        z_hash_set(out, k, v);
        zj_ws(c);
        if (c->p < c->end && *c->p == ',') { c->p++; continue; }
        if (c->p < c->end && *c->p == '}') { c->p++; break; }
        c->err = 1; break;
    }
    return out;
}

static ZVal zj_value(ZJsonCur* c) {
    zj_ws(c);
    if (c->p >= c->end) { c->err = 1; return z_null(); }
    char ch = *c->p;
    if (ch == '"') return zj_string(c);
    if (ch == '[') return zj_array(c);
    if (ch == '{') return zj_object(c);
    if (ch == 't' && c->end - c->p >= 4 && memcmp(c->p, "true", 4) == 0) { c->p += 4; return z_bool(1); }
    if (ch == 'f' && c->end - c->p >= 5 && memcmp(c->p, "false", 5) == 0) { c->p += 5; return z_bool(0); }
    if (ch == 'n' && c->end - c->p >= 4 && memcmp(c->p, "null", 4) == 0) { c->p += 4; return z_null(); }
    /* 数字 */
    const char* start = c->p;
    int isFloat = 0;
    if (*c->p == '-') c->p++;
    while (c->p < c->end && *c->p >= '0' && *c->p <= '9') c->p++;
    if (c->p < c->end && *c->p == '.') { isFloat = 1; c->p++; while (c->p < c->end && *c->p >= '0' && *c->p <= '9') c->p++; }
    if (c->p < c->end && (*c->p == 'e' || *c->p == 'E')) { isFloat = 1; c->p++; if (*c->p == '+' || *c->p == '-') c->p++; while (c->p < c->end && *c->p >= '0' && *c->p <= '9') c->p++; }
    if (c->p == start) { c->err = 1; return z_null(); }
    char buf[128];
    int64_t n = c->p - start;
    if (n >= (int64_t)sizeof(buf)) n = (int64_t)sizeof(buf) - 1;
    memcpy(buf, start, (size_t)n); buf[n] = 0;
    if (isFloat) return z_float(strtod(buf, NULL));
    return z_int(strtoll(buf, NULL, 10));
}

ZVal z_json_parse(ZVal sv) {
    if (sv.kind != ZK_STR) z_die("E220", "json解析() 参数必须是字符串");
    ZJsonCur c;
    c.p = sv.as.s->data; c.end = sv.as.s->data + sv.as.s->len; c.err = 0;
    ZVal v = zj_value(&c);
    if (c.err) z_die("E221", "json解析() 失败: 非法 JSON");
    return v;
}

static void zj_write(ZBuf* b, ZVal v);

static void zj_write_str(ZBuf* b, ZStr* s) {
    buf_putc(b, '"');
    for (int64_t i = 0; i < s->len; i++) {
        char c = s->data[i];
        switch (c) {
            case '"': buf_puts(b, "\\\""); break;
            case '\\': buf_puts(b, "\\\\"); break;
            case '\n': buf_puts(b, "\\n"); break;
            case '\r': buf_puts(b, "\\r"); break;
            case '\t': buf_puts(b, "\\t"); break;
            default:
                if ((unsigned char)c < 0x20) {
                    char tmp[8]; snprintf(tmp, sizeof(tmp), "\\u%04x", (unsigned char)c); buf_puts(b, tmp);
                } else buf_putc(b, c);
        }
    }
    buf_putc(b, '"');
}

static void zj_write(ZBuf* b, ZVal v) {
    char tmp[64];
    switch (v.kind) {
        case ZK_NULL: buf_puts(b, "null"); break;
        case ZK_INT: snprintf(tmp, sizeof(tmp), "%lld", (long long)v.as.i); buf_puts(b, tmp); break;
        case ZK_FLOAT: z_format_float(v.as.f, tmp, sizeof(tmp)); buf_puts(b, tmp); break;
        case ZK_BOOL: buf_puts(b, v.as.b ? "true" : "false"); break;
        case ZK_STR: zj_write_str(b, v.as.s); break;
        case ZK_ARRAY:
            buf_putc(b, '[');
            for (int64_t i = 0; i < v.as.a->len; i++) {
                if (i) buf_putc(b, ',');
                zj_write(b, v.as.a->items[i]);
            }
            buf_putc(b, ']');
            break;
        case ZK_HASH: {
            /* 与 Go json.Marshal 一致：key 按字典序 */
            int64_t n = v.as.h->len;
            int64_t* order = (int64_t*)z_alloc(sizeof(int64_t) * (size_t)(n > 0 ? n : 1));
            for (int64_t i = 0; i < n; i++) order[i] = i;
            for (int64_t i = 1; i < n; i++) {
                int64_t key = order[i], j = i - 1;
                while (j >= 0) {
                    ZVal a = v.as.h->entries[order[j]].key, bb = v.as.h->entries[key].key;
                    int cmp = 0;
                    if (a.kind == ZK_STR && bb.kind == ZK_STR) cmp = z_str_cmp(a.as.s, bb.as.s);
                    if (cmp <= 0) break;
                    order[j + 1] = order[j]; j--;
                }
                order[j + 1] = key;
            }
            buf_putc(b, '{');
            for (int64_t i = 0; i < n; i++) {
                if (i) buf_putc(b, ',');
                ZHashEntry* e = &v.as.h->entries[order[i]];
                ZVal ks = z_to_str(e->key);
                zj_write_str(b, ks.as.s);
                buf_putc(b, ':');
                zj_write(b, e->val);
            }
            buf_putc(b, '}');
            free(order);
            break;
        }
        case ZK_FUNC: buf_puts(b, "null"); break;
    }
}

ZVal z_json_stringify(ZVal v) {
    ZBuf b; buf_init(&b);
    zj_write(&b, v);
    return z_str_take(b.data, b.len);
}

/* ══════════════════════════════════════════════════════════
 * 文件 I/O
 * ══════════════════════════════════════════════════════════ */

ZVal z_read_file(ZVal pv) {
    if (pv.kind != ZK_STR) z_die("E222", "读文件() 参数必须是字符串");
    FILE* f = fopen(pv.as.s->data, "rb");
    if (!f) z_die("E223", "读文件() 失败: 无法打开");
    fseek(f, 0, SEEK_END);
    long n = ftell(f);
    fseek(f, 0, SEEK_SET);
    char* buf = (char*)z_alloc((size_t)n + 1);
    size_t got = fread(buf, 1, (size_t)n, f);
    fclose(f);
    buf[got] = 0;
    return z_str_take(buf, (int64_t)got);
}

ZVal z_write_file(ZVal pv, ZVal cv) {
    if (pv.kind != ZK_STR || cv.kind != ZK_STR) z_die("E222", "写文件() 参数必须是字符串");
    FILE* f = fopen(pv.as.s->data, "wb");
    if (!f) z_die("E223", "写文件() 失败: 无法打开");
    fwrite(cv.as.s->data, 1, (size_t)cv.as.s->len, f);
    fclose(f);
    return z_null();
}

ZVal z_append_file(ZVal pv, ZVal cv) {
    if (pv.kind != ZK_STR || cv.kind != ZK_STR) z_die("E222", "追加文件() 参数必须是字符串");
    FILE* f = fopen(pv.as.s->data, "ab");
    if (!f) z_die("E223", "追加文件() 失败: 无法打开");
    fwrite(cv.as.s->data, 1, (size_t)cv.as.s->len, f);
    fclose(f);
    return z_null();
}

ZVal z_file_exists(ZVal pv) {
    if (pv.kind != ZK_STR) z_die("E222", "文件存在() 参数必须是字符串");
    FILE* f = fopen(pv.as.s->data, "rb");
    if (f) { fclose(f); return z_bool(1); }
    return z_bool(0);
}

ZVal z_create_file(ZVal pv) { return z_write_file(pv, z_str("")); }

ZVal z_delete_file(ZVal pv) {
    if (pv.kind != ZK_STR) z_die("E222", "删除文件() 参数必须是字符串");
    return z_bool(remove(pv.as.s->data) == 0);
}

ZVal z_rename_file(ZVal a, ZVal b) {
    if (a.kind != ZK_STR || b.kind != ZK_STR) z_die("E222", "重命名() 参数必须是字符串");
    return z_bool(rename(a.as.s->data, b.as.s->data) == 0);
}

/* ══════════════════════════════════════════════════════════
 * 编码 / 解码
 * ══════════════════════════════════════════════════════════ */

static const char ZB64[] = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

ZVal z_base64_encode(ZVal v) {
    ZStr* s = zs_arg(v, "base64编码");
    int64_t olen = 4 * ((s->len + 2) / 3);
    char* out = (char*)z_alloc((size_t)olen + 1);
    int64_t i = 0, j = 0;
    while (i < s->len) {
        int64_t remain = s->len - i;
        unsigned a = (unsigned char)s->data[i];
        unsigned b = remain > 1 ? (unsigned char)s->data[i + 1] : 0;
        unsigned c = remain > 2 ? (unsigned char)s->data[i + 2] : 0;
        unsigned n = (a << 16) | (b << 8) | c;
        out[j++] = ZB64[(n >> 18) & 63];
        out[j++] = ZB64[(n >> 12) & 63];
        out[j++] = (remain > 1) ? ZB64[(n >> 6) & 63] : '=';
        out[j++] = (remain > 2) ? ZB64[n & 63] : '=';
        i += 3;
    }
    out[j] = 0;
    return z_str_take(out, j);
}

static int zb64_val(char c) {
    if (c >= 'A' && c <= 'Z') return c - 'A';
    if (c >= 'a' && c <= 'z') return c - 'a' + 26;
    if (c >= '0' && c <= '9') return c - '0' + 52;
    if (c == '+' || c == '-') return 62;
    if (c == '/' || c == '_') return 63;
    return -1;
}

ZVal z_base64_decode(ZVal v) {
    ZStr* s = zs_arg(v, "base64解码");
    char* out = (char*)z_alloc((size_t)s->len + 1);
    int64_t j = 0;
    unsigned buf = 0;
    int bits = 0;
    for (int64_t i = 0; i < s->len; i++) {
        char c = s->data[i];
        if (c == '=') break;
        int val = zb64_val(c);
        if (val < 0) continue;
        buf = (buf << 6) | (unsigned)val;
        bits += 6;
        if (bits >= 8) {
            bits -= 8;
            out[j++] = (char)((buf >> bits) & 0xFF);
        }
    }
    out[j] = 0;
    return z_str_take(out, j);
}

ZVal z_hex_encode(ZVal v) {
    ZStr* s = zs_arg(v, "hex编码");
    char* out = (char*)z_alloc((size_t)s->len * 2 + 1);
    const char* H = "0123456789abcdef";
    for (int64_t i = 0; i < s->len; i++) {
        out[i * 2] = H[((unsigned char)s->data[i]) >> 4];
        out[i * 2 + 1] = H[((unsigned char)s->data[i]) & 15];
    }
    out[s->len * 2] = 0;
    return z_str_take(out, s->len * 2);
}

ZVal z_hex_decode(ZVal v) {
    ZStr* s = zs_arg(v, "hex解码");
    char* out = (char*)z_alloc((size_t)s->len / 2 + 1);
    int64_t j = 0;
    for (int64_t i = 0; i + 1 < s->len; i += 2) {
        int hi = 0, lo = 0;
        char a = s->data[i], b = s->data[i + 1];
        hi = (a >= '0' && a <= '9') ? a - '0' : ((a >= 'a' && a <= 'f') ? a - 'a' + 10 : ((a >= 'A' && a <= 'F') ? a - 'A' + 10 : 0));
        lo = (b >= '0' && b <= '9') ? b - '0' : ((b >= 'a' && b <= 'f') ? b - 'a' + 10 : ((b >= 'A' && b <= 'F') ? b - 'A' + 10 : 0));
        out[j++] = (char)((hi << 4) | lo);
    }
    out[j] = 0;
    return z_str_take(out, j);
}

ZVal z_url_encode(ZVal v) {
    ZStr* s = zs_arg(v, "url编码");
    ZBuf b; buf_init(&b);
    const char* H = "0123456789ABCDEF";
    for (int64_t i = 0; i < s->len; i++) {
        unsigned char c = (unsigned char)s->data[i];
        if ((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
            c == '-' || c == '_' || c == '.' || c == '~') {
            buf_putc(&b, (char)c);
        } else if (c == ' ') {
            buf_putc(&b, '+');
        } else {
            buf_putc(&b, '%'); buf_putc(&b, H[c >> 4]); buf_putc(&b, H[c & 15]);
        }
    }
    return z_str_take(b.data, b.len);
}

ZVal z_url_decode(ZVal v) {
    ZStr* s = zs_arg(v, "url解码");
    ZBuf b; buf_init(&b);
    for (int64_t i = 0; i < s->len; i++) {
        char c = s->data[i];
        if (c == '+') { buf_putc(&b, ' '); continue; }
        if (c == '%' && i + 2 < s->len) {
            char a = s->data[i + 1], d = s->data[i + 2];
            int hi = (a >= '0' && a <= '9') ? a - '0' : ((a >= 'a' && a <= 'f') ? a - 'a' + 10 : ((a >= 'A' && a <= 'F') ? a - 'A' + 10 : 0));
            int lo = (d >= '0' && d <= '9') ? d - '0' : ((d >= 'a' && d <= 'f') ? d - 'a' + 10 : ((d >= 'A' && d <= 'F') ? d - 'A' + 10 : 0));
            buf_putc(&b, (char)((hi << 4) | lo));
            i += 2;
            continue;
        }
        buf_putc(&b, c);
    }
    return z_str_take(b.data, b.len);
}

/* ══════════════════════════════════════════════════════════
 * 时间
 * ══════════════════════════════════════════════════════════ */

ZVal z_unix_time(void) { return z_int((int64_t)time(NULL)); }

ZVal z_unix_milli(void) {
#ifdef _WIN32
    return z_int((int64_t)time(NULL) * 1000);
#else
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    return z_int((int64_t)ts.tv_sec * 1000 + ts.tv_nsec / 1000000);
#endif
}

ZVal z_now(void) {
    time_t t = time(NULL);
    struct tm tmv;
#ifdef _WIN32
    localtime_s(&tmv, &t);
#else
    localtime_r(&t, &tmv);
#endif
    char buf[64];
    strftime(buf, sizeof(buf), "%Y-%m-%d %H:%M:%S", &tmv);
    return z_str(buf);
}

ZVal z_sleep_ms(ZVal v) {
    if (v.kind != ZK_INT) z_die("E224", "睡眠() 参数必须是整数（毫秒）");
#ifdef _WIN32
    Sleep((DWORD)v.as.i);
#else
    struct timespec ts;
    ts.tv_sec = v.as.i / 1000;
    ts.tv_nsec = (v.as.i % 1000) * 1000000;
    nanosleep(&ts, NULL);
#endif
    return z_null();
}

/* ══════════════════════════════════════════════════════════
 * 正则（回溯子集：. * + ? [] ^ $ \d\w\s 分组）
 * ══════════════════════════════════════════════════════════ */

static int zre_class_match(const char* p, int plen, int start, int end, char c) {
    /* p[start..end) 是字符类内容（不含 [ ]），支持 ^ 取反与 a-z 区间 */
    int i = start;
    int neg = 0;
    if (i < end && p[i] == '^') { neg = 1; i++; }
    int hit = 0;
    while (i < end) {
        if (p[i] == '\\' && i + 1 < end) {
            char e = p[i + 1];
            if ((e == 'd' && c >= '0' && c <= '9') ||
                (e == 'w' && ((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_')) ||
                (e == 's' && zs_is_space(c))) hit = 1;
            i += 2;
        } else if (i + 2 < end && p[i + 1] == '-') {
            if (c >= p[i] && c <= p[i + 2]) hit = 1;
            i += 3;
        } else {
            if (p[i] == c) hit = 1;
            i++;
        }
    }
    return neg ? !hit : hit;
}

/* 返回 atom 结束后的 pattern 下标 */
static int zre_atom_end(const char* p, int plen, int pi) {
    if (pi >= plen) return pi;
    if (p[pi] == '\\') return pi + 2 <= plen ? pi + 2 : plen;
    if (p[pi] == '[') {
        int i = pi + 1;
        if (i < plen && p[i] == '^') i++;
        if (i < plen && p[i] == ']') i++;
        while (i < plen && p[i] != ']') i++;
        return i < plen ? i + 1 : plen;
    }
    if (p[pi] == '(') {
        int depth = 0, i = pi;
        while (i < plen) {
            if (p[i] == '\\') { i += 2; continue; }
            if (p[i] == '(') depth++;
            else if (p[i] == ')') { depth--; if (depth == 0) return i + 1; }
            i++;
        }
        return plen;
    }
    return pi + 1;
}

static int zre_match_here(const char* p, int plen, int pi, const char* s, int slen, int si);

/* 单字符匹配（atom 起始 pi） */
static int zre_atom_char(const char* p, int plen, int pi, char c) {
    if (pi >= plen) return 0;
    if (p[pi] == '\\' && pi + 1 < plen) {
        char e = p[pi + 1];
        if (e == 'd') return c >= '0' && c <= '9';
        if (e == 'w') return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_';
        if (e == 's') return zs_is_space(c);
        if (e == 'D') return !(c >= '0' && c <= '9');
        if (e == 'W') return !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_');
        if (e == 'S') return !zs_is_space(c);
        return c == e;
    }
    if (p[pi] == '.') return c != '\n';
    if (p[pi] == '[') {
        int e = zre_atom_end(p, plen, pi);
        return zre_class_match(p, plen, pi + 1, e - 1, c);
    }
    if (p[pi] == '(') {
        int e = zre_atom_end(p, plen, pi);
        /* 分组：用子表达式整体匹配一个"字符串"，此处简化成子表达式匹配 */
        /* 用递归：在 s[si..] 上匹配 (inner) */
        return -1; /* 由 zre_match_here 专门处理 */
    }
    return p[pi] == c;
}

static int zre_match_here(const char* p, int plen, int pi, const char* s, int slen, int si) {
    if (pi >= plen) return si;
    if (p[pi] == '$' && pi + 1 == plen) return si == slen ? si : -1;
    if (p[pi] == '^') return pi == 0 ? zre_match_here(p, plen, pi + 1, s, slen, si) : -1;

    int ae = zre_atom_end(p, plen, pi);
    char q = (ae < plen) ? p[ae] : 0;
    int min = 1, max = 1, next = ae;
    if (q == '*') { min = 0; max = 0x7fffffff; next = ae + 1; }
    else if (q == '+') { min = 1; max = 0x7fffffff; next = ae + 1; }
    else if (q == '?') { min = 0; max = 1; next = ae + 1; }

    /* 分组：贪婪匹配子表达式 */
    if (p[pi] == '(') {
        int inner_start = pi + 1;
        int inner_end = ae - 1; /* 去掉 ')' */
        int count = 0, pos = si;
        while (count < max) {
            int r = zre_match_here(p, inner_end, inner_start, s, slen, pos);
            if (r < 0 || r == pos) break;
            pos = r; count++;
            if (count > 100000) break;
        }
        for (int k = count; k >= min; k--) {
            /* 重新计算 k 次分组后的位置 */
            int pp = si;
            for (int t = 0; t < k; t++) {
                int r = zre_match_here(p, inner_end, inner_start, s, slen, pp);
                if (r < 0) { pp = -1; break; }
                pp = r;
            }
            if (pp < 0) continue;
            int r2 = zre_match_here(p, plen, next, s, slen, pp);
            if (r2 >= 0) return r2;
        }
        return -1;
    }

    int count = 0, pos = si;
    while (count < max && pos < slen && zre_atom_char(p, plen, pi, s[pos]) == 1) { pos++; count++; }
    for (int k = count; k >= min; k--) {
        int r = zre_match_here(p, plen, next, s, slen, si + k);
        if (r >= 0) return r;
    }
    return -1;
}

/* 在 s 中从 start 起找首个匹配：[ms, me) */
static int zre_search(const char* p, int plen, const char* s, int slen, int start, int* ms, int* me) {
    for (int i = start; i <= slen; i++) {
        int r = zre_match_here(p, plen, 0, s, slen, i);
        if (r >= 0) { *ms = i; *me = r; return 1; }
    }
    return 0;
}

static void zre_args(ZVal s, ZVal p, ZStr** ss, ZStr** pp, const char* fn) {
    if (s.kind != ZK_STR || p.kind != ZK_STR) {
        char buf[128];
        snprintf(buf, sizeof(buf), "%s() 参数必须是字符串", fn);
        z_die("E225", buf);
    }
    *ss = s.as.s;
    *pp = p.as.s;
}

ZVal z_regex_test(ZVal sv, ZVal pv) {
    ZStr* s; ZStr* p;
    zre_args(sv, pv, &s, &p, "正则验证");
    int ms, me;
    return z_bool(zre_search(p->data, (int)p->len, s->data, (int)s->len, 0, &ms, &me));
}

ZVal z_regex_match(ZVal sv, ZVal pv) {
    /* 对齐解释器：返回 FindStringSubmatch 的数组（组 0 = 整个匹配；捕获组本轮未单独提取） */
    ZStr* s; ZStr* p;
    zre_args(sv, pv, &s, &p, "正则匹配");
    int ms, me;
    if (!zre_search(p->data, (int)p->len, s->data, (int)s->len, 0, &ms, &me)) return z_null();
    ZVal out = z_array();
    z_array_push(out, z_str_len(s->data + ms, me - ms));
    return out;
}

ZVal z_regex_find_all(ZVal sv, ZVal pv) {
    /* 对齐解释器：二维数组，每个匹配是一个数组（本轮只含组 0） */
    ZStr* s; ZStr* p;
    zre_args(sv, pv, &s, &p, "正则查找全部");
    ZVal out = z_array();
    int start = 0, ms, me, guard = 0;
    while (start <= s->len && zre_search(p->data, (int)p->len, s->data, (int)s->len, start, &ms, &me)) {
        ZVal one = z_array();
        z_array_push(one, z_str_len(s->data + ms, me - ms));
        z_array_push(out, one);
        start = (me > ms) ? me : ms + 1;
        if (++guard > 100000) break;
    }
    return out;
}

ZVal z_regex_replace(ZVal sv, ZVal pv, ZVal rv) {
    ZStr* s; ZStr* p;
    zre_args(sv, pv, &s, &p, "正则替换");
    if (rv.kind != ZK_STR) z_die("E225", "正则替换() 第三个参数必须是字符串");
    ZStr* r = rv.as.s;
    ZBuf b; buf_init(&b);
    int pos = 0, ms, me, guard = 0;
    while (pos <= s->len && zre_search(p->data, (int)p->len, s->data, (int)s->len, pos, &ms, &me)) {
        buf_putn(&b, s->data + pos, ms - pos);
        buf_putn(&b, r->data, r->len);
        pos = (me > ms) ? me : ms + 1;
        if (++guard > 100000) break;
    }
    if (pos <= s->len) buf_putn(&b, s->data + pos, s->len - pos);
    return z_str_take(b.data, b.len);
}

ZVal z_regex_split(ZVal sv, ZVal pv) {
    ZStr* s; ZStr* p;
    zre_args(sv, pv, &s, &p, "正则分割");
    ZVal out = z_array();
    int pos = 0, ms, me, guard = 0;
    while (pos <= s->len && zre_search(p->data, (int)p->len, s->data, (int)s->len, pos, &ms, &me)) {
        z_array_push(out, z_str_len(s->data + pos, ms - pos));
        pos = (me > ms) ? me : ms + 1;
        if (++guard > 100000) break;
    }
    if (pos <= s->len) z_array_push(out, z_str_len(s->data + pos, s->len - pos));
    return out;
}

ZVal z_regex_extract(ZVal sv, ZVal pv) {
    /* 对齐解释器：返回首个匹配字符串，无匹配返回空 */
    ZStr* s; ZStr* p;
    zre_args(sv, pv, &s, &p, "正则提取");
    int ms, me;
    if (!zre_search(p->data, (int)p->len, s->data, (int)s->len, 0, &ms, &me)) return z_null();
    if (me == ms) return z_null();
    return z_str_len(s->data + ms, me - ms);
}

/* ══════════════════════════════════════════════════════════
 * 文本命名空间扩展（ASCII 语义）
 * ══════════════════════════════════════════════════════════ */

ZVal zs_capitalize(ZVal v) {
    ZStr* s = zs_arg(v, "首字母大写");
    char* p = (char*)z_alloc((size_t)s->len + 1);
    for (int64_t i = 0; i < s->len; i++) {
        char c = s->data[i];
        if (i == 0 && c >= 'a' && c <= 'z') c = (char)(c - 32);
        else if (i > 0 && c >= 'A' && c <= 'Z') c = (char)(c + 32);
        p[i] = c;
    }
    p[s->len] = 0;
    return z_str_take(p, s->len);
}

ZVal zs_title(ZVal v) {
    ZStr* s = zs_arg(v, "标题化");
    char* p = (char*)z_alloc((size_t)s->len + 1);
    int start = 1;
    for (int64_t i = 0; i < s->len; i++) {
        char c = s->data[i];
        if (c == ' ' || c == '\t' || c == '\n' || c == '\r') { start = 1; p[i] = c; continue; }
        if (start) {
            if (c >= 'a' && c <= 'z') c = (char)(c - 32);
            start = 0;
        } else if (c >= 'A' && c <= 'Z') {
            c = (char)(c + 32);
        }
        p[i] = c;
    }
    p[s->len] = 0;
    return z_str_take(p, s->len);
}

ZVal zs_swapcase(ZVal v) {
    ZStr* s = zs_arg(v, "大小写互换");
    char* p = (char*)z_alloc((size_t)s->len + 1);
    for (int64_t i = 0; i < s->len; i++) {
        char c = s->data[i];
        if (c >= 'a' && c <= 'z') c = (char)(c - 32);
        else if (c >= 'A' && c <= 'Z') c = (char)(c + 32);
        p[i] = c;
    }
    p[s->len] = 0;
    return z_str_take(p, s->len);
}

ZVal zs_lstrip(ZVal v) {
    ZStr* s = zs_arg(v, "去左空白");
    int64_t a = 0;
    while (a < s->len && zs_is_space(s->data[a])) a++;
    return z_str_len(s->data + a, s->len - a);
}

ZVal zs_rstrip(ZVal v) {
    ZStr* s = zs_arg(v, "去右空白");
    int64_t b = s->len;
    while (b > 0 && zs_is_space(s->data[b - 1])) b--;
    return z_str_len(s->data, b);
}

ZVal zs_remove_prefix(ZVal v, ZVal pv) {
    ZStr* s = zs_arg(v, "去前缀");
    ZStr* p = zs_arg(pv, "去前缀");
    if (p->len <= s->len && memcmp(s->data, p->data, (size_t)p->len) == 0)
        return z_str_len(s->data + p->len, s->len - p->len);
    return v;
}

ZVal zs_remove_suffix(ZVal v, ZVal pv) {
    ZStr* s = zs_arg(v, "去后缀");
    ZStr* p = zs_arg(pv, "去后缀");
    if (p->len <= s->len && memcmp(s->data + (s->len - p->len), p->data, (size_t)p->len) == 0)
        return z_str_len(s->data, s->len - p->len);
    return v;
}

ZVal zs_replace_n(ZVal v, ZVal oldv, ZVal newv, ZVal nv) {
    ZStr* s = zs_arg(v, "替换次");
    ZStr* o = zs_arg(oldv, "替换次");
    ZStr* n = zs_arg(newv, "替换次");
    int64_t limit = (nv.kind == ZK_INT) ? nv.as.i : -1;
    if (o->len == 0) return v;
    ZBuf b; buf_init(&b);
    int64_t i = 0, done = 0;
    while (i < s->len) {
        if ((limit < 0 || done < limit) && i + o->len <= s->len &&
            memcmp(s->data + i, o->data, (size_t)o->len) == 0) {
            buf_putn(&b, n->data, n->len);
            i += o->len; done++;
        } else {
            buf_putc(&b, s->data[i]); i++;
        }
    }
    return z_str_take(b.data, b.len);
}

ZVal zs_split_n(ZVal v, ZVal sepv, ZVal nv) {
    ZStr* s = zs_arg(v, "分割次");
    ZStr* sep = zs_arg(sepv, "分割次");
    int64_t limit = (nv.kind == ZK_INT) ? nv.as.i : 0;
    ZVal out = z_array();
    if (sep->len == 0) { z_array_push(out, v); return out; }
    int64_t start = 0, i = 0, parts = 0;
    while (i < s->len) {
        if ((limit <= 0 || parts < limit - 1) && i + sep->len <= s->len &&
            memcmp(s->data + i, sep->data, (size_t)sep->len) == 0) {
            z_array_push(out, z_str_len(s->data + start, i - start));
            i += sep->len; start = i; parts++;
        } else {
            i++;
        }
    }
    z_array_push(out, z_str_len(s->data + start, s->len - start));
    return out;
}

/* 按 rune 数补齐（填充串按 UTF-8 码点循环） */
static ZVal zs_pad(ZVal v, ZVal widthv, ZVal fillv, int mode) {
    ZStr* s = zs_arg(v, "填充");
    int64_t width = (widthv.kind == ZK_INT) ? widthv.as.i : 0;
    ZStr* fill = (fillv.kind == ZK_STR && fillv.as.s->len > 0) ? fillv.as.s : NULL;
    int64_t runes = z_utf8_len(s->data, s->len);
    if (width <= runes) return v;
    int64_t pad = width - runes;
    int64_t left = 0, right = 0;
    if (mode == 0) right = pad;            /* 左对齐 */
    else if (mode == 1) left = pad;        /* 右对齐 */
    else { left = (pad + 1) / 2; right = pad - left; } /* 居中：对齐 CPython（多出的一格补左侧） */
    ZBuf b; buf_init(&b);
    int64_t total = left + right, k = 0;
    if (!fill) { for (int64_t i = 0; i < left; i++) buf_putc(&b, ' '); }
    else {
        int64_t fl = z_utf8_len(fill->data, fill->len);
        for (int64_t i = 0; i < left; i++) {
            /* 循环取填充串的第 k 个码点 */
            int64_t idx = k % fl, ri = 0, bi = 0;
            while (bi < fill->len && ri < idx) {
                unsigned char c = (unsigned char)fill->data[bi];
                int64_t w = 1;
                if (c >= 0xF0) w = 4; else if (c >= 0xE0) w = 3; else if (c >= 0xC0) w = 2;
                bi += w; ri++;
            }
            if (bi < fill->len) {
                unsigned char c = (unsigned char)fill->data[bi];
                int64_t w = 1;
                if (c >= 0xF0) w = 4; else if (c >= 0xE0) w = 3; else if (c >= 0xC0) w = 2;
                buf_putn(&b, fill->data + bi, w);
            }
            k++;
        }
    }
    buf_putn(&b, s->data, s->len);
    for (int64_t i = 0; i < right; i++) {
        if (!fill) { buf_putc(&b, ' '); continue; }
        int64_t fl = z_utf8_len(fill->data, fill->len);
        int64_t idx = (k) % fl, ri = 0, bi = 0;
        while (bi < fill->len && ri < idx) {
            unsigned char c = (unsigned char)fill->data[bi];
            int64_t w = 1;
            if (c >= 0xF0) w = 4; else if (c >= 0xE0) w = 3; else if (c >= 0xC0) w = 2;
            bi += w; ri++;
        }
        if (bi < fill->len) {
            unsigned char c = (unsigned char)fill->data[bi];
            int64_t w = 1;
            if (c >= 0xF0) w = 4; else if (c >= 0xE0) w = 3; else if (c >= 0xC0) w = 2;
            buf_putn(&b, fill->data + bi, w);
        }
        k++;
    }
    (void)total;
    return z_str_take(b.data, b.len);
}

ZVal zs_ljust(ZVal v, ZVal w, ZVal f) { return zs_pad(v, w, f, 0); }
ZVal zs_rjust(ZVal v, ZVal w, ZVal f) { return zs_pad(v, w, f, 1); }
ZVal zs_center(ZVal v, ZVal w, ZVal f) { return zs_pad(v, w, f, 2); }

/* ══════════════════════════════════════════════════════════
 * 摘要：MD5 / SHA1 / SHA256（标准实现，输出小写 hex）
 * ══════════════════════════════════════════════════════════ */

typedef struct { uint32_t h[4]; uint64_t len; unsigned char buf[64]; size_t n; } ZMd5;
static const uint32_t MD5_K[64] = {
0xd76aa478,0xe8c7b756,0x242070db,0xc1bdceee,0xf57c0faf,0x4787c62a,0xa8304613,0xfd469501,
0x698098d8,0x8b44f7af,0xffff5bb1,0x895cd7be,0x6b901122,0xfd987193,0xa679438e,0x49b40821,
0xf61e2562,0xc040b340,0x265e5a51,0xe9b6c7aa,0xd62f105d,0x02441453,0xd8a1e681,0xe7d3fbc8,
0x21e1cde6,0xc33707d6,0xf4d50d87,0x455a14ed,0xa9e3e905,0xfcefa3f8,0x676f02d9,0x8d2a4c8a,
0xfffa3942,0x8771f681,0x6d9d6122,0xfde5380c,0xa4beea44,0x4bdecfa9,0xf6bb4b60,0xbebfbc70,
0x289b7ec6,0xeaa127fa,0xd4ef3085,0x04881d05,0xd9d4d039,0xe6db99e5,0x1fa27cf8,0xc4ac5665,
0xf4292244,0x432aff97,0xab9423a7,0xfc93a039,0x655b59c3,0x8f0ccc92,0xffeff47d,0x85845dd1,
0x6fa87e4f,0xfe2ce6e0,0xa3014314,0x4e0811a1,0xf7537e82,0xbd3af235,0x2ad7d2bb,0xeb86d391};
static const int MD5_S[64] = {7,12,17,22,7,12,17,22,7,12,17,22,7,12,17,22,
                              5,9,14,20,5,9,14,20,5,9,14,20,5,9,14,20,
                              4,11,16,23,4,11,16,23,4,11,16,23,4,11,16,23,
                              6,10,15,21,6,10,15,21,6,10,15,21,6,10,15,21};

static uint32_t rotl32(uint32_t x, int c) { return (x << c) | (x >> (32 - c)); }

static void zmd5_block(ZMd5* s, const unsigned char* p) {
    uint32_t w[16];
    for (int i = 0; i < 16; i++)
        w[i] = (uint32_t)p[i*4] | ((uint32_t)p[i*4+1] << 8) | ((uint32_t)p[i*4+2] << 16) | ((uint32_t)p[i*4+3] << 24);
    uint32_t a = s->h[0], b = s->h[1], c = s->h[2], d = s->h[3];
    for (int i = 0; i < 64; i++) {
        uint32_t f; int g;
        if (i < 16) { f = (b & c) | (~b & d); g = i; }
        else if (i < 32) { f = (d & b) | (~d & c); g = (5*i + 1) % 16; }
        else if (i < 48) { f = b ^ c ^ d; g = (3*i + 5) % 16; }
        else { f = c ^ (b | ~d); g = (7*i) % 16; }
        uint32_t tmp = d;
        d = c; c = b;
        b = b + rotl32(a + f + MD5_K[i] + w[g], MD5_S[i]);
        a = tmp;
    }
    s->h[0] += a; s->h[1] += b; s->h[2] += c; s->h[3] += d;
}

static void zmd5_init(ZMd5* s) {
    s->h[0] = 0x67452301; s->h[1] = 0xefcdab89; s->h[2] = 0x98badcfe; s->h[3] = 0x10325476;
    s->len = 0; s->n = 0;
}

static void zmd5_update(ZMd5* s, const unsigned char* p, size_t n) {
    s->len += n;
    while (n > 0) {
        size_t take = 64 - s->n;
        if (take > n) take = n;
        memcpy(s->buf + s->n, p, take);
        s->n += take; p += take; n -= take;
        if (s->n == 64) { zmd5_block(s, s->buf); s->n = 0; }
    }
}

static void zmd5_final(ZMd5* s, unsigned char out[16]) {
    uint64_t bits = s->len * 8;
    unsigned char pad = 0x80;
    zmd5_update(s, &pad, 1);
    unsigned char zero = 0;
    while (s->n != 56) zmd5_update(s, &zero, 1);
    unsigned char lenb[8];
    for (int i = 0; i < 8; i++) lenb[i] = (unsigned char)(bits >> (8*i));
    zmd5_update(s, lenb, 8);
    for (int i = 0; i < 4; i++) {
        out[i*4] = (unsigned char)(s->h[i]);
        out[i*4+1] = (unsigned char)(s->h[i] >> 8);
        out[i*4+2] = (unsigned char)(s->h[i] >> 16);
        out[i*4+3] = (unsigned char)(s->h[i] >> 24);
    }
}

static ZVal z_hex_bytes(const unsigned char* p, int n) {
    char* out = (char*)z_alloc((size_t)n * 2 + 1);
    const char* H = "0123456789abcdef";
    for (int i = 0; i < n; i++) {
        out[i*2] = H[p[i] >> 4];
        out[i*2+1] = H[p[i] & 15];
    }
    out[n*2] = 0;
    return z_str_take(out, n * 2);
}

ZVal z_md5(ZVal v) {
    ZStr* s = zs_arg(v, "md5");
    ZMd5 st; zmd5_init(&st);
    zmd5_update(&st, (const unsigned char*)s->data, (size_t)s->len);
    unsigned char out[16];
    zmd5_final(&st, out);
    return z_hex_bytes(out, 16);
}

/* ── SHA1 ── */
typedef struct { uint32_t h[5]; uint64_t len; unsigned char buf[64]; size_t n; } ZSha1;

static void zsha1_block(ZSha1* s, const unsigned char* p) {
    uint32_t w[80];
    for (int i = 0; i < 16; i++)
        w[i] = ((uint32_t)p[i*4] << 24) | ((uint32_t)p[i*4+1] << 16) | ((uint32_t)p[i*4+2] << 8) | (uint32_t)p[i*4+3];
    for (int i = 16; i < 80; i++) w[i] = rotl32(w[i-3] ^ w[i-8] ^ w[i-14] ^ w[i-16], 1);
    uint32_t a = s->h[0], b = s->h[1], c = s->h[2], d = s->h[3], e = s->h[4];
    for (int i = 0; i < 80; i++) {
        uint32_t f, k;
        if (i < 20) { f = (b & c) | (~b & d); k = 0x5A827999; }
        else if (i < 40) { f = b ^ c ^ d; k = 0x6ED9EBA1; }
        else if (i < 60) { f = (b & c) | (b & d) | (c & d); k = 0x8F1BBCDC; }
        else { f = b ^ c ^ d; k = 0xCA62C1D6; }
        uint32_t tmp = rotl32(a, 5) + f + e + k + w[i];
        e = d; d = c; c = rotl32(b, 30); b = a; a = tmp;
    }
    s->h[0] += a; s->h[1] += b; s->h[2] += c; s->h[3] += d; s->h[4] += e;
}

static void zsha1_init(ZSha1* s) {
    s->h[0] = 0x67452301; s->h[1] = 0xEFCDAB89; s->h[2] = 0x98BADCFE; s->h[3] = 0x10325476; s->h[4] = 0xC3D2E1F0;
    s->len = 0; s->n = 0;
}
static void zsha1_update(ZSha1* s, const unsigned char* p, size_t n) {
    s->len += n;
    while (n > 0) {
        size_t take = 64 - s->n;
        if (take > n) take = n;
        memcpy(s->buf + s->n, p, take);
        s->n += take; p += take; n -= take;
        if (s->n == 64) { zsha1_block(s, s->buf); s->n = 0; }
    }
}
static void zsha1_final(ZSha1* s, unsigned char out[20]) {
    uint64_t bits = s->len * 8;
    unsigned char pad = 0x80;
    zsha1_update(s, &pad, 1);
    unsigned char zero = 0;
    while (s->n != 56) zsha1_update(s, &zero, 1);
    unsigned char lenb[8];
    for (int i = 0; i < 8; i++) lenb[i] = (unsigned char)(bits >> (8*(7-i)));
    zsha1_update(s, lenb, 8);
    for (int i = 0; i < 5; i++) {
        out[i*4] = (unsigned char)(s->h[i] >> 24);
        out[i*4+1] = (unsigned char)(s->h[i] >> 16);
        out[i*4+2] = (unsigned char)(s->h[i] >> 8);
        out[i*4+3] = (unsigned char)(s->h[i]);
    }
}

ZVal z_sha1(ZVal v) {
    ZStr* s = zs_arg(v, "sha1");
    ZSha1 st; zsha1_init(&st);
    zsha1_update(&st, (const unsigned char*)s->data, (size_t)s->len);
    unsigned char out[20];
    zsha1_final(&st, out);
    return z_hex_bytes(out, 20);
}

/* ── SHA256 ── */
typedef struct { uint32_t h[8]; uint64_t len; unsigned char buf[64]; size_t n; } ZSha256;
static const uint32_t SHA256_K[64] = {
0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2};

static uint32_t rotr32(uint32_t x, int c) { return (x >> c) | (x << (32 - c)); }

static void zsha256_block(ZSha256* s, const unsigned char* p) {
    uint32_t w[64];
    for (int i = 0; i < 16; i++)
        w[i] = ((uint32_t)p[i*4] << 24) | ((uint32_t)p[i*4+1] << 16) | ((uint32_t)p[i*4+2] << 8) | (uint32_t)p[i*4+3];
    for (int i = 16; i < 64; i++) {
        uint32_t s0 = rotr32(w[i-15], 7) ^ rotr32(w[i-15], 18) ^ (w[i-15] >> 3);
        uint32_t s1 = rotr32(w[i-2], 17) ^ rotr32(w[i-2], 19) ^ (w[i-2] >> 10);
        w[i] = w[i-16] + s0 + w[i-7] + s1;
    }
    uint32_t a = s->h[0], b = s->h[1], c = s->h[2], d = s->h[3];
    uint32_t e = s->h[4], f = s->h[5], g = s->h[6], h = s->h[7];
    for (int i = 0; i < 64; i++) {
        uint32_t S1 = rotr32(e, 6) ^ rotr32(e, 11) ^ rotr32(e, 25);
        uint32_t ch = (e & f) ^ (~e & g);
        uint32_t t1 = h + S1 + ch + SHA256_K[i] + w[i];
        uint32_t S0 = rotr32(a, 2) ^ rotr32(a, 13) ^ rotr32(a, 22);
        uint32_t maj = (a & b) ^ (a & c) ^ (b & c);
        uint32_t t2 = S0 + maj;
        h = g; g = f; f = e; e = d + t1;
        d = c; c = b; b = a; a = t1 + t2;
    }
    s->h[0] += a; s->h[1] += b; s->h[2] += c; s->h[3] += d;
    s->h[4] += e; s->h[5] += f; s->h[6] += g; s->h[7] += h;
}

static void zsha256_init(ZSha256* s) {
    s->h[0]=0x6a09e667; s->h[1]=0xbb67ae85; s->h[2]=0x3c6ef372; s->h[3]=0xa54ff53a;
    s->h[4]=0x510e527f; s->h[5]=0x9b05688c; s->h[6]=0x1f83d9ab; s->h[7]=0x5be0cd19;
    s->len = 0; s->n = 0;
}
static void zsha256_update(ZSha256* s, const unsigned char* p, size_t n) {
    s->len += n;
    while (n > 0) {
        size_t take = 64 - s->n;
        if (take > n) take = n;
        memcpy(s->buf + s->n, p, take);
        s->n += take; p += take; n -= take;
        if (s->n == 64) { zsha256_block(s, s->buf); s->n = 0; }
    }
}
static void zsha256_final(ZSha256* s, unsigned char out[32]) {
    uint64_t bits = s->len * 8;
    unsigned char pad = 0x80;
    zsha256_update(s, &pad, 1);
    unsigned char zero = 0;
    while (s->n != 56) zsha256_update(s, &zero, 1);
    unsigned char lenb[8];
    for (int i = 0; i < 8; i++) lenb[i] = (unsigned char)(bits >> (8*(7-i)));
    zsha256_update(s, lenb, 8);
    for (int i = 0; i < 8; i++) {
        out[i*4] = (unsigned char)(s->h[i] >> 24);
        out[i*4+1] = (unsigned char)(s->h[i] >> 16);
        out[i*4+2] = (unsigned char)(s->h[i] >> 8);
        out[i*4+3] = (unsigned char)(s->h[i]);
    }
}

ZVal z_sha256(ZVal v) {
    ZStr* s = zs_arg(v, "sha256");
    ZSha256 st; zsha256_init(&st);
    zsha256_update(&st, (const unsigned char*)s->data, (size_t)s->len);
    unsigned char out[32];
    zsha256_final(&st, out);
    return z_hex_bytes(out, 32);
}

/* ══════════════════════════════════════════════════════════
 * 格式化（%d %s %f %.Nf %x %X %v %c %% 子集）
 * ══════════════════════════════════════════════════════════ */

static double z_round_half_even(double x, int prec) {
    double scale = 1.0;
    for (int i = 0; i < prec; i++) scale *= 10.0;
    double scaled = x * scale;
    double lower = floor(scaled);
    double frac = scaled - lower;
    if (frac > 0.5 || (frac == 0.5 && fmod(lower, 2.0) != 0.0)) lower += 1.0;
    return lower / scale;
}

ZVal z_format(int argc, ZVal* argv) {
    if (argc < 1 || argv[0].kind != ZK_STR) z_die("E226", "格式化() 第一个参数必须是字符串模板");
    ZStr* fmt = argv[0].as.s;
    ZBuf b; buf_init(&b);
    int argi = 1;
    for (int64_t i = 0; i < fmt->len; i++) {
        char c = fmt->data[i];
        if (c != '%') { buf_putc(&b, c); continue; }
        if (i + 1 >= fmt->len) { buf_putc(&b, '%'); break; }
        /* 解析 %.Nf */
        int prec = -1;
        int j = i + 1;
        if (fmt->data[j] == '.') {
            j++;
            prec = 0;
            while (j < (int)fmt->len && fmt->data[j] >= '0' && fmt->data[j] <= '9') {
                prec = prec * 10 + (fmt->data[j] - '0');
                j++;
            }
        }
        if (j >= fmt->len) { buf_putc(&b, '%'); break; }
        char conv = fmt->data[j];
        i = j;
        if (conv == '%') { buf_putc(&b, '%'); continue; }
        if (argi >= argc) { buf_putc(&b, '%'); buf_putc(&b, conv); continue; }
        ZVal a = argv[argi++];
        char tmp[128];
        switch (conv) {
            case 'd': case 'i':
                if (a.kind == ZK_INT) snprintf(tmp, sizeof(tmp), "%lld", (long long)a.as.i);
                else if (a.kind == ZK_FLOAT) snprintf(tmp, sizeof(tmp), "%lld", (long long)a.as.f);
                else snprintf(tmp, sizeof(tmp), "0");
                buf_puts(&b, tmp);
                break;
            case 'f': {
                double d = (a.kind == ZK_FLOAT) ? a.as.f : ((a.kind == ZK_INT) ? (double)a.as.i : 0.0);
                if (prec < 0) prec = 6;
                d = z_round_half_even(d, prec);
                snprintf(tmp, sizeof(tmp), "%.*f", prec, d);
                buf_puts(&b, tmp);
                break;
            }
            case 'x': case 'X': {
                long long n = (a.kind == ZK_INT) ? (long long)a.as.i : 0;
                snprintf(tmp, sizeof(tmp), conv == 'x' ? "%llx" : "%llX", n);
                buf_puts(&b, tmp);
                break;
            }
            case 'c': {
                long long n = (a.kind == ZK_INT) ? (long long)a.as.i : 0;
                buf_putc(&b, (char)n);
                break;
            }
            case 's': case 'v': default: {
                ZVal sv = z_to_str(a);
                buf_putn(&b, sv.as.s->data, sv.as.s->len);
                break;
            }
        }
    }
    return z_str_take(b.data, b.len);
}

/* ══════════════════════════════════════════════════════════
 * 目录 / 环境
 * ══════════════════════════════════════════════════════════ */

ZVal z_read_dir(ZVal pv) {
    if (pv.kind != ZK_STR) z_die("E227", "读目录() 参数必须是字符串");
    ZVal out = z_array();
#ifdef _WIN32
    char pat[4096];
    snprintf(pat, sizeof(pat), "%s\\*", pv.as.s->data);
    WIN32_FIND_DATAA fd;
    HANDLE h = FindFirstFileA(pat, &fd);
    if (h == INVALID_HANDLE_VALUE) z_die("E228", "读目录() 失败: 无法打开目录");
    do {
        if (strcmp(fd.cFileName, ".") == 0 || strcmp(fd.cFileName, "..") == 0) continue;
        char name[1024];
        if (fd.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY) snprintf(name, sizeof(name), "%s/", fd.cFileName);
        else snprintf(name, sizeof(name), "%s", fd.cFileName);
        z_array_push(out, z_str(name));
    } while (FindNextFileA(h, &fd));
    FindClose(h);
#else
    z_die("E228", "读目录() 当前平台未实现");
#endif
    return out;
}

ZVal z_getenv(ZVal pv) {
    if (pv.kind != ZK_STR) z_die("E229", "环境变量() 参数必须是字符串");
    const char* v = getenv(pv.as.s->data);
    return v ? z_str(v) : z_str("");
}
