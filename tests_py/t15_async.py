import asyncio


async def fetch(name, delay=0):
    await asyncio.sleep(delay)
    return f"data-{name}"


async def compute(a, b):
    return a + b


async def main():
    print("start")
    x = await compute(2, 3)
    print("compute:", x)
    r1 = await fetch("users")
    r2 = await fetch("orders")
    print(r1, r2)
    results = await asyncio.gather(fetch("a"), fetch("b"))
    print(results)
    total = 0
    for i in range(3):
        v = await compute(i, i * 10)
        total += v
    print("total:", total)
    print("done")


asyncio.run(main())

coro = fetch("x")
print(asyncio.iscoroutine(coro))
coro.close()
