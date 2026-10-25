def handle(x):
    match x:
        case 0:
            return "zero"
        case 1 | 2:
            return "small"
        case [a, b]:
            return f"list {a} {b}"
        case {"k": val}:
            return f"dict {val}"
        case _:
            return "other"

for arg in (0, 3, [3, 4], {"k": 9}, "s"):
    print(handle(arg))
