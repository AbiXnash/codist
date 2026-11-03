"""Tests for the parts of the real-bug test that need no network: python3 -m unittest discover eval/bench"""
import unittest

import run_bench
import select_bugs

PATCH = """diff --git a/src/A.java b/src/A.java
--- a/src/A.java
+++ b/src/A.java
@@ -10,3 +10,4 @@ class A {
 int a;
-int b = 1;
+int b = 2;
+
 int c;
"""


class Parsing(unittest.TestCase):
    def test_added_lines_skip_blank_lines_and_count_correctly(self):
        self.assertEqual(run_bench.added_lines(PATCH), {"src/A.java": [11]})

    def test_located_uses_the_same_file_and_a_small_distance(self):
        t = {"src/A.java": [11]}
        near = {"path": "src/A.java", "line": 14}
        far = {"path": "src/A.java", "line": 15}
        other = {"path": "src/B.java", "line": 11}
        self.assertEqual(run_bench.located([near, far, other], t), [near])

    def test_files_in_reads_both_sides(self):
        f = select_bugs.files_in(PATCH)[0]
        self.assertEqual((f["path"], f["removed"], f["added"]), ("src/A.java", ["int b = 1;"], ["int b = 2;", ""]))


class Qualifying(unittest.TestCase):
    def test_a_small_source_fix_qualifies(self):
        self.assertEqual(select_bugs.why_not(PATCH, "removed"), "")

    def test_a_fix_that_only_adds_code_does_not(self):
        only_adds = PATCH.replace("-int b = 1;\n", "")
        self.assertIn("only adds", select_bugs.why_not(only_adds, "removed"))

    def test_a_test_file_does_not(self):
        self.assertIn("more than", select_bugs.why_not(PATCH.replace("src/A", "src/test/A"), "removed"))

    def test_a_whole_project_goes_to_one_split(self):
        self.assertEqual(select_bugs.split_of("a/b"), select_bugs.split_of("a/b"))


if __name__ == "__main__":
    unittest.main()
