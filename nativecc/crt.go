package nativecc

// BuiltinHeaders 内置的极简系统头（只给声明，实现由 msvcrt.dll / kernel32.dll 提供）
var BuiltinHeaders = map[string]string{
	"stddef.h": `
#ifndef _NATIVECC_STDDEF_H
#define _NATIVECC_STDDEF_H
typedef unsigned long long size_t;
typedef long long ptrdiff_t;
#define NULL ((void*)0)
#define offsetof(t, m) __builtin_offsetof(t, m)
#endif
`,
	"stdint.h": `
#ifndef _NATIVECC_STDINT_H
#define _NATIVECC_STDINT_H
typedef signed char int8_t;
typedef unsigned char uint8_t;
typedef short int16_t;
typedef unsigned short uint16_t;
typedef int int32_t;
typedef unsigned int uint32_t;
typedef long long int64_t;
typedef unsigned long long uint64_t;
typedef long long intptr_t;
typedef unsigned long long uintptr_t;
typedef long long intmax_t;
typedef unsigned long long uintmax_t;
#define INT64_MAX 9223372036854775807LL
#define INT64_MIN (-9223372036854775807LL - 1)
#define INT32_MAX 2147483647
#define UINT32_MAX 4294967295U
#endif
`,
	"stdio.h": `
#ifndef _NATIVECC_STDIO_H
#define _NATIVECC_STDIO_H
#include <stddef.h>
typedef struct _iobuf { char _reserved[48]; } FILE;
extern FILE* __iob_func(void);
#define stdin  (&__iob_func()[0])
#define stdout (&__iob_func()[1])
#define stderr (&__iob_func()[2])
#define EOF (-1)
#define SEEK_SET 0
#define SEEK_CUR 1
#define SEEK_END 2
FILE* fopen(const char* name, const char* mode);
int fclose(FILE* f);
size_t fread(void* ptr, size_t size, size_t n, FILE* f);
size_t fwrite(const void* ptr, size_t size, size_t n, FILE* f);
int fseek(FILE* f, long off, int whence);
long ftell(FILE* f);
int fputc(int c, FILE* f);
int fputs(const char* s, FILE* f);
int printf(const char* fmt, ...);
int sprintf(char* buf, const char* fmt, ...);
int _snprintf(char* buf, size_t n, const char* fmt, ...);
#define snprintf _snprintf
int remove(const char* path);
int rename(const char* a, const char* b);
int puts(const char* s);
int _fileno(FILE* f);
#endif
`,
	"stdlib.h": `
#ifndef _NATIVECC_STDLIB_H
#define _NATIVECC_STDLIB_H
#include <stddef.h>
#define EXIT_SUCCESS 0
#define EXIT_FAILURE 1
#define RAND_MAX 32767
void* malloc(size_t n);
void* calloc(size_t n, size_t sz);
void* realloc(void* p, size_t n);
void free(void* p);
void exit(int code);
int rand(void);
void srand(unsigned seed);
char* getenv(const char* name);
double strtod(const char* s, char** endp);
long long _strtoi64(const char* s, char** endp, int base);
#define strtoll _strtoi64
int abs(int x);
#endif
`,
	"string.h": `
#ifndef _NATIVECC_STRING_H
#define _NATIVECC_STRING_H
#include <stddef.h>
void* memcpy(void* dst, const void* src, size_t n);
void* memmove(void* dst, const void* src, size_t n);
void* memset(void* dst, int c, size_t n);
int memcmp(const void* a, const void* b, size_t n);
void* memchr(const void* s, int c, size_t n);
size_t strlen(const char* s);
int strcmp(const char* a, const char* b);
int strncmp(const char* a, const char* b, size_t n);
char* strcpy(char* d, const char* s);
char* strncpy(char* d, const char* s, size_t n);
char* strcat(char* d, const char* s);
char* strstr(const char* hay, const char* needle);
char* strchr(const char* s, int c);
char* strrchr(const char* s, int c);
#endif
`,
	"math.h": `
#ifndef _NATIVECC_MATH_H
#define _NATIVECC_MATH_H
double pow(double a, double b);
double sqrt(double x);
double sin(double x);
double cos(double x);
double log(double x);
double ceil(double x);
double floor(double x);
double fmod(double a, double b);
double fabs(double x);
#define trunc(x) ((x) >= 0 ? floor(x) : ceil(x))
#define round(x) ((x) >= 0 ? floor((x) + 0.5) : ceil((x) - 0.5))
#define isnan(x) ((x) != (x))
#define isinf(x) (((x) != (x)) ? 0 : ((((x) - (x)) != 0)))
#endif
`,
	"time.h": `
#ifndef _NATIVECC_TIME_H
#define _NATIVECC_TIME_H
#include <stddef.h>
typedef long long time_t;
typedef long long clock_t;
struct tm {
    int tm_sec;
    int tm_min;
    int tm_hour;
    int tm_mday;
    int tm_mon;
    int tm_year;
    int tm_wday;
    int tm_yday;
    int tm_isdst;
};
time_t time(time_t* out);
clock_t clock(void);
struct tm* localtime(const time_t* t);
int _localtime64_s(struct tm* out, const time_t* t);
#define localtime_s(a, b) _localtime64_s((a), (b))
size_t strftime(char* buf, size_t cap, const char* fmt, const struct tm* t);
#endif
`,
	"setjmp.h": `
#ifndef _NATIVECC_SETJMP_H
#define _NATIVECC_SETJMP_H
typedef char jmp_buf[16];
int setjmp(jmp_buf env);
void longjmp(jmp_buf env, int val);
#endif
`,
	"fcntl.h": `
#ifndef _NATIVECC_FCNTL_H
#define _NATIVECC_FCNTL_H
#define _O_RDONLY 0
#define _O_WRONLY 1
#define _O_RDWR 2
#define _O_BINARY 0x8000
#endif
`,
	"io.h": `
#ifndef _NATIVECC_IO_H
#define _NATIVECC_IO_H
int _setmode(int fd, int mode);
#endif
`,
	"windows.h": `
#ifndef _NATIVECC_WINDOWS_H
#define _NATIVECC_WINDOWS_H
typedef unsigned long DWORD;
typedef int BOOL;
typedef void* HANDLE;
typedef char* LPSTR;
typedef const char* LPCSTR;
#define INVALID_HANDLE_VALUE ((HANDLE)(long long)-1)
#define FILE_ATTRIBUTE_DIRECTORY 0x10
typedef struct _FILETIME {
    DWORD dwLowDateTime;
    DWORD dwHighDateTime;
} FILETIME;
typedef struct _WIN32_FIND_DATAA {
    DWORD dwFileAttributes;
    FILETIME ftCreationTime;
    FILETIME ftLastAccessTime;
    FILETIME ftLastWriteTime;
    DWORD nFileSizeHigh;
    DWORD nFileSizeLow;
    DWORD dwReserved0;
    DWORD dwReserved1;
    char cFileName[260];
    char cAlternateFileName[14];
} WIN32_FIND_DATAA;
HANDLE FindFirstFileA(const char* name, WIN32_FIND_DATAA* data);
int FindNextFileA(HANDLE h, WIN32_FIND_DATAA* data);
int FindClose(HANDLE h);
void Sleep(DWORD ms);
DWORD GetTickCount(void);
void ExitProcess(unsigned code);
#endif
`,
	"errno.h": `
#ifndef _NATIVECC_ERRNO_H
#define _NATIVECC_ERRNO_H
extern int errno;
#endif
`,
}

// ImportSym 导入符号描述
type ImportSym struct {
	DLL    string // 目标 DLL（小写）
	Export string // 实际导出名（空 = 与源符号同名）
	NArgs  int    // 参数个数；-1 = 可变参数
}

// Imports 外部符号 → DLL 映射表（由 msvcrt.dll / kernel32.dll 提供实现）
var Imports = map[string]ImportSym{
	// ── 内存 ──
	"malloc":    {"msvcrt.dll", "", 1},
	"calloc":    {"msvcrt.dll", "", 2},
	"realloc":   {"msvcrt.dll", "", 2},
	"free":      {"msvcrt.dll", "", 1},
	"memcpy":    {"msvcrt.dll", "", 3},
	"memmove":   {"msvcrt.dll", "", 3},
	"memset":    {"msvcrt.dll", "", 3},
	"memcmp":    {"msvcrt.dll", "", 3},
	"memchr":    {"msvcrt.dll", "", 3},
	// ── 字符串 ──
	"strlen":  {"msvcrt.dll", "", 1},
	"strcmp":  {"msvcrt.dll", "", 2},
	"strncmp": {"msvcrt.dll", "", 3},
	"strcpy":  {"msvcrt.dll", "", 2},
	"strncpy": {"msvcrt.dll", "", 3},
	"strcat":  {"msvcrt.dll", "", 2},
	"strstr":  {"msvcrt.dll", "", 2},
	"strchr":  {"msvcrt.dll", "", 2},
	"strrchr": {"msvcrt.dll", "", 2},
	"strtod":  {"msvcrt.dll", "", 2},
	"_strtoi64": {"msvcrt.dll", "", 3},
	// ── IO ──
	"fopen":   {"msvcrt.dll", "", 2},
	"fclose":  {"msvcrt.dll", "", 1},
	"fread":   {"msvcrt.dll", "", 4},
	"fwrite":  {"msvcrt.dll", "", 4},
	"fseek":   {"msvcrt.dll", "", 3},
	"ftell":   {"msvcrt.dll", "", 1},
	"fputc":   {"msvcrt.dll", "", 2},
	"fputs":   {"msvcrt.dll", "", 2},
	"fprintf": {"msvcrt.dll", "", -1},
	"remove":  {"msvcrt.dll", "", 1},
	"rename":  {"msvcrt.dll", "", 2},
	"puts":    {"msvcrt.dll", "", 1},
	"_fileno": {"msvcrt.dll", "", 1},
	"_setmode": {"msvcrt.dll", "", 2},
	"__iob_func": {"msvcrt.dll", "", 0},
	// ── 格式化（可变参数，MS ABI）──
	"printf":    {"msvcrt.dll", "", -1},
	"sprintf":   {"msvcrt.dll", "", -1},
	"_snprintf": {"msvcrt.dll", "", -1},
	// ── 进程 ──
	"exit":  {"msvcrt.dll", "", 1},
	"getenv": {"msvcrt.dll", "", 1},
	"rand":   {"msvcrt.dll", "", 0},
	"srand":  {"msvcrt.dll", "", 1},
	// ── 数学 ──
	"pow":   {"msvcrt.dll", "", 2},
	"sqrt":  {"msvcrt.dll", "", 1},
	"sin":   {"msvcrt.dll", "", 1},
	"cos":   {"msvcrt.dll", "", 1},
	"log":   {"msvcrt.dll", "", 1},
	"ceil":  {"msvcrt.dll", "", 1},
	"floor": {"msvcrt.dll", "", 1},
	"fmod":  {"msvcrt.dll", "", 2},
	"fabs":  {"msvcrt.dll", "", 1},
	// ── 时间 ──
	"time":          {"msvcrt.dll", "", 1},
	"clock":         {"msvcrt.dll", "", 0},
	"localtime":     {"msvcrt.dll", "", 1},
	"_localtime64_s": {"msvcrt.dll", "", 2},
	"strftime":      {"msvcrt.dll", "", 4},
	// ── 跳转 ──
	"setjmp":  {"msvcrt.dll", "", 1},
	"longjmp": {"msvcrt.dll", "", 2},
	// ── Win32 ──
	"FindFirstFileA": {"kernel32.dll", "", 2},
	"FindNextFileA":  {"kernel32.dll", "", 2},
	"FindClose":      {"kernel32.dll", "", 1},
	"Sleep":          {"kernel32.dll", "", 1},
	"GetTickCount":   {"kernel32.dll", "", 0},
	"ExitProcess":    {"kernel32.dll", "", 1},
}

// Builtins 编译器内建函数（不产生外部导入）
var Builtins = map[string]bool{
	"__builtin_add_overflow": true,
	"__builtin_sub_overflow": true,
	"__builtin_mul_overflow": true,
	"__builtin_offsetof":     true,
	"__builtin_isinf":        true,
	"__builtin_isnan":        true,
}
