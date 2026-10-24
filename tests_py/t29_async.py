import asyncio

async def fetch(name, delay=0):
    await asyncio.sleep(delay)
    return f"data-{name}"

async def compute(a, b):
    return a + b

async def gen(n):
    for i in range(n):
        await asyncio.sleep(0)
        yield i * i

async def worker(q, out, wid):
    while True:
        item = await q.get()
        if item is None:
            q.task_done()
            break
        out.append((wid, item * 10))
        q.task_done()

async def main():
    print("start")
    print("compute:", await compute(2, 3))
    print(await fetch("users"), await fetch("orders"))
    print(await asyncio.gather(fetch("a"), fetch("b")))

    total = 0
    async for v in gen(4):
        total += v
    print("gen total", total)
    print("squares", [v async for v in gen(3)])

    q = asyncio.Queue()
    out = []
    for i in range(5):
        q.put_nowait(i)
    q.put_nowait(None)
    task = asyncio.create_task(worker(q, out, 1))
    await q.join()
    await task
    print("queue out", out)

    try:
        await asyncio.wait_for(fetch("slow"), timeout=0.01)
    except asyncio.TimeoutError:
        print("timeout ok")
    print("running loop:", asyncio.get_running_loop() is not None)
    print("done")

asyncio.run(main())
tail = fetch("z")
print("after run", asyncio.iscoroutine(tail))
tail.close()
