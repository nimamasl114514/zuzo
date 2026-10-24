import asyncio
import threading
import time


async def worker(n):
    await asyncio.sleep(0.05)
    return n * n


async def main():
    start = time.time()
    results = await asyncio.gather(worker(1), worker(2), worker(3))
    elapsed = time.time() - start
    print(results)
    print("concurrent:", elapsed < 0.14)
    task = asyncio.create_task(worker(9))
    v = await task
    print("task:", v)


asyncio.run(main())

counter = {"n": 0}
lock = threading.Lock()


def inc():
    for _ in range(1000):
        with lock:
            counter["n"] += 1


ts = [threading.Thread(target=inc) for _ in range(4)]
for t in ts:
    t.start()
for t in ts:
    t.join()
print("counter:", counter["n"])

order = []


def job(i):
    order.append(i)


threads = [threading.Thread(target=job, args=(i,)) for i in range(3)]
for t in threads:
    t.start()
for t in threads:
    t.join()
print("threads done:", len(order))

lock2 = threading.Lock()
lock2.acquire()
print("locked")
lock2.release()
with threading.Lock():
    print("with lock ok")
