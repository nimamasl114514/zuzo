import math
import json
import re
import os
import sys
import collections
import itertools
import functools
import copy
import hashlib
import base64
import random
import string

print(math.pi)
print(math.sqrt(16), math.floor(3.7), math.ceil(3.2), math.fabs(-5))
print(math.pow(2, 8), math.gcd(12, 18), math.factorial(5))
print(math.isnan(float("nan")), math.isinf(float("inf")))

print(json.dumps({"a": 1, "b": [1, 2], "c": None, "d": True}))
print(json.dumps({"a": 1}, indent=2))
parsed = json.loads('{"x": 1, "y": [2, 3], "z": "s"}')
print(parsed["x"], parsed["y"], parsed["z"])

print(re.findall(r"\d+", "a1 b22 c333"))
print(re.sub(r"\d", "#", "a1b2"))
m = re.search(r"(\w+)@(\w+)", "mail: user@example")
print(m.group(0), m.group(1), m.group(2), m.groups())
print(re.split(r"[,;]", "a,b;c"))
print(re.match(r"\d+", "123abc") is not None)

print(os.path.join("a", "b"))
print(os.path.basename("C:/x/y.txt"), os.path.splitext("y.txt"))
print("a" in sys.version)

c = collections.Counter("abracadabra")
print(c.most_common(2))
dd = collections.defaultdict(list)
dd["x"].append(1)
print(dict(dd))
dq = collections.deque([1, 2, 3])
dq.appendleft(0)
print(list(dq))

print(list(itertools.chain([1], [2, 3])))
print(list(itertools.combinations([1, 2, 3], 2)))
print(list(itertools.permutations([1, 2], 2)))
print(list(itertools.islice(range(100), 3)))

print(functools.reduce(lambda a, b: a + b, [1, 2, 3, 4]))
add10 = functools.partial(lambda a, b: a + b, 10)
print(add10(5))

orig = {"a": [1, 2]}
sh = copy.copy(orig)
dp = copy.deepcopy(orig)
orig["a"].append(3)
print(sh["a"], dp["a"])

print(hashlib.md5(b"abc").hexdigest())
print(hashlib.sha256("abc".encode()).hexdigest()[:16])
print(base64.b64encode(b"hello"))
print(base64.b64decode("aGVsbG8="))

random.seed(42)
print(random.randint(1, 6) in list(range(1, 7)))
print(random.choice([1, 2, 3]) in [1, 2, 3])
print(string.ascii_lowercase[:5], string.digits)

print(sorted([{"n": 2}, {"n": 1}], key=lambda x: x["n"]))
print(min([1, 2, 3], key=lambda x: -x))
print(list(filter(None, [0, 1, "", "a", None])))
