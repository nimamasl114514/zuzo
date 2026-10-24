def area(w, h):
    return w * h


def perimeter(w, h):
    return 2 * (w + h)


PI = 3.14159


class Shape:
    def __init__(self, name):
        self.name = name

    def describe(self):
        return f"Shape:{self.name}"
