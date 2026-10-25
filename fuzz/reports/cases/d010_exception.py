def f(mode):
    if mode == 1:
        return 1 / 0
    if mode == 2:
        return int("not-a-number")
    if mode == 3:
        raise KeyError("missing")
    return 0

for m in (1, 2, 3, 4):
    try:
        print("v", f(m))
    except ZeroDivisionError as e:
        print("ZDE", type(e).__name__)
    except (ValueError, KeyError) as e:
        print("EXC", type(e).__name__, str(e).strip("'\""))
    else:
        print("ok")
    finally:
        print("fin", m)
