package nativecc

import "testing"

func TestStr2(t *testing.T) {
	src := `
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    z_print(2, (ZVal[]){z_base64_encode(z_str("hello")), z_base64_decode(z_str("aGVsbG8="))});
    z_print(3, (ZVal[]){zs_ljust(z_str("hi"), z_int(6), z_str("-")), zs_rjust(z_str("hi"), z_int(6), z_str("-")), zs_center(z_str("hi"), z_int(7), z_str("-"))});
    z_print(3, (ZVal[]){zs_remove_prefix(z_str("prefix-body"), z_str("prefix-")), zs_remove_suffix(z_str("a.txt"), z_str(".txt")), zs_replace_n(z_str("aaa"), z_str("a"), z_str("b"), z_int(2)) });
    z_print(2, (ZVal[]){zs_split_n(z_str("a,b,c"), z_str(","), z_int(2)), zs_trim(z_str("  x  "))});
    return 0;
}
`
	exe := compileWithRT(t, "str2", src)
	c, o := runExe(t, exe)
	t.Logf("str2: code=%d out=%q", c, o)
}
