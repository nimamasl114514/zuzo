package runtime

// zlib_mod.go — zlib / gzip 模块的 Go 原生实现（第14轮）
//
// 触发点：urllib3/response.py:11 `import zlib`（HTTP 内容解压）。
// wbits 语义与 CPython 一致：15 = zlib 容器，31 = gzip 容器，-15 = raw deflate。

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"hash/adler32"
	"hash/crc32"
	"io"

	"zuzo/py/object"
)

func init() {
	builtinModuleInits["zlib"] = modZlib
	builtinModuleInits["gzip"] = modGzip
}

func modZlib(ip *Interp) *object.Module {
	m := newModule("zlib")
	m.Dict["Z_OK"] = &object.Int{V: 0}
	m.Dict["Z_STREAM_END"] = &object.Int{V: 1}
	m.Dict["Z_BEST_COMPRESSION"] = &object.Int{V: 9}
	m.Dict["Z_BEST_SPEED"] = &object.Int{V: 1}
	m.Dict["Z_DEFAULT_COMPRESSION"] = &object.Int{V: -1}
	m.Dict["MAX_WBITS"] = &object.Int{V: 15}
	m.Dict["DEFLATED"] = &object.Int{V: 8}
	m.Dict["DEF_MEM_LEVEL"] = &object.Int{V: 8}

	b2b := func(o object.Object) []byte {
		switch v := o.(type) {
		case *object.Bytes:
			return v.V
		case *object.ByteArray:
			return v.V
		case *object.MemoryView:
			return v.V
		case *object.Str:
			return []byte(v.V)
		}
		return []byte(o.Str())
	}

	m.Dict["compress"] = bi("compress", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return &object.Bytes{}, nil
		}
		var buf bytes.Buffer
		w, err := zlib.NewWriterLevel(&buf, flate.DefaultCompression)
		if err != nil {
			ip.raise(ip.excType("zlib.error"), "%v", err)
		}
		w.Write(b2b(a[0]))
		w.Close()
		return &object.Bytes{V: buf.Bytes()}, nil
	})

	m.Dict["decompress"] = bi("decompress", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return &object.Bytes{}, nil
		}
		data := b2b(a[0])
		wbits := int64(15)
		if len(a) > 1 {
			wbits, _ = object.AsInt(a[1])
		}
		var r io.ReadCloser
		var err error
		switch {
		case wbits >= 24 && wbits <= 31: // gzip 容器
			r, err = gzip.NewReader(bytes.NewReader(data))
		case wbits < 0: // raw deflate
			r = flate.NewReader(bytes.NewReader(data))
		default: // zlib 容器
			r, err = zlib.NewReader(bytes.NewReader(data))
		}
		if err != nil {
			ip.raise(ip.excType("zlib.error"), "%v", err)
		}
		out, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			ip.raise(ip.excType("zlib.error"), "%v", err)
		}
		return &object.Bytes{V: out}, nil
	})

	m.Dict["crc32"] = bi("crc32", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return &object.Int{V: 0}, nil
		}
		var prev uint32
		if len(a) > 1 {
			if n, ok := object.AsInt(a[1]); ok {
				prev = uint32(n)
			}
		}
		return &object.Int{V: int64(crc32.Update(prev, crc32.IEEETable, b2b(a[0])))}, nil
	})

	m.Dict["adler32"] = bi("adler32", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return &object.Int{V: 1}, nil
		}
		// Go 的 adler32 不支持续算（无 Update），忽略 prev——调用方基本都不传
		return &object.Int{V: int64(adler32.Checksum(b2b(a[0])))}, nil
	})
	return m
}

func modGzip(ip *Interp) *object.Module {
	m := newModule("gzip")
	m.Dict["compress"] = bi("compress", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return &object.Bytes{}, nil
		}
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		switch v := a[0].(type) {
		case *object.Bytes:
			w.Write(v.V)
		case *object.Str:
			w.Write([]byte(v.V))
		default:
			w.Write([]byte(a[0].Str()))
		}
		w.Close()
		return &object.Bytes{V: buf.Bytes()}, nil
	})
	m.Dict["decompress"] = bi("decompress", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return &object.Bytes{}, nil
		}
		var data []byte
		switch v := a[0].(type) {
		case *object.Bytes:
			data = v.V
		case *object.ByteArray:
			data = v.V
		default:
			data = []byte(a[0].Str())
		}
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		out, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		return &object.Bytes{V: out}, nil
	})
	return m
}
