import math

print(2 ** 100)
print(2 ** 200 - 1)
print(10 ** 30 + 7)
a = 123456789012345678901234567890
b = 987654321098765432109876543210
print(a + b)
print(b - a)
print(a * b)
print(b // a)
print(b % a)
print(-2 ** 100)
print(abs(-(2 ** 80)))
print(math.factorial(25))
print(len(str(2 ** 1000)))
print(2 ** 100 > 2 ** 99)
print(2 ** 64 == 18446744073709551616)
print(hex(2 ** 100))
print(bin(2 ** 20))
print(oct(2 ** 30))
print(int("123456789012345678901234567890"))
print(int("ff" * 10, 16))
print(f"{2**100:,}")
print(f"{2**100}")
print((2 ** 100) // (2 ** 50))
print(divmod(2 ** 100, 7))
print(sum([2 ** 60, 2 ** 60, 2 ** 60]))
total = 1
for i in range(1, 40):
    total *= i
print(total)
print(total % 1000007)
