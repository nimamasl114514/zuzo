class AppError(Exception):
    pass

class ConfigError(AppError):
    def __init__(self, key, msg="missing key"):
        super().__init__(f"{msg}: {key}")
        self.key = key

def load(key):
    try:
        raise KeyError(key)
    except KeyError as exc:
        raise ConfigError(key) from exc

try:
    load("host")
except ConfigError as e:
    print("caught", type(e).__name__, e, "key=", e.key)
    print("cause", type(e.__cause__).__name__, e.__cause__, e.__suppress_context__)

def quiet():
    try:
        int("abc")
    except ValueError:
        raise AppError("parse failed") from None

try:
    quiet()
except AppError as e:
    print("quiet", e, e.__cause__, e.__suppress_context__)

def walk(err):
    names = []
    cur = err
    while cur is not None:
        names.append(type(cur).__name__)
        cur = cur.__cause__ if cur.__suppress_context__ else (cur.__cause__ or cur.__context__)
    return names

try:
    load("port")
except ConfigError as e:
    print(walk(e))

def guarded(x):
    try:
        if x < 0:
            raise ValueError("negative")
    except ValueError as e:
        print("handle", e)
        return "handled"
    else:
        print("no error")
        return "ok"
    finally:
        print("finally", x)

print(guarded(-1), guarded(1))
