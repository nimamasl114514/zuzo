import zuzo
zuzo.设置("x", 5)
print("zuzo x =", zuzo.获取("x"))
print("zuzo 存在 x:", zuzo.存在("x"), "存在 y:", zuzo.存在("y"))
print("zuzo 长度:", zuzo.调用("长度", [1, 2, 3, 4]))
print("zuzo 类型:", zuzo.调用("类型", "abc"))
