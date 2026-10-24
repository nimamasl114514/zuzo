import os
from pathlib import Path
import logging
import argparse
import unittest

base = Path("tests_py/_tmp/demo")
base.mkdir(exist_ok=True)
f = base / "note.txt"
f.write_text("hello pathlib\n")
print(f.exists(), f.is_file(), f.is_dir())
print(f.name, f.stem, f.suffix)
print(f.read_text().strip())
print(len(list(base.iterdir())))
print(base.parent.name)
print(str(f).endswith("note.txt"))

logging.basicConfig(level=logging.INFO)
log = logging.getLogger("app")
log.info("started")
log.warning("careful")
log.error("boom")

p = argparse.ArgumentParser(description="demo")
p.add_argument("name")
p.add_argument("--count", type=int, default=1)
p.add_argument("--verbose", action="store_true")
ns = p.parse_args(["alice", "--count", "3", "--verbose"])
print(ns.name, ns.count, ns.verbose)

p2 = argparse.ArgumentParser()
p2.add_argument("--mode", default="fast")
ns2 = p2.parse_args([])
print(ns2.mode)


class MathTest(unittest.TestCase):
    def runTest(self):
        pass


t = MathTest()
t.assertEqual(1 + 1, 2)
t.assertTrue(True)
t.assertFalse(False)
t.assertIn("a", "abc")
t.assertIsNone(None)
t.assertAlmostEqual(0.1 + 0.2, 0.3)
print("unittest asserts ok")
