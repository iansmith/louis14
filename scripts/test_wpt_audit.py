#!/usr/bin/env python3
"""Unit tests for scripts/wpt-audit.py (LOU-694). Run: python3 scripts/test_wpt_audit.py"""

import importlib.util
import os
import subprocess
import tempfile
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

    def test_wpt_hosts_reduce_to_paths(self):
        # stripWPTHost (pkg/visualtest/wpt_sub.go) with DefaultWPTServerConfig.
        self.assertEqual(wa.harness_resolve(C3 + "/a/t.html", "http://web-platform.test:8000/css/support/x.png"),
                         C3 + "/support/x.png")
        self.assertEqual(wa.harness_resolve(C3 + "/a/t.html", "//www.not-web-platform.test/fonts/ahem.css"),
                         C3 + "/fonts/ahem.css")
        self.assertIsNone(wa.harness_resolve(C3 + "/a/t.html", "http://example.test/css/x.png"))

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


    def test_unquoted_attributes(self):
        data = b"""<link rel=match href=r.html><img src=i.png><link rel=stylesheet href=s.css>"""
        self.assertEqual(set(wa.loads(data)), {"r.html", "i.png", "s.css"})

    def test_not_loads(self):
        data = b"""<a href="other.html">x</a><img data-src="lazy.png"><div data-href="d.html"></div>
<p>href="text.html"</p>"""
        self.assertEqual(wa.loads(data), [])

    def test_svg_and_inline_style(self):
        data = b"""<svg><use href="sprite.svg#a"/><image xlink:href="pic.png"/></svg>
<div style="background: url(inline.png)"></div><object data="obj.svg"></object>"""
        self.assertEqual(set(wa.loads(data)), {"sprite.svg#a", "pic.png", "inline.png", "obj.svg"})

    def test_css_file(self):
        self.assertEqual(set(wa.loads(b"@import 'a.css'; p { background: url(b.png) }", ".css")), {"a.css", "b.png"})

    def test_match_links(self):
        data = b"""<link rel=match href=r.html><link rel="mismatch" href='n.html'><link rel=stylesheet href=s.css>"""
        self.assertEqual(wa.match_hrefs(data), ["r.html", "n.html"])


class SuiteOf(unittest.TestCase):
    def test_outside_both_suites(self):
        target = wa.harness_resolve(C3 + "/css-lists/t.html", "../../images/green-16x16.png")
        self.assertEqual(target, "pkg/visualtest/testdata/images/green-16x16.png")
        self.assertIsNone(wa.suite_of(target))
        self.assertEqual(wa.suite_of(C3 + "/css-lists/t.html"), C3)


class Plan(unittest.TestCase):
    """plan() against a scratch repo, with the upstream tree and downloads
    stubbed."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.repo = self.tmp.name
        subprocess.check_call(["git", "init", "-q", self.repo])
        self.saved = (wa.REPO, wa.load_upstream, wa.download, wa.RELOCATIONS)
        wa.REPO = self.repo
        wa.RELOCATIONS = os.path.join(self.repo, "none.tsv")
        self.upstream = {}
        wa.load_upstream = lambda sha, cache: self.upstream
        wa.download = lambda sha, up, ublob: self.bytes[up]

    def tearDown(self):
        wa.REPO, wa.load_upstream, wa.download, wa.RELOCATIONS = self.saved
        self.tmp.cleanup()

    def put(self, local, data):
        path = os.path.join(self.repo, local)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "wb") as f:
            f.write(data)
        subprocess.check_call(["git", "add", local], cwd=self.repo)

    def set_upstream(self, files):
        self.bytes = files
        self.upstream = {p: wa.git_blob_sha(d) for p, d in files.items()}

    def test_two_copies_of_one_upstream_file(self):
        self.set_upstream({"css/css-a/reference/r.html": b"R"})
        self.put(C3 + "/css-a/r.html", b"R")
        self.put(C3 + "/css-a/sub/r.html", b"R")
        # Same bytes at one upstream path from two local copies: one moves,
        # the other is removed as a duplicate of the planned target.
        actions, log = wa.plan("sha", os.path.join(self.repo, "m.tsv"), C3 + "/", [])
        kinds = sorted(a[0] for a in actions if a[0] in ("mv", "rm"))
        self.assertEqual(kinds, ["mv", "rm"], log)

    def test_existing_target_with_other_bytes(self):
        self.set_upstream({"css/css-a/reference/r-ref.html": b"NEW", "css/css-a/t.html": b"T"})
        self.put(C3 + "/css-a/t.html", b"T")
        self.put(C3 + "/css-a/reference/r-ref.html", b"OLD")
        self.put(C3 + "/css-a/r-ref.html", b"NEW")
        actions, log = wa.plan("sha", os.path.join(self.repo, "m.tsv"), C3 + "/", [])
        self.assertNotIn("mv", [a[0] for a in actions], log)
        self.assertIn(("write", C3 + "/css-a/reference/r-ref.html", b"NEW"), actions)

    def test_dangling_load_is_reported(self):
        self.set_upstream({"css/css-a/t.html": b'<img src="../../images/x.png">'})
        self.put(C3 + "/css-a/t.html", b'<img src="../../images/x.png">')
        actions, log = wa.plan("sha", os.path.join(self.repo, "m.tsv"), C3 + "/", [])
        self.assertIn(("dangling", C3 + "/css-a/t.html", "pkg/visualtest/testdata/images/x.png"), log)

    def test_ignored_targets_are_not_fetched(self):
        # The corpus fonts are gitignored and installed out of band; a fetch
        # there could never be committed.
        with open(os.path.join(self.repo, ".gitignore"), "w") as f:
            f.write("fonts/\n")
        self.set_upstream({"css/css-a/t.html": b'<link rel=stylesheet href="/fonts/x.css">',
                           "fonts/x.css": b"X"})
        self.put(C3 + "/css-a/t.html", b'<link rel=stylesheet href="/fonts/x.css">')
        actions, log = wa.plan("sha", os.path.join(self.repo, "m.tsv"), C3 + "/", [])
        self.assertEqual(actions, [])
        self.assertIn(("ignored", C3 + "/fonts/x.css", "fonts/x.css"), log)

    def test_identical_files_walk_without_a_mapping(self):
        # plan() audits the tree itself, so dependencies of identical files
        # are fetched even though a committed mapping leaves those rows out.
        self.set_upstream({"css/css-a/t.html": b'<link rel=match href=t-ref.html>',
                           "css/css-a/t-ref.html": b"REF"})
        self.put(C3 + "/css-a/t.html", b'<link rel=match href=t-ref.html>')
        actions, log = wa.plan("sha", os.path.join(self.repo, "m.tsv"), C3 + "/", [])
        self.assertIn(("write", C3 + "/css-a/t-ref.html", b"REF"), actions)

    def test_deleted_paths_are_not_hashed(self):
        self.set_upstream({"css/css-a/t.html": b"T"})
        self.put(C3 + "/css-a/t.html", b"T")
        self.put(C3 + "/css-a/gone.html", b"G")
        os.remove(os.path.join(self.repo, C3 + "/css-a/gone.html"))
        rows = wa.audit_rows(self.upstream)
        self.assertEqual([r[0] for r in rows], [C3 + "/css-a/t.html"])


class Download(unittest.TestCase):
    def test_timeout_mid_read_is_retried(self):
        calls = []

        class Resp:
            def __enter__(self):
                return self

            def __exit__(self, *a):
                return False

            def read(self):
                calls.append(1)
                if len(calls) == 1:
                    raise TimeoutError("read timed out")
                return b"X"

        saved = (wa.urllib.request.urlopen, wa.time.sleep)
        wa.urllib.request.urlopen = lambda url, timeout: Resp()
        wa.time.sleep = lambda s: None
        try:
            self.assertEqual(wa.download("sha", "css/x", wa.git_blob_sha(b"X")), b"X")
        finally:
            wa.urllib.request.urlopen, wa.time.sleep = saved


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
