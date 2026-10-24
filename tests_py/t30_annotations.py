import typing
from typing import Callable, Dict, List, Literal, Optional, Tuple, TypedDict, Union


class Point(TypedDict):
    x: int
    y: int


def greet(name: str, times: int = 1, suffix: Optional[str] = None) -> str:
    out = " ".join([f"hi {name}"] * times)
    return out + (suffix or "")


def apply(fn: Callable[[int], int], values: List[int]) -> List[int]:
    return [fn(v) for v in values]


def pick(flag: Literal["a", "b"]) -> Union[str, int]:
    return "A" if flag == "a" else 0


def pair() -> Tuple[int, str]:
    return 7, "seven"


def table() -> Dict[str, int]:
    return {"a": 1, "b": 2}


print(greet("bob"))
print(greet("ann", 2, "!"))
print(apply(lambda v: v * v, [1, 2, 3]))
print(pick("a"), pick("b"))
print(pair(), table())

p: Point = {"x": 1, "y": 2}
print(p, sorted(Point.__annotations__.items()))

print(greet.__annotations__)
print(typing.get_type_hints(greet))

alias = List[int]
print(alias, typing.get_origin(alias), typing.get_args(alias))
print(typing.get_args(Optional[str]))
print(typing.get_origin(Union[int, str]))
print(typing.get_origin(Dict[str, int]))

score: int = 10
score = "changed"
print(score, type(score).__name__)

print(isinstance(True, int), isinstance(1, int))
print(typing.get_type_hints(pair))
print(sorted(typing.get_type_hints(apply)))
