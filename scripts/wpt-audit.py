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
                     at upstream_path
  relocated-differs  no file at the direct path; exactly one upstream file with
                     the same name in the same upstream section, different bytes
  ambiguous          several upstream candidates by name; upstream_path lists them
  no-upstream        nothing upstream with this name in the section

Usage:
  scripts/wpt-audit.py <wpt-sha> <out.tsv>
      audit: write the mapping.
  scripts/wpt-audit.py <wpt-sha> <out.tsv> --apply <local-prefix> [<exclude>...]
      for the rows of an existing mapping under <local-prefix> (a path under
      pkg/visualtest/testdata), skipping paths containing any <exclude>:
        differs            overwrite with the upstream bytes
        relocated          git mv to the upstream location (or git rm when an
                           identical copy is already there)
        relocated-differs  the same, with the upstream bytes, but only for
                           mappings listed in scripts/wpt-audit-relocations.tsv
                           (a same-named upstream file may be a different test)
      then fetch every relatively referenced file (href=, src=, url()) of the
      written files and of the identical ones under <local-prefix> that
      exists upstream but not locally, transitively.
      Prints one line per action.
  scripts/wpt-audit.py --missing-refs <dir>...
      list reftests whose <link rel=match|mismatch> target is missing.

The committed scripts/wpt-audit-mapping.tsv is this output with the identical
rows dropped.

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
import time
import sys
import urllib.error
import urllib.request

# The repo root: WPT_AUDIT_REPO, else the checkout this script lives in.
REPO = os.environ.get("WPT_AUDIT_REPO") or os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
TESTDATA = "pkg/visualtest/testdata"
GH = shutil.which("gh") or "/opt/homebrew/bin/gh"
# Top-level wpt directories the corpus draws from.
UPSTREAM_DIRS = ["css", "common", "fonts", "resources", "images", "media"]
# wpt-css3 subdirectories that mirror top-level wpt directories, not css/.
CSS3_TOP_LEVEL = {"common", "fonts", "resources", "images", "media"}


def upstream_path(local):
    """Direct upstream path for a local corpus path."""
    rel = local[len(TESTDATA) + 1:]
    suite, _, rest = rel.partition("/")
    if suite == "wpt-css2":
        return "css/CSS2/" + rest
    first = rest.split("/", 1)[0]
    if first in CSS3_TOP_LEVEL:
        return rest
    return "css/" + rest


def section_prefix(up):
    """The upstream directory a relocated file is searched in."""
    parts = up.split("/")
    if parts[0] == "css" and len(parts) > 2:
        return "/".join(parts[:2]) + "/"
    return parts[0] + "/"


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
    out = subprocess.check_output(
        ["git", "ls-files", "-z", TESTDATA + "/wpt-css2", TESTDATA + "/wpt-css3"], cwd=REPO)
    return [p for p in out.decode().split("\0") if p]


def blob_hashes(paths):
    out = subprocess.run(["git", "hash-object", "--stdin-paths"], cwd=REPO,
                         input="\n".join(paths) + "\n", capture_output=True, text=True, check=True)
    return out.stdout.split()


def git_blob_sha(data):
    return hashlib.sha1(b"blob %d\0" % len(data) + data).hexdigest()


def audit(sha, out_tsv):
    upstream = load_upstream(sha, out_tsv)
    by_blob = collections.defaultdict(list)
    by_name = collections.defaultdict(list)
    for path, blob in upstream.items():
        by_blob[blob].append(path)
        by_name[(section_prefix(path), os.path.basename(path))].append(path)

    locals_ = local_files()
    rows = []
    for local, lblob in zip(locals_, blob_hashes(locals_)):
        up = upstream_path(local)
        if up in upstream:
            ublob = upstream[up]
            status = "identical" if ublob == lblob else "differs"
        else:
            same = sorted(by_blob.get(lblob, []))
            sec = section_prefix(up)
            in_sec = [p for p in same if p.startswith(sec)]
            named = sorted(by_name.get((sec, os.path.basename(up)), []))
            if same:
                up = (in_sec or same)[0]
                ublob, status = lblob, "relocated"
            elif len(named) == 1:
                up, ublob, status = named[0], upstream[named[0]], "relocated-differs"
            elif named:
                up, ublob, status = ",".join(named), "", "ambiguous"
            else:
                up, ublob, status = "", "", "no-upstream"
        rows.append((local, status, up, lblob, ublob))

    with open(out_tsv, "w") as f:
        f.write("# wpt %s\n" % sha)
        f.write("local_path\tstatus\tupstream_path\tlocal_blob\tupstream_blob\n")
        for r in rows:
            f.write("\t".join(r) + "\n")
    counts = collections.Counter(r[1] for r in rows)
    for k in sorted(counts):
        print("%-18s %d" % (k, counts[k]))


def local_path(up):
    """Inverse of upstream_path: where an upstream file lives in the corpus."""
    if up.startswith("css/CSS2/"):
        return TESTDATA + "/wpt-css2/" + up[len("css/CSS2/"):]
    if up.startswith("css/"):
        return TESTDATA + "/wpt-css3/" + up[len("css/"):]
    return TESTDATA + "/wpt-css3/" + up


def fetch(sha, up, ublob, local=None):
    """Upstream bytes of up; read from local instead when it already has them."""
    if local and os.path.exists(os.path.join(REPO, local)):
        with open(os.path.join(REPO, local), "rb") as f:
            data = f.read()
        if git_blob_sha(data) == ublob:
            return data
    url = "https://raw.githubusercontent.com/web-platform-tests/wpt/%s/%s" % (sha, up)
    for attempt in range(4):
        try:
            data = urllib.request.urlopen(url, timeout=60).read()
            break
        except urllib.error.HTTPError as e:
            if e.code not in (403, 429) or attempt == 3:
                raise
            time.sleep(2 ** attempt)
    if git_blob_sha(data) != ublob:
        sys.exit("%s: downloaded bytes do not match upstream blob %s" % (url, ublob))
    return data


def write(local, data):
    full = os.path.join(REPO, local)
    os.makedirs(os.path.dirname(full), exist_ok=True)
    with open(full, "wb") as f:
        f.write(data)


REF_RE = re.compile(rb"""(?:href|src)\s*=\s*["']([^"'#?]+)|url\(\s*["']?([^"')#?]+)""", re.I)
TEXT_EXT = (".html", ".htm", ".xht", ".xhtml", ".svg", ".css", ".js", ".xml")


def references(up, data):
    """Upstream paths a file refers to by relative URL."""
    out = set()
    if not up.endswith(TEXT_EXT):
        return out
    base = posixpath.dirname(up)
    for m in REF_RE.finditer(data):
        ref = (m.group(1) or m.group(2)).decode("utf-8", "replace").strip()
        if not ref or ":" in ref or ref.startswith("//"):
            continue
        out.add(posixpath.normpath(ref.lstrip("/") if ref.startswith("/") else posixpath.join(base, ref)))
    return out


RELOCATIONS = os.path.join(REPO, "scripts", "wpt-audit-relocations.tsv")


def load_reviewed():
    """Reviewed relocated-differs mappings: local path -> upstream path."""
    out = {}
    if os.path.exists(RELOCATIONS):
        for line in open(RELOCATIONS):
            if line.strip() and not line.startswith("#"):
                local, up = line.rstrip("\n").split("\t")[:2]
                out[local] = up
    return out


MATCH_LINK_RE = re.compile(rb"""<link\b[^>]*\brel\s*=\s*["']?(?:mis)?match["']?[^>]*>""", re.I)
HREF_RE = re.compile(rb"""href\s*=\s*["']([^"']+)["']""", re.I)


def missing_refs(dirs):
    """Print reftests whose rel=match/mismatch target is missing locally."""
    for root in dirs:
        for d, _, files in os.walk(os.path.join(REPO, root)):
            for name in files:
                if not name.endswith(TEXT_EXT):
                    continue
                path = os.path.join(d, name)
                with open(path, "rb") as f:
                    text = f.read()
                for link in MATCH_LINK_RE.finditer(text):
                    href = HREF_RE.search(link.group(0))
                    ref = href.group(1).decode("utf-8", "replace").split("#")[0] if href else ""
                    if ref and ":" not in ref and not os.path.exists(os.path.normpath(os.path.join(d, ref))):
                        print("%s\t%s" % (os.path.relpath(path, REPO), ref))


def apply(sha, out_tsv, prefix, excludes):
    upstream = load_upstream(sha, out_tsv)
    written = []  # (upstream path, bytes)
    reviewed = load_reviewed()
    for line in open(out_tsv):
        if line.startswith("#") or line.startswith("local_path"):
            continue
        local, status, up, _, ublob = line.rstrip("\n").split("\t")
        if not local.startswith(prefix) or any(e in local for e in excludes):
            continue
        if status == "identical":
            # Unchanged files still seed the reference walk: a reference an
            # upstream test names may never have been imported.
            with open(os.path.join(REPO, local), "rb") as f:
                written.append((up, f.read()))
        elif status == "differs":
            data = fetch(sha, up, ublob, local)
            write(local, data)
            written.append((up, data))
            print("restored\t%s\t%s" % (local, up))
        elif status == "relocated-differs" and reviewed.get(local) != up:
            # A same-named upstream file may be a different test: apply only
            # mappings a person reviewed (wpt-audit-relocations.tsv).
            print("unreviewed\t%s\t%s" % (local, up))
        elif status in ("relocated", "relocated-differs"):
            if any(e in up for e in excludes):
                print("skipped-excluded\t%s\t%s" % (local, up))
                continue
            target = local_path(up)
            data = fetch(sha, up, ublob, target if not os.path.exists(os.path.join(REPO, local)) else local)
            if not os.path.exists(os.path.join(REPO, local)):
                pass  # already moved or removed by an earlier run
            elif os.path.exists(os.path.join(REPO, target)):
                with open(os.path.join(REPO, target), "rb") as f:
                    there = f.read()
                if there != data:
                    write(target, data)
                    print("restored\t%s\t%s" % (target, up))
                subprocess.check_call(["git", "rm", "-q", local], cwd=REPO)
                print("removed-duplicate\t%s\t%s" % (local, up))
            else:
                os.makedirs(os.path.dirname(os.path.join(REPO, target)), exist_ok=True)
                subprocess.check_call(["git", "mv", local, target], cwd=REPO)
                write(target, data)
                print("moved\t%s\t%s" % (local, target))
            written.append((up, data))
    seen = set()
    while written:
        up, data = written.pop()
        for ref in references(up, data):
            if ref in seen or ref not in upstream or any(e in ref for e in excludes):
                continue
            seen.add(ref)
            target = local_path(ref)
            if os.path.exists(os.path.join(REPO, target)):
                continue
            rdata = fetch(sha, ref, upstream[ref])
            write(target, rdata)
            print("fetched\t%s\t%s" % (target, ref))
            written.append((ref, rdata))


if __name__ == "__main__":
    if len(sys.argv) >= 3 and sys.argv[1] == "--missing-refs":
        missing_refs(sys.argv[2:])
    elif len(sys.argv) == 3:
        audit(sys.argv[1], sys.argv[2])
    elif len(sys.argv) >= 5 and sys.argv[3] == "--apply":
        apply(sys.argv[1], sys.argv[2], sys.argv[4], sys.argv[5:])
    else:
        sys.exit(__doc__)
