try:
    x = 1 / 0
except ZeroDivisionError as e:
    print("caught:", type(e).__name__, e)

try:
    arr = [1]
    print(arr[5])
except IndexError:
    print("index error")

try:
    {}["missing"]
except KeyError as e:
    print("key error:", e)

try:
    int("abc")
except ValueError:
    print("value error")

try:
    raise RuntimeError("boom")
except Exception as e:
    print("raised:", e)


class MyError(Exception):
    pass


class AppError(MyError):
    def __init__(self, code, msg):
        super().__init__(msg)
        self.code = code


try:
    raise AppError(404, "not found")
except AppError as e:
    print("app error:", e.code, e, isinstance(e, MyError), isinstance(e, Exception))

try:
    try:
        raise ValueError("inner")
    finally:
        print("finally runs")

except ValueError as e:
    print("outer caught:", e)

try:
    pass
except Exception:
    print("no")
else:
    print("else runs")

try:
    x = 1
finally:
    print("finally alone")

result = []
for i in range(3):
    try:
        if i == 1:
            continue
        result.append(i)
    finally:
        result.append(f"f{i}")
print(result)

def safe_div(a, b):
    try:
        return a / b
    except ZeroDivisionError:
        return None
    except TypeError:
        return "type"

print(safe_div(6, 3), safe_div(1, 0), safe_div("a", 0))

try:
    assert 1 == 2, "assertion msg"
except AssertionError as e:
    print("assert:", e)

print("end")
