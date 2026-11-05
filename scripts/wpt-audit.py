#!/usr/bin/env python3
"""Audit the vendored WPT corpus against upstream at a pinned wpt SHA (LOU-694).

For every tracked file under pkg/visualtest/testdata/wpt-css2 and wpt-css3,
map the local path to its upstream path and compare git blob hashes, which is
a byte-for-byte comparison without downloading anything. Writes a TSV mapping
with one row per local file:

    local_path  status  upstream_path  local_blob  upstream_blob

status is one of:
  identical          direct path match, same bytes
  differs            direct path match, different bytes
  relocated          no file at the direct path; the same bytes exist upstream
                     in the same upstream section, at upstream_path
  relocated-differs  no file at the direct path; exactly one upstream file with
                     the same name in the same section, different bytes
  ambiguous          the same bytes exist only in another section, or several
                     same-named files exist in the section; upstream_path lists
                     the candidates. Left alone.
  no-upstream        nothing upstream matches

Paths follow the reftest harness (pkg/visualtest/wpt_filesystem_path.go):
each suite directory (wpt-css2, wpt-css3) is its own doc root, and the mount
table serves both "/css/" and "/" from it. So wpt-css3/X is upstream css/X,
wpt-css2/X is upstream css/CSS2/X, and in either suite a first segment naming
a top-level wpt directory (fonts, common, resources, images, media) is that
top-level directory.

Usage:
  scripts/wpt-audit.py <wpt-sha> <out.tsv>
      audit: write the mapping.
  scripts/wpt-audit.py <wpt-sha> <out.tsv> --apply <local-prefix> [<exclude>...]
      for the rows of an existing mapping under <local-prefix> (a path under
      pkg/visualtest/testdata), skipping local paths containing any <exclude>:
        differs            overwrite with the upstream bytes
        relocated          git mv to the upstream location, or git rm when an
                           identical copy is already there
        relocated-differs  the same, with the upstream bytes, but only for
                           mappings listed in scripts/wpt-audit-relocations.tsv
                           (a same-named upstream file may be a different test)
      A file any local file loads (src=, href=, url(), @import) is never moved
      or removed; it is copied to the upstream location instead. Then every
      file the written and identical files load is fetched when it exists
      upstream but not locally, transitively. All downloads and hash checks
      happen before anything is changed. Prints one line per action.
  scripts/wpt-audit.py --missing-refs <dir>...
      list reftests whose <link rel=match|mismatch> target is missing.

Tests: python3 scripts/test_wpt_audit.py

The committed scripts/wpt-audit-mapping.tsv is the audit output with the
identical rows dropped.

Upstream trees come from `gh api` (cached next to the output as
wpt-tree-<sha>-<dir>.json); restored bytes come from
raw.githubusercontent.com at the pinned SHA, and are checked against the
upstream blob hash before being written.
"""

import collections
import hashlib
import json
import os
import posixpath
import re
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.request

# The repo root: WPT_AUDIT_REPO, else the checkout this script lives in.
REPO = os.environ.get("WPT_AUDIT_REPO") or os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
TESTDATA = "pkg/visualtest/testdata"
SUITES = (TESTDATA + "/wpt-css2", TESTDATA + "/wpt-css3")
GH = shutil.which("gh") or "/opt/homebrew/bin/gh"
# Top-level wpt directories a suite serves at its own root ("/" mount).
TOP_LEVEL = {"common", "fonts", "resources", "images", "media"}
UPSTREAM_DIRS = ["css"] + sorted(TOP_LEVEL)
# Hosts the harness serves locally (stripWPTHost with DefaultWPTServerConfig,
# pkg/visualtest/wpt_sub.go).
WPT_HOSTS = {"web-platform.test", "www.web-platform.test",
             "not-web-platform.test", "www.not-web-platform.test"}
# The harness mount table (wptMountPoints), most specific first: URL prefix
# served from the suite's doc root.
MOUNTS = ("/css/", "/")
RELOCATIONS = os.environ.get("WPT_AUDIT_RELOCATIONS") or os.path.join(REPO, "scripts", "wpt-audit-relocations.tsv")


def repo_path(local):
    return os.path.join(REPO, local)


def read_bytes(local):
    with open(repo_path(local), "rb") as f:
        return f.read()


def suite_root(local):
    """The suite doc root (findWPTRoot) a corpus path is under."""
    for root in SUITES:
        if local == root or local.startswith(root + "/"):
            return root
    raise ValueError("not a corpus path: " + local)


def upstream_path(local):
    """Upstream path of a corpus path."""
    root = suite_root(local)
    rest = local[len(root) + 1:]
    if rest.split("/", 1)[0] in TOP_LEVEL:
        return rest
    return ("css/CSS2/" if root.endswith("wpt-css2") else "css/") + rest


def local_path(up, root):
    """Where upstream path up lives in the suite with doc root root, or None
    when that suite cannot hold it. Inverse of upstream_path."""
    first = up.split("/", 1)[0]
    if first in TOP_LEVEL:
        return root + "/" + up
    prefix = "css/CSS2/" if root.endswith("wpt-css2") else "css/"
    if first != "css" or not up.startswith(prefix):
        return None
    rest = up[len(prefix):]
    if rest.split("/", 1)[0] in TOP_LEVEL:
        return None  # shadowed by the top-level directory of that name
    return root + "/" + rest


def harness_resolve(local_file, ref):
    """The corpus path the harness reads for ref in local_file, or None for a
    URL that names no corpus file. Mirrors wptResourceResolver.filesystemPath:
    query and fragment dropped, a URL on a WPT host reduced to its path
    (stripWPTHost), root-relative paths cleaned and served from the suite doc
    root through MOUNTS, other absolute URLs refused, relative ones joined onto
    the document's directory."""
    ref = re.split(r"[?#]", ref.strip(), maxsplit=1)[0]
    host = re.match(r"^(?:https?:)?//([^/:]*)(?::\d+)?(/.*)?$", ref)
    if host and host.group(1) in WPT_HOSTS:
        ref = host.group(2) or "/"
    if not ref or ref.startswith("//") or re.match(r"^[a-zA-Z][a-zA-Z0-9+.-]*:", ref):
        return None
    if ref.startswith("/"):
        cleaned = posixpath.normpath(ref)
        for mount in MOUNTS:
            if cleaned.startswith(mount):
                return posixpath.join(suite_root(local_file), cleaned[len(mount):])
        return None
    return posixpath.normpath(posixpath.join(posixpath.dirname(local_file), ref))


TEXT_EXT = (".html", ".htm", ".xht", ".xhtml", ".svg", ".css", ".js", ".xml")
LOAD_RE = re.compile(
    rb"""(?:href|src)\s*=\s*["']([^"']*)["']"""
    rb"""|url\(\s*["']?([^"')]*)["']?\s*\)"""
    rb"""|@import\s+["']([^"']*)["']""", re.I)


def loads(data):
    """Every URL a document or stylesheet names: src=, href=, url(), @import."""
    out = []
    for m in LOAD_RE.finditer(data):
        ref = next(g for g in m.groups() if g is not None).decode("utf-8", "replace").strip()
        if ref:
            out.append(ref)
    return out


def file_loads(local, data):
    """Corpus paths the file at local loads."""
    if not local.endswith(TEXT_EXT):
        return set()
    return {t for t in (harness_resolve(local, r) for r in loads(data)) if t}


def excluded(local, excludes):
    return any(e in local for e in excludes)


def section_prefix(up):
    """The upstream directory a relocated file is searched in."""
    parts = up.split("/")
    if parts[0] == "css" and len(parts) > 2:
        return "/".join(parts[:2]) + "/"
    return parts[0] + "/"


class UpstreamIndex:
    """Upstream path -> blob, with lookups by blob and by (section, name)."""

    def __init__(self, blobs):
        self.blobs = blobs
        self.by_blob = collections.defaultdict(list)
        self.by_name = collections.defaultdict(list)
        for path, blob in blobs.items():
            self.by_blob[blob].append(path)
            self.by_name[(section_prefix(path), posixpath.basename(path))].append(path)


def classify(up, lblob, index):
    """(status, upstream path, upstream blob) for a local file whose direct
    upstream path is up and whose blob is lblob."""
    if up in index.blobs:
        ublob = index.blobs[up]
        return ("identical" if ublob == lblob else "differs"), up, ublob
    sec = section_prefix(up)
    same = sorted(index.by_blob.get(lblob, []))
    in_sec = [p for p in same if p.startswith(sec)]
    named = sorted(index.by_name.get((sec, posixpath.basename(up)), []))
    if len(in_sec) == 1:
        return "relocated", in_sec[0], lblob
    if in_sec or same:
        return "ambiguous", ",".join(in_sec or same), ""
    if len(named) == 1:
        return "relocated-differs", named[0], index.blobs[named[0]]
    if named:
        return "ambiguous", ",".join(named), ""
    return "no-upstream", "", ""


def gh_tree(sha, recursive=False):
    url = "repos/web-platform-tests/wpt/git/trees/" + sha + ("?recursive=1" if recursive else "")
    return subprocess.check_output([GH, "api", url])


def load_upstream(sha, out_tsv):
    """Upstream path -> blob sha for UPSTREAM_DIRS, cached beside out_tsv."""
    cache_dir = os.path.dirname(os.path.abspath(out_tsv))
    root = None
    upstream = {}
    for top in UPSTREAM_DIRS:
        cache = os.path.join(cache_dir, "wpt-tree-%s-%s.json" % (sha[:12], top))
        if not os.path.exists(cache):
            if root is None:
                root = json.loads(gh_tree(sha))
            sub = [t["sha"] for t in root["tree"] if t["path"] == top]
            if not sub:
                continue
            data = gh_tree(sub[0], recursive=True)
            if json.loads(data).get("truncated"):
                sys.exit("tree for %s is truncated" % top)
            with open(cache, "wb") as f:
                f.write(data)
        with open(cache) as f:
            tree = json.load(f)
        upstream.update({top + "/" + t["path"]: t["sha"] for t in tree["tree"] if t["type"] == "blob"})
    return upstream


def local_files():
    out = subprocess.check_output(["git", "ls-files", "-z"] + list(SUITES), cwd=REPO)
    return [p for p in out.decode().split("\0") if p]


def blob_hashes(paths):
    out = subprocess.run(["git", "hash-object", "--stdin-paths"], cwd=REPO,
                         input="\n".join(paths) + "\n", capture_output=True, text=True, check=True)
    return out.stdout.split()


def git_blob_sha(data):
    return hashlib.sha1(b"blob %d\0" % len(data) + data).hexdigest()


def audit(sha, out_tsv):
    index = UpstreamIndex(load_upstream(sha, out_tsv))
    locals_ = local_files()
    rows = [(local,) + classify(upstream_path(local), lblob, index) + (lblob,)
            for local, lblob in zip(locals_, blob_hashes(locals_))]
    with open(out_tsv, "w") as f:
        f.write("# wpt %s\n" % sha)
        f.write("local_path\tstatus\tupstream_path\tlocal_blob\tupstream_blob\n")
        for local, status, up, ublob, lblob in rows:
            f.write("\t".join((local, status, up, lblob, ublob)) + "\n")
    counts = collections.Counter(r[1] for r in rows)
    for k in sorted(counts):
        print("%-18s %d" % (k, counts[k]))


def download(sha, up, ublob):
    url = "https://raw.githubusercontent.com/web-platform-tests/wpt/%s/%s" % (sha, up)
    for attempt in range(4):
        try:
            with urllib.request.urlopen(url, timeout=60) as r:
                data = r.read()
            break
        except urllib.error.HTTPError as e:
            if e.code not in (403, 429) or attempt == 3:
                raise
        except urllib.error.URLError:
            if attempt == 3:
                raise
        time.sleep(2 ** attempt)
    if git_blob_sha(data) != ublob:
        sys.exit("%s: downloaded bytes do not match upstream blob %s" % (url, ublob))
    return data


def upstream_bytes(sha, up, ublob, have=None):
    """Upstream bytes of up; taken from the local file have when it already
    holds them, downloaded otherwise."""
    if have and os.path.exists(repo_path(have)):
        data = read_bytes(have)
        if git_blob_sha(data) == ublob:
            return data
    return download(sha, up, ublob)


def load_reviewed():
    """Reviewed relocated-differs mappings: local path -> upstream path."""
    out = {}
    if os.path.exists(RELOCATIONS):
        with open(RELOCATIONS) as f:
            for line in f:
                if line.strip() and not line.startswith("#"):
                    local, up = line.rstrip("\n").split("\t")[:2]
                    out[local] = up
    return out


def corpus_text_files(dirs):
    for root in dirs:
        for d, _, files in os.walk(repo_path(root)):
            for name in files:
                if name.endswith(TEXT_EXT):
                    yield os.path.relpath(os.path.join(d, name), REPO)


MATCH_LINK_RE = re.compile(rb"""<link\b[^>]*\brel\s*=\s*["']?(?:mis)?match["']?[^>]*>""", re.I)
HREF_RE = re.compile(rb"""href\s*=\s*["']([^"']+)["']""", re.I)


def missing_refs(dirs):
    """Print reftests whose rel=match/mismatch target is missing locally."""
    for local in corpus_text_files(dirs):
        for link in MATCH_LINK_RE.finditer(read_bytes(local)):
            href = HREF_RE.search(link.group(0))
            ref = href.group(1).decode("utf-8", "replace") if href else ""
            target = harness_resolve(local, ref)
            if target and not os.path.exists(repo_path(target)):
                print("%s\t%s" % (local, ref))


def read_mapping(out_tsv):
    with open(out_tsv) as f:
        for line in f:
            if not (line.startswith("#") or line.startswith("local_path")):
                yield line.rstrip("\n").split("\t")


def plan(sha, out_tsv, prefix, excludes):
    """The actions --apply takes, with every download and hash check done.
    Returns (actions, log): actions are ("write", path, bytes), ("mv", from,
    to) and ("rm", path), in order."""
    upstream = load_upstream(sha, out_tsv)
    reviewed = load_reviewed()
    rows = [r for r in read_mapping(out_tsv)
            if r[0].startswith(prefix) and not excluded(r[0], excludes)]
    exists = {}  # planned existence overrides: path -> bool

    def present(path):
        return exists.get(path, os.path.exists(repo_path(path)))

    actions, log, walk = [], [], []
    restored = {}  # path -> upstream bytes for the differs rows
    for local, status, up, _, ublob in rows:
        if status == "identical":
            walk.append((local, read_bytes(local)))
        elif status == "differs":
            data = upstream_bytes(sha, up, ublob, local)
            restored[local] = data
            if data != read_bytes(local):
                actions.append(("write", local, data))
                log.append(("restored", local, up))
            walk.append((local, data))
    # Every corpus file some local file loads, once the restores above are
    # in, stays where it is.
    loaded = set()
    relocating = any(r[1] in ("relocated", "relocated-differs") for r in rows)
    for local in corpus_text_files(SUITES) if relocating else ():
        loaded |= file_loads(local, restored.get(local) or read_bytes(local))
    for local, status, up, _, ublob in rows:
        if status in ("identical", "differs"):
            continue
        if status == "relocated-differs" and reviewed.get(local) != up:
            log.append(("unreviewed", local, up))
        elif status in ("relocated", "relocated-differs"):
            target = local_path(up, suite_root(local))
            if target is None or excluded(target, excludes):
                log.append(("skipped", local, up))
                continue
            if not present(local):
                continue  # already moved or removed by an earlier run
            data = upstream_bytes(sha, up, ublob, local)
            target_has = present(target) and read_bytes(target) == data
            if local in loaded:
                # Some local file loads it: copy, never move or remove.
                if not target_has:
                    actions.append(("write", target, data))
                log.append(("copied-loaded" if not target_has else "kept-loaded", local, target))
            elif target_has:
                actions.append(("rm", local))
                log.append(("removed-duplicate", local, target))
                exists[local] = False
            else:
                actions.append(("mv", local, target))
                actions.append(("write", target, data))
                log.append(("moved", local, target))
                exists[local] = False
            exists[target] = True
            walk.append((target, data))
    seen = set()
    while walk:
        local, data = walk.pop()
        for target in sorted(file_loads(local, data)):
            if target in seen or present(target) or excluded(target, excludes):
                continue
            seen.add(target)
            up = upstream_path(target)
            if up not in upstream:
                continue
            tdata = download(sha, up, upstream[up])
            actions.append(("write", target, tdata))
            exists[target] = True
            log.append(("fetched", target, up))
            walk.append((target, tdata))
    return actions, log


def apply(sha, out_tsv, prefix, excludes):
    actions, log = plan(sha, out_tsv, prefix, excludes)
    for action in actions:
        if action[0] == "write":
            os.makedirs(os.path.dirname(repo_path(action[1])), exist_ok=True)
            with open(repo_path(action[1]), "wb") as f:
                f.write(action[2])
        elif action[0] == "mv":
            os.makedirs(os.path.dirname(repo_path(action[2])), exist_ok=True)
            subprocess.check_call(["git", "mv", action[1], action[2]], cwd=REPO)
        else:
            subprocess.check_call(["git", "rm", "-q", action[1]], cwd=REPO)
    for entry in log:
        print("\t".join(entry))


if __name__ == "__main__":
    if len(sys.argv) >= 3 and sys.argv[1] == "--missing-refs":
        missing_refs(sys.argv[2:])
    elif len(sys.argv) == 3:
        audit(sys.argv[1], sys.argv[2])
    elif len(sys.argv) >= 5 and sys.argv[3] == "--apply":
        apply(sys.argv[1], sys.argv[2], sys.argv[4], sys.argv[5:])
    else:
        sys.exit(__doc__)
