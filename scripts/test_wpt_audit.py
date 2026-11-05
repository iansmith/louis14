#!/usr/bin/env python3
"""Unit tests for scripts/wpt-audit.py (LOU-694). Run: python3 scripts/test_wpt_audit.py"""

import importlib.util
import os
import unittest

_spec = importlib.util.spec_from_file_location(
    "wpt_audit", os.path.join(os.path.dirname(os.path.abspath(__file__)), "wpt-audit.py"))
wa = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(wa)

C2 = "pkg/visualtest/testdata/wpt-css2"
C3 = "pkg/visualtest/testdata/wpt-css3"


class PathMapping(unittest.TestCase):
    CASES = [
        (C2 + "/floats/float-root.html", "css/CSS2/floats/float-root.html"),
        (C2 + "/reference/ref-filled-green-100px-square.xht", "css/CSS2/reference/ref-filled-green-100px-square.xht"),
        (C2 + "/fonts/ahem.css", "fonts/ahem.css"),
        (C3 + "/css-grid/grid-model/display-inline-grid.html", "css/css-grid/grid-model/display-inline-grid.html"),
        (C3 + "/reference/blank.html", "css/reference/blank.html"),
        (C3 + "/support/parsing-testcommon.js", "css/support/parsing-testcommon.js"),
        (C3 + "/CSS2/reference/ref-filled-green-100px-square.xht", "css/CSS2/reference/ref-filled-green-100px-square.xht"),
        (C3 + "/fonts/ahem.css", "fonts/ahem.css"),
        (C3 + "/resources/testharness.js", "resources/testharness.js"),
    ]

    def test_upstream_path(self):
        for local, up in self.CASES:
            self.assertEqual(wa.upstream_path(local), up, local)

    def test_local_path_inverts_upstream_path(self):
        for local, up in self.CASES:
            self.assertEqual(wa.local_path(up, wa.suite_root(local)), local, up)
            self.assertEqual(wa.upstream_path(wa.local_path(up, wa.suite_root(local))), up, up)

    def test_unrepresentable(self):
        # wpt-css2 serves /fonts/ and /media/ at its root, so css/CSS2/fonts/
        # and css/CSS2/media/ have no place in that suite; css/<section>/
        # outside CSS2 has none either.
        self.assertIsNone(wa.local_path("css/CSS2/fonts/font-001.xht", C2))
        self.assertIsNone(wa.local_path("css/css-grid/x.html", C2))
        self.assertIsNone(wa.local_path("css/fonts/x.css", C3))
        self.assertIsNone(wa.local_path("tools/x.py", C3))


class HarnessResolve(unittest.TestCase):
    """Mirrors wptResourceResolver.filesystemPath and wptMountPoints
    (pkg/visualtest/wpt_filesystem_path.go)."""

    def test_relative(self):
        self.assertEqual(wa.harness_resolve(C3 + "/css-grid/grid-model/t.html", "../reference/r.html"),
                         C3 + "/css-grid/reference/r.html")

    def test_root_relative_css_mount(self):
        self.assertEqual(wa.harness_resolve(C3 + "/css-values/t.html", "/css/CSS2/reference/r.xht"),
                         C3 + "/CSS2/reference/r.xht")
        self.assertEqual(wa.harness_resolve(C3 + "/css-values/t.html", "/css/support/a.js"),
                         C3 + "/support/a.js")

    def test_root_relative_root_mount(self):
        self.assertEqual(wa.harness_resolve(C3 + "/css-fonts/t.html", "/fonts/ahem.css"), C3 + "/fonts/ahem.css")
        self.assertEqual(wa.harness_resolve(C2 + "/margin-padding-clear/t.xht", "/fonts/ahem.css"),
                         C2 + "/fonts/ahem.css")

    def test_query_fragment_dot_segments(self):
        self.assertEqual(wa.harness_resolve(C3 + "/a/t.html", "/css/../fonts/x.css?v=1#f"), C3 + "/fonts/x.css")
        self.assertEqual(wa.harness_resolve(C3 + "/a/t.html", "r.html#x"), C3 + "/a/r.html")

    def test_absolute_urls_are_not_files(self):
        for ref in ("http://example.com/x", "data:text/plain,x", "//host/x", "mailto:a@b", "#top", ""):
            self.assertIsNone(wa.harness_resolve(C3 + "/a/t.html", ref), ref)


class Loads(unittest.TestCase):
    def test_every_load_kind(self):
        data = b"""<link rel="match" href="r.html"><link rel=stylesheet href='s.css'>
<img src="i.png"><script src="/resources/testharness.js"></script>
<style>@import "imp.css"; @import url(imp2.css); div { background: url('bg.png') }</style>"""
        self.assertEqual(set(wa.loads(data)),
                         {"r.html", "s.css", "i.png", "/resources/testharness.js", "imp.css", "imp2.css", "bg.png"})


class Classify(unittest.TestCase):
    def setUp(self):
        self.upstream = {
            "css/css-grid/grid-model/display-inline-grid.html": "aaa",
            "css/css-grid/reference/x-ref.html": "bbb",
            "css/css-position/overlay/green-ref.html": "ccc",
            "css/css-text/word-spacing/word-spacing-001.html": "ddd",
        }
        self.index = wa.UpstreamIndex(self.upstream)

    def test_direct(self):
        self.assertEqual(wa.classify("css/css-grid/reference/x-ref.html", "bbb", self.index)[0], "identical")
        self.assertEqual(wa.classify("css/css-grid/reference/x-ref.html", "zzz", self.index)[0], "differs")

    def test_relocated_within_section(self):
        self.assertEqual(wa.classify("css/css-grid/x-ref.html", "bbb", self.index),
                         ("relocated", "css/css-grid/reference/x-ref.html", "bbb"))

    def test_same_bytes_only_in_another_section_is_ambiguous(self):
        status, up, _ = wa.classify("css/mediaqueries/green.html", "ccc", self.index)
        self.assertEqual(status, "ambiguous")
        self.assertEqual(up, "css/css-position/overlay/green-ref.html")

    def test_same_name_different_bytes(self):
        self.assertEqual(wa.classify("css/css-text/word-spacing-001.html", "eee", self.index),
                         ("relocated-differs", "css/css-text/word-spacing/word-spacing-001.html", "ddd"))
        self.assertEqual(wa.classify("css/css-text/nothing.html", "eee", self.index)[0], "no-upstream")


class Excluded(unittest.TestCase):
    def test_local_path_space(self):
        ex = ["filter-effects", "css-masking", "compositing"]
        self.assertTrue(wa.excluded(C3 + "/css-masking/clip-path/x.html", ex))
        self.assertFalse(wa.excluded(C3 + "/css-grid/x.html", ex))
        # An upstream-only spelling must not be what decides.
        self.assertFalse(wa.excluded(C2 + "/reference/x.xht", ["css/CSS2"]))


if __name__ == "__main__":
    unittest.main()
