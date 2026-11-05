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
      audit the tree afresh (out.tsv only places the upstream tree cache and
      is not written), then for
      the rows under <local-prefix> (a path under pkg/visualtest/testdata),
      skipping local paths containing any <exclude>:
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
      happen before anything is changed, and every path touched is staged.
      Loads that resolve outside both suites are reported as "dangling", and
      missing files at gitignored paths (the corpus fonts) as "ignored".
      Prints one line per action.
  scripts/wpt-audit.py <wpt-sha> <cache.tsv> --orphans
      list local-only files (no-upstream) that nothing loads and that are not
      reftests: references left behind when their tests moved.
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
import http.client
import html.parser
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
TOP_LEVEL = {"common", "dom", "fonts", "resources", "images", "media"}
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


def suite_of(local):
    """The suite doc root (findWPTRoot) a corpus path is under, or None."""
    for root in SUITES:
        if local == root or local.startswith(root + "/"):
            return root
    return None


def suite_root(local):
    root = suite_of(local)
    if root is None:
        raise ValueError("not a corpus path: " + local)
    return root


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


DOC_EXT = (".html", ".htm", ".xht", ".xhtml", ".svg")
TEXT_EXT = DOC_EXT + (".css", ".js", ".xml")
CSS_URL_RE = re.compile(
    r"""url\(\s*["']?([^"')]*?)["']?\s*\)|@import\s+["']([^"']*)["']""", re.I)
# Elements whose href fetches a resource (an <a> href is a navigation; a
# <link> loads only for the rels in LINK_LOAD_RELS).
HREF_LOAD_TAGS = {"use", "image", "feimage", "script", "pattern", "lineargradient",
                  "radialgradient", "filter", "mask", "clippath", "textpath", "tref", "mpath"}
LINK_LOAD_RELS = {"stylesheet", "match", "mismatch", "preload", "modulepreload", "prefetch", "icon"}
# Attributes whose value is one URL to load, on any element.
URL_ATTRS = {"src", "poster", "background"}
# A quoted relative path to a file in script text: the best a scanner can do
# for URLs built in JavaScript.
SCRIPT_URL_RE = re.compile(r"""["']((?:\.{0,2}/)?[\w./-]*\.(?:js|css|png|jpe?g|gif|svg|webp|avif|ico|html?|xht|xhtml|xml|json|woff2?|ttf|otf|webm|mp4|ogg|wav|mp3|vtt))["']""")


class _LoadParser(html.parser.HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.refs, self.match, self._text = [], [], None

    def handle_starttag(self, tag, attrs):
        tag = tag.split(":")[-1]  # svg:image, svg:use
        attrs = [(k.lower(), v) for k, v in attrs if v is not None]
        named = dict(attrs)
        rels = set((named.get("rel") or "").lower().split())
        for k, v in attrs:
            if k in URL_ATTRS or (k == "data" and tag == "object"):
                self.refs.append(v)
            elif k == "srcset":
                self.refs.extend(c.strip().split()[0] for c in v.split(",") if c.strip())
            elif k in ("href", "xlink:href") and (tag in HREF_LOAD_TAGS or (tag == "link" and rels & LINK_LOAD_RELS)):
                self.refs.append(v)
            elif k == "style":
                self.refs.extend(css_loads(v))
        if tag == "link" and rels & {"match", "mismatch"} and named.get("href"):
            self.match.append(named["href"])
        self._text = tag if tag in ("style", "script") else None

    handle_startendtag = handle_starttag

    def handle_endtag(self, tag):
        self._text = None

    def handle_data(self, data):
        if self._text == "style":
            self.refs.extend(css_loads(data))
        elif self._text == "script":
            self.refs.extend(SCRIPT_URL_RE.findall(data))


def css_loads(text):
    """URLs a stylesheet loads: url() and @import."""
    return [(a or b).strip() for a, b in CSS_URL_RE.findall(text) if (a or b).strip()]


def _parse(data):
    p = _LoadParser()
    p.feed(data.decode("utf-8", "replace"))
    p.close()
    return p


def loads(data, ext=".html"):
    """Every URL a document or stylesheet loads: src=, object data=, href= on
    elements that fetch it (link, SVG use/image/...), url() and @import in
    style sheets, <style> and style attributes. Attributes may be unquoted;
    data-* attributes, <a href> and <link rel=author|help> are not loads.
    srcset, poster and background= count; namespaced SVG tags (svg:image)
    count as their local names. Script text, inline or .js, is scanned for
    quoted relative file paths, since a URL built in JavaScript cannot be
    resolved statically."""
    if ext == ".css":
        return css_loads(data.decode("utf-8", "replace"))
    if ext == ".js":
        return SCRIPT_URL_RE.findall(data.decode("utf-8", "replace"))
    return [r.strip() for r in _parse(data).refs if r.strip()]


def match_hrefs(data):
    """hrefs of <link rel=match|mismatch> in a document."""
    return _parse(data).match


def file_loads(local, data):
    """Corpus paths the file at local loads, including ones outside both
    suites (callers report those as dangling)."""
    if not local.endswith(TEXT_EXT):
        return set()
    ext = posixpath.splitext(local)[1]
    return {t for t in (harness_resolve(local, r) for r in loads(data, ext)) if t}


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
    """Tracked corpus files present in the worktree."""
    out = subprocess.check_output(["git", "ls-files", "-z"] + list(SUITES), cwd=REPO)
    return [p for p in out.decode().split("\0") if p and os.path.exists(repo_path(p))]


def blob_hashes(paths):
    out = subprocess.run(["git", "hash-object", "--stdin-paths"], cwd=REPO,
                         input="\n".join(paths) + "\n", capture_output=True, text=True, check=True)
    return out.stdout.split()


def git_blob_sha(data):
    return hashlib.sha1(b"blob %d\0" % len(data) + data).hexdigest()


def audit_rows(upstream):
    """(local, status, upstream path, local blob, upstream blob) per file,
    against the upstream path -> blob map."""
    index = UpstreamIndex(upstream)
    locals_ = local_files()
    if not locals_:
        return []
    return [(local, status, up, lblob, ublob)
            for local, lblob in zip(locals_, blob_hashes(locals_))
            for status, up, ublob in [classify(upstream_path(local), lblob, index)]]


def audit(sha, out_tsv):
    rows = audit_rows(load_upstream(sha, out_tsv))
    write_mapping(sha, out_tsv, rows)
    counts = collections.Counter(r[1] for r in rows)
    for k in sorted(counts):
        print("%-18s %d" % (k, counts[k]))


def write_mapping(sha, out_tsv, rows):
    with open(out_tsv, "w") as f:
        f.write("# wpt %s\n" % sha)
        f.write("local_path\tstatus\tupstream_path\tlocal_blob\tupstream_blob\n")
        for row in rows:
            f.write("\t".join(row) + "\n")


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
        except (urllib.error.URLError, TimeoutError, ConnectionError, http.client.IncompleteRead):
            # TimeoutError covers socket.timeout raised by read().
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


def missing_refs(dirs):
    """Print reftests whose rel=match/mismatch target is missing locally."""
    for local in corpus_text_files(dirs):
        if not local.endswith(DOC_EXT):
            continue
        for ref in match_hrefs(read_bytes(local)):
            target = harness_resolve(local, ref)
            if target and not os.path.exists(repo_path(target)):
                print("%s\t%s" % (local, ref))


_IGNORED = {}


def check_ignored(paths):
    """Look up whether git ignores each path, one git call for those not yet
    cached. Ignored files cannot be committed."""
    todo = [p for p in paths if (REPO, p) not in _IGNORED]
    if todo:
        out = subprocess.run(["git", "check-ignore", "--stdin", "-z"], cwd=REPO,
                             input="\0".join(todo) + "\0", capture_output=True, text=True).stdout
        hits = set(p for p in out.split("\0") if p)
        for p in todo:
            _IGNORED[(REPO, p)] = p in hits


def ignored(path):
    check_ignored([path])
    return _IGNORED[(REPO, path)]


def orphans(upstream):
    """Corpus files with no upstream counterpart (no-upstream) that no file
    loads and that are not reftests themselves: references left behind when
    their tests moved, and other dead local files."""
    rows = audit_rows(upstream)
    loaded = set()
    for local in corpus_text_files(SUITES):
        loaded |= file_loads(local, read_bytes(local))
    out = []
    for local, status, _, _, _ in rows:
        if status != "no-upstream" or local in loaded:
            continue
        if local.endswith(DOC_EXT) and match_hrefs(read_bytes(local)):
            continue  # a louis14-authored reftest (LOU-172), not an orphan
        out.append(local)
    return out


def plan(sha, out_tsv, prefix, excludes):
    """The actions --apply takes, with every download and hash check done.
    Audits the tree itself (so identical rows, which the committed mapping
    leaves out, still seed the dependency walk); out_tsv only names the
    directory the upstream tree cache lives in, and is not written. Returns
    (actions, log): actions are ("write", path, bytes), ("mv", from, to) and
    ("rm", path), in order. Nothing is written to a gitignored path, and no
    relocation writes outside prefix."""
    upstream = load_upstream(sha, out_tsv)
    reviewed = load_reviewed()
    all_rows = audit_rows(upstream)
    rows = [r for r in all_rows if r[0].startswith(prefix) and not excluded(r[0], excludes)]
    planned = {}  # path -> planned bytes, or None once planned away

    def content(path):
        """The bytes path holds once the planned actions run, or None."""
        if path in planned:
            return planned[path]
        return read_bytes(path) if os.path.exists(repo_path(path)) else None

    actions, log, walk = [], [], []

    def write(path, data, up):
        """Plan a write unless git ignores path; reports whether it did."""
        if ignored(path):
            log.append(("ignored", path, up))
            return False
        actions.append(("write", path, data))
        planned[path] = data
        return True

    check_ignored([r[0] for r in rows])

    for local, status, up, _, ublob in rows:
        if status == "identical":
            walk.append((local, content(local)))
        elif status == "differs":
            if ignored(local):
                log.append(("ignored", local, up))
                continue
            data = upstream_bytes(sha, up, ublob, local)
            if data != content(local) and write(local, data, up):
                log.append(("restored", local, up))
            walk.append((local, data))
    # Every corpus file some local file loads, once the restores above are
    # in, stays where it is.
    loaded = set()
    if any(r[1] in ("relocated", "relocated-differs") for r in rows):
        for local in corpus_text_files(SUITES):
            loaded |= file_loads(local, content(local))
    for local, status, up, _, ublob in rows:
        if status in ("identical", "differs"):
            continue
        if status == "relocated-differs" and reviewed.get(local) != up:
            log.append(("unreviewed", local, up))
            continue
        if status not in ("relocated", "relocated-differs"):
            continue
        target = local_path(up, suite_root(local))
        if target is None or excluded(target, excludes):
            log.append(("skipped", local, up))
            continue
        if not target.startswith(prefix):
            log.append(("outside-prefix", local, target))
            continue
        local_bytes = content(local)
        if local_bytes is None:
            continue  # already moved or removed
        if ignored(target):
            log.append(("ignored", target, up))
            continue
        data = upstream_bytes(sha, up, ublob, local)
        there = content(target)
        if local in loaded:
            # Some local file loads it: copy, never move or remove.
            if there != data:
                write(target, data, up)
            log.append(("copied-loaded" if there != data else "kept-loaded", local, target))
        elif there is not None:
            if there != data:
                write(target, data, up)
            actions.append(("rm", local))
            planned[local] = None
            log.append(("removed-duplicate", local, target))
        else:
            actions.append(("mv", local, target))
            planned[local] = None
            planned[target] = local_bytes
            if data != local_bytes:
                write(target, data, up)
            log.append(("moved", local, target))
        walk.append((target, data))
    seen = set()
    while walk:
        local, data = walk.pop()
        targets = sorted(file_loads(local, data))
        check_ignored([t for t in targets if suite_of(t) is not None])
        for target in targets:
            if target in seen:
                continue
            seen.add(target)
            if suite_of(target) is None:
                log.append(("dangling", local, target))
                continue
            if content(target) is not None or excluded(target, excludes):
                continue
            up = upstream_path(target)
            if up not in upstream:
                continue
            if ignored(target):
                # e.g. the corpus fonts, installed out of band.
                log.append(("ignored", target, up))
                continue
            tdata = download(sha, up, upstream[up])
            write(target, tdata, up)
            log.append(("fetched", target, up))
            walk.append((target, tdata))
    return actions, log


def apply(sha, out_tsv, prefix, excludes):
    """Run plan()'s actions and stage every path they touch."""
    actions, log = plan(sha, out_tsv, prefix, excludes)
    written = []
    for action in actions:
        if action[0] == "write":
            os.makedirs(os.path.dirname(repo_path(action[1])), exist_ok=True)
            with open(repo_path(action[1]), "wb") as f:
                f.write(action[2])
            written.append(action[1])
        elif action[0] == "mv":
            os.makedirs(os.path.dirname(repo_path(action[2])), exist_ok=True)
            subprocess.check_call(["git", "mv", action[1], action[2]], cwd=REPO)
        else:
            subprocess.check_call(["git", "rm", "-q", action[1]], cwd=REPO)
    for i in range(0, len(written), 500):
        subprocess.check_call(["git", "add", "--"] + written[i:i + 500], cwd=REPO)
    for entry in log:
        print("\t".join(entry))


if __name__ == "__main__":
    if len(sys.argv) == 4 and sys.argv[3] == "--orphans":
        print("\n".join(orphans(load_upstream(sys.argv[1], sys.argv[2]))))
    elif len(sys.argv) >= 3 and sys.argv[1] == "--missing-refs":
        missing_refs(sys.argv[2:])
    elif len(sys.argv) == 3:
        audit(sys.argv[1], sys.argv[2])
    elif len(sys.argv) >= 5 and sys.argv[3] == "--apply":
        apply(sys.argv[1], sys.argv[2], sys.argv[4], sys.argv[5:])
    else:
        sys.exit(__doc__)
