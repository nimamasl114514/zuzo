def bubble_sort(arr):
    a = arr[:]
    n = len(a)
    for i in range(n):
        for j in range(n - 1 - i):
            if a[j] > a[j + 1]:
                a[j], a[j + 1] = a[j + 1], a[j]
    return a


def quick_sort(arr):
    if len(arr) <= 1:
        return arr
    pivot = arr[len(arr) // 2]
    left = [x for x in arr if x < pivot]
    mid = [x for x in arr if x == pivot]
    right = [x for x in arr if x > pivot]
    return quick_sort(left) + mid + quick_sort(right)


def merge_sort(arr):
    if len(arr) <= 1:
        return arr
    mid = len(arr) // 2
    l = merge_sort(arr[:mid])
    r = merge_sort(arr[mid:])
    out = []
    i = j = 0
    while i < len(l) and j < len(r):
        if l[i] <= r[j]:
            out.append(l[i])
            i += 1
        else:
            out.append(r[j])
            j += 1
    out.extend(l[i:])
    out.extend(r[j:])
    return out


def binary_search(arr, target):
    lo, hi = 0, len(arr) - 1
    while lo <= hi:
        mid = (lo + hi) // 2
        if arr[mid] == target:
            return mid
        if arr[mid] < target:
            lo = mid + 1
        else:
            hi = mid - 1
    return -1


def fib_dp(n):
    if n < 2:
        return n
    dp = [0] * (n + 1)
    dp[1] = 1
    for i in range(2, n + 1):
        dp[i] = dp[i - 1] + dp[i - 2]
    return dp[n]


def lcs(a, b):
    m, n = len(a), len(b)
    dp = [[0] * (n + 1) for _ in range(m + 1)]
    for i in range(1, m + 1):
        for j in range(1, n + 1):
            if a[i - 1] == b[j - 1]:
                dp[i][j] = dp[i - 1][j - 1] + 1
            else:
                dp[i][j] = max(dp[i - 1][j], dp[i][j - 1])
    return dp[m][n]


def is_prime(n):
    if n < 2:
        return False
    i = 2
    while i * i <= n:
        if n % i == 0:
            return False
        i += 1
    return True


def gcd(a, b):
    while b:
        a, b = b, a % b
    return a


data = [5, 2, 9, 1, 5, 6]
print(bubble_sort(data))
print(quick_sort(data))
print(merge_sort(data))
print(binary_search(sorted(data), 6), binary_search(sorted(data), 7))
print(fib_dp(30))
print(lcs("AGGTAB", "GXTXAYB"))
print([x for x in range(30) if is_prime(x)])
print(gcd(48, 18))

matrix = [[1, 2, 3], [4, 5, 6], [7, 8, 9]]
transposed = [[row[i] for row in matrix] for i in range(3)]
print(transposed)
flat = [x for row in matrix for x in row]
print(flat, sum(flat))

words = ["apple", "banana", "cherry"]
print({w: len(w) for w in words})
print(sorted(words, key=len, reverse=True))
print(max(words, key=len))

counts = {}
for ch in "abracadabra":
    counts[ch] = counts.get(ch, 0) + 1
print(sorted(counts.items(), key=lambda kv: (-kv[1], kv[0])))

s = "the quick brown fox jumps over the lazy dog the end"
freq = {}
for w in s.split():
    freq[w] = freq.get(w, 0) + 1
top = sorted(freq.items(), key=lambda kv: (-kv[1], kv[0]))[:3]
print(top)

print(list(reversed(range(5))))
print(sum(x for x in range(10) if x % 2 == 0))
