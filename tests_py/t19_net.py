from urllib.parse import quote, unquote, urlencode, quote_plus
import glob
import os
import tempfile
import uuid

print(quote("a b/c&d"))
print(quote_plus("a b"))
print(unquote("a%20b%2Fc"))
print(urlencode({"a": 1, "b": "x y"}))

print(os.path.isabs("C:/x"))
print(os.path.normpath("a/./b/../c"))
print(os.path.relpath("a/b/c", "a"))

d = tempfile.mkdtemp()
f = os.path.join(d, "a.txt")
open(f, "w").write("hi")
print(os.path.basename(f))
print(len(glob.glob(os.path.join(d, "*.txt"))))
os.remove(f)
os.rmdir(d)
print("temp ok")

u = uuid.uuid4()
s = str(u)
print(len(s), s.count("-"), len(u.hex))
print(uuid.uuid4() != uuid.uuid4())
