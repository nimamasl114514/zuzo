class Node:
    def __init__(self, val, next=None):
        self.val = val
        self.next = next


class LinkedList:
    def __init__(self):
        self.head = None
        self.size = 0

    def push(self, val):
        self.head = Node(val, self.head)
        self.size += 1

    def to_list(self):
        out = []
        cur = self.head
        while cur:
            out.append(cur.val)
            cur = cur.next
        return out

    def reverse(self):
        prev = None
        cur = self.head
        while cur:
            nxt = cur.next
            cur.next = prev
            prev = cur
            cur = nxt
        self.head = prev

    def __len__(self):
        return self.size


ll = LinkedList()
for i in range(5):
    ll.push(i)
print(ll.to_list(), len(ll))
ll.reverse()
print(ll.to_list())


class Queue:
    def __init__(self):
        self.items = []

    def enqueue(self, x):
        self.items.append(x)

    def dequeue(self):
        return self.items.pop(0)

    def empty(self):
        return len(self.items) == 0


q = Queue()
q.enqueue("a")
q.enqueue("b")
print(q.dequeue(), q.empty(), q.dequeue(), q.empty())


class TreeNode:
    def __init__(self, val):
        self.val = val
        self.left = None
        self.right = None

    def insert(self, val):
        if val < self.val:
            if self.left:
                self.left.insert(val)
            else:
                self.left = TreeNode(val)
        else:
            if self.right:
                self.right.insert(val)
            else:
                self.right = TreeNode(val)

    def inorder(self):
        out = []
        if self.left:
            out.extend(self.left.inorder())
        out.append(self.val)
        if self.right:
            out.extend(self.right.inorder())
        return out

    def height(self):
        lh = self.left.height() if self.left else 0
        rh = self.right.height() if self.right else 0
        return 1 + max(lh, rh)


root = TreeNode(10)
for v in [5, 15, 3, 7, 12, 20]:
    root.insert(v)
print(root.inorder())
print("height:", root.height())


class Graph:
    def __init__(self):
        self.adj = {}

    def add_edge(self, a, b):
        self.adj.setdefault(a, []).append(b)
        self.adj.setdefault(b, []).append(a)

    def bfs(self, start):
        seen = {start}
        order = []
        queue = [start]
        while queue:
            node = queue.pop(0)
            order.append(node)
            for nxt in self.adj.get(node, []):
                if nxt not in seen:
                    seen.add(nxt)
                    queue.append(nxt)
        return order

    def dfs(self, start, seen=None):
        if seen is None:
            seen = set()
        seen.add(start)
        order = [start]
        for nxt in self.adj.get(start, []):
            if nxt not in seen:
                order.extend(self.dfs(nxt, seen))
        return order


g = Graph()
for a, b in [(1, 2), (1, 3), (2, 4), (3, 4), (4, 5)]:
    g.add_edge(a, b)
print(g.bfs(1))
print(g.dfs(1))


class Matrix:
    def __init__(self, rows):
        self.rows = rows

    def __matmul__(self, other):
        n = len(self.rows)
        m = len(other.rows[0])
        k = len(other.rows)
        out = [[0] * m for _ in range(n)]
        for i in range(n):
            for j in range(m):
                for x in range(k):
                    out[i][j] += self.rows[i][x] * other.rows[x][j]
        return Matrix(out)

    def __repr__(self):
        return "Matrix(" + repr(self.rows) + ")"


a = Matrix([[1, 2], [3, 4]])
b = Matrix([[5, 6], [7, 8]])
print(a @ b)


class Temperature:
    _instances = []

    def __init__(self, c):
        self.c = c
        Temperature._instances.append(self)

    @classmethod
    def count(cls):
        return len(cls._instances)

    @classmethod
    def average(cls):
        return sum(t.c for t in cls._instances) / len(cls._instances)


Temperature(10)
Temperature(20)
Temperature(30)
print(Temperature.count(), Temperature.average())


class Base2:
    def __init__(self):
        self.trace = []

    def run(self):
        self.trace.append("base")
        return self.trace


class Derived(Base2):
    def run(self):
        super().run()
        self.trace.append("derived")
        return self.trace


print(Derived().run())
print([c.__name__ for c in Derived.__mro__])
