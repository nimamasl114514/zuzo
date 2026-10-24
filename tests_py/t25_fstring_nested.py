name = "world"
n = 7
pi = 3.14159265

print(f"hello {name}")
print(f"{n:03d} {n:x} {n:o} {n:b}")
print(f"{pi:.2f} {pi:10.3f} {pi:<10.3f}| {pi:^10.3f}|")
print(f"{1234567:,} {1234567:_}")
print(f"{0.256:.1%}")

items = ["a", "b", "c"]
print(f"{len(items)} items: {', '.join(items)}")
print(f"{[i * i for i in range(4)]}")

width = 8
prec = 3
print(f"[{pi:{width}.{prec}f}]")
print(f"[{n:{'>'}{width}d}]")
print(f"[{name:{'*'}^11}]")

print(f"{f'{n:02d}'}")
print(f"{f'[{name.upper():>7}]'}")
print(f"{'yes' if n > 5 else 'no'}")
print(f"{name!r} {name!s}")

print(f"{ {i: i * 2 for i in range(3)} }")

data = {"k": "v", "num": 42}
print(f"dict access {data['k']} and {data['num']:04d}")

total = 0
for i in range(1, 5):
    total += i
    print(f"step {i}: partial={total:>3d} pct={i / 4:.0%}")

print(f"""multi
line {name}
value {pi:.1f}""")

print(f"{name=} {n=} {items[1]=}")
print(f"{'nested':{'>'}{10}}|")
print(f"{3.0:.0f} {3.5:.0f} {-2.5:.0f}")
