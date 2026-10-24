def classify(x):
    match x:
        case 0:
            return "zero"
        case 1 | 2 | 3:
            return "small"
        case -1:
            return "neg one"
        case int() if x > 100:
            return "big"
        case int():
            return "int"
        case str():
            return "string"
        case None:
            return "none"
        case _:
            return "other"

for v in [0, 2, -1, 500, 42, "s", None, 3.5, [1]]:
    print(repr(v), classify(v))
print("---")

def route(cmd):
    match cmd:
        case {"action": "move", "to": (x, y)}:
            return f"move to {x},{y}"
        case {"action": "stop"}:
            return "stop"
        case {"action": act, **extra}:
            return f"other {act} {sorted(extra)}"
        case _:
            return "invalid"

print(route({"action": "move", "to": (1, 2)}))
print(route({"action": "stop", "why": "x"}))
print(route({"action": "jump", "h": 3}))
print(route("nope"))
print("---")

match 5:
    case n if n % 2:
        print("odd", n)
    case _:
        print("even")

match (1, 2):
    case (a, b) if a < b:
        print("asc", a, b)

match "cmd":
    case "cmd" as c:
        print("as", c)
