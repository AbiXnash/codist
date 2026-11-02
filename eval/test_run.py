"""Tests for the measuring code, which needs no server or model: python3 -m unittest discover eval"""
import json, os, tempfile, unittest

import run


class Stats(unittest.TestCase):
    def test_wilson_matches_the_published_range(self):
        low, high = run.wilson(12, 16)
        self.assertAlmostEqual(low, 0.505, places=2)
        self.assertAlmostEqual(high, 0.898, places=2)

    def test_wilson_of_nothing_says_nothing(self):
        self.assertEqual(run.wilson(0, 0), (0.0, 1.0))

    def test_six_wins_and_no_losses_is_the_smallest_significant_result(self):
        self.assertGreater(run.mcnemar(5, 0), 0.05)
        self.assertLess(run.mcnemar(6, 0), 0.05)

    def test_an_even_split_shows_no_difference(self):
        self.assertEqual(run.mcnemar(0, 0), 1.0)
        self.assertEqual(run.mcnemar(3, 3), 1.0)


class Score(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory()
        path = os.path.join(self.dir.name, run.JAVA)
        os.makedirs(path)
        with open(os.path.join(path, "A.java"), "w", encoding="utf-8") as f:
            f.write("class A {\n  int x = bug();\n}\n")
        self.labels = {"bugs": [{"id": "b", "file": "A.java", "near": "bug()", "match": "race", "kind": "logic"}], "alsoTrue": "unused", "clean": ["C.java"]}

    def tearDown(self):
        self.dir.cleanup()

    def finding(self, title, line=2, path="A.java", severity="low", source="llm"):
        return {"title": title, "line": line, "path": run.JAVA + path, "severity": severity, "source": source}

    def test_a_match_near_the_line_counts(self):
        results, noise, excused, _ = run.score([self.finding("A race")], self.labels, self.dir.name)
        self.assertTrue(results["b"]["found"])
        self.assertEqual(results["b"]["by"], ["llm"])
        self.assertEqual((noise, excused), ([], []))

    def test_a_match_far_from_the_line_does_not(self):
        results, noise, _, _ = run.score([self.finding("A race", line=20)], self.labels, self.dir.name)
        self.assertFalse(results["b"]["found"])
        self.assertEqual(len(noise), 1)

    def test_excused_findings_are_reported_not_hidden(self):
        _, noise, excused, _ = run.score([self.finding("Unused import")], self.labels, self.dir.name)
        self.assertEqual((len(noise), len(excused)), (0, 1))

    def test_findings_in_clean_files_are_false_alarms(self):
        _, _, _, false_alarms = run.score([self.finding("Whatever", path="C.java")], self.labels, self.dir.name)
        self.assertEqual(len(false_alarms), 1)

    def test_a_label_that_no_longer_matches_the_code_stops_the_run(self):
        self.labels["bugs"][0]["near"] = "gone()"
        with self.assertRaises(SystemExit):
            run.score([], self.labels, self.dir.name)


class Results(unittest.TestCase):
    ACTIVITY = [
        {"kind": "review-file", "model": "m", "status": "done", "path": "A.java", "tookMs": 1500, "promptTokens": 10, "replyTokens": 5},
        {"kind": "step", "title": run.DROPPED + "vague advice", "note": "Consider logging", "path": "A.java"},
        {"kind": "step", "title": "Ran the rules"},
    ]

    def test_only_real_model_calls_count(self):
        calls = run.model_calls(self.ACTIVITY)
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0]["tookMs"], 1500)

    def test_drops_keep_their_reason(self):
        self.assertEqual(run.drops(self.ACTIVITY), [{"reason": "vague advice", "title": "Consider logging", "path": "A.java"}])

    def test_a_model_run_with_no_model_calls_is_invalid(self):
        result = {"config": "m", "calls": [], "failedFiles": []}
        self.assertTrue(run.problems(result, rules_only=False))
        self.assertFalse(run.problems(result, rules_only=True))

    def test_failed_calls_do_not_count_as_a_model_run(self):
        result = {"config": "m", "calls": [{"status": "failed"}], "failedFiles": []}
        self.assertTrue(run.problems(result, rules_only=False))

    def test_compare_finds_gains_and_losses(self):
        def write(name, bugs):
            path = os.path.join(d, name)
            with open(path, "w", encoding="utf-8") as f:
                json.dump({"provenance": {"labels": "x"}, "results": [{"config": name, "bugs": bugs}]}, f)
            return path
        with tempfile.TemporaryDirectory() as d:
            a = write("a", {"p": {"found": True}, "q": {"found": False}})
            b = write("b", {"p": {"found": False}, "q": {"found": True}})
            self.assertEqual(run.compare(a, b), (1, 1, 1.0))


if __name__ == "__main__":
    unittest.main()
