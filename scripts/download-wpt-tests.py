#!/usr/bin/env python3
"""
Download all WPT CSS reftests from GitHub into our local testdata directory.
Skips files that already exist. Uses parallel downloads for efficiency.
"""

import os
import re
import sys
import json
import time
import urllib.request
import urllib.error
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
from threading import Lock

BASE_LOCAL = Path("/Users/iansmith/louis14/pkg/visualtest/testdata/wpt-css3")
WPT_RAW = "https://raw.githubusercontent.com/web-platform-tests/wpt/master"
WPT_API = "https://api.github.com/repos/web-platform-tests/wpt/contents"

print_lock = Lock()

def log(msg):
    with print_lock:
        print(msg, flush=True)

def fetch_url(url, retries=3):
    for attempt in range(retries):
        try:
            req = urllib.request.Request(url, headers={"User-Agent": "wpt-downloader/1.0"})
            with urllib.request.urlopen(req, timeout=30) as r:
                return r.read()
        except urllib.error.HTTPError as e:
            if e.code == 404:
                return None
            if e.code == 403 or e.code == 429:
                time.sleep(2 ** attempt)
                continue
            return None
        except Exception:
            time.sleep(1)
    return None

def get_dir_listing(wpt_path):
    """Fetch directory listing from GitHub API."""
    url = f"{WPT_API}/{wpt_path}"
    data = fetch_url(url)
    if not data:
        return []
    try:
        items = json.loads(data)
        if isinstance(items, dict):  # error response
            return []
        return items
    except Exception:
        return []

def is_ref_file(name):
    return (name.endswith("-ref.html") or name.endswith("-ref.xht") or
            name.endswith("-ref.htm") or name.endswith("-ref.xhtml") or
            name.endswith("-notref.html") or name.endswith("-notref.xht"))

def find_ref_href(content_bytes):
    """Extract href from <link rel="match" href="...">"""
    try:
        content = content_bytes.decode("utf-8", errors="replace")
    except Exception:
        return None
    m = re.search(r'<link\s[^>]*rel=["\']match["\'][^>]*href=["\']([^"\']+)["\']', content, re.IGNORECASE)
    if m:
        return m.group(1)
    m = re.search(r'<link\s[^>]*href=["\']([^"\']+)["\'][^>]*rel=["\']match["\']', content, re.IGNORECASE)
    if m:
        return m.group(1)
    return None

def resolve_ref_path(test_wpt_path, ref_href):
    """Resolve relative ref href to absolute WPT path."""
    if ref_href.startswith("http"):
        return None  # external URL, skip
    if ref_href.startswith("/"):
        return ref_href.lstrip("/")
    # Relative path
    base = test_wpt_path.rsplit("/", 1)[0]
    # Handle ../ in ref path
    parts = (base + "/" + ref_href).split("/")
    resolved = []
    for p in parts:
        if p == "..":
            if resolved:
                resolved.pop()
        elif p != ".":
            resolved.append(p)
    return "/".join(resolved)

def download_file(wpt_path, local_path):
    """Download a file from WPT raw GitHub if it doesn't exist locally."""
    if local_path.exists():
        return "exists"
    local_path.parent.mkdir(parents=True, exist_ok=True)
    url = f"{WPT_RAW}/{wpt_path}"
    data = fetch_url(url)
    if data is None:
        return "failed"
    local_path.write_bytes(data)
    return "downloaded"

def process_test_file(item, wpt_dir, local_dir):
    """Process a single test file: download it and its reference."""
    name = item["name"]
    if is_ref_file(name):
        return None
    if not any(name.endswith(ext) for ext in [".html", ".xht", ".htm", ".xhtml"]):
        return None

    wpt_path = f"{wpt_dir}/{name}"
    local_path = local_dir / name

    # Download test file
    if local_path.exists():
        content = local_path.read_bytes()
        test_status = "exists"
    else:
        url = f"{WPT_RAW}/{wpt_path}"
        content = fetch_url(url)
        if content is None:
            return {"name": name, "status": "failed_download"}
        local_path.parent.mkdir(parents=True, exist_ok=True)
        local_path.write_bytes(content)
        test_status = "downloaded"

    # Check if it's a reftest
    ref_href = find_ref_href(content)
    if not ref_href:
        return None  # not a reftest

    # Resolve and download the reference file
    ref_wpt_path = resolve_ref_path(wpt_path, ref_href)
    if ref_wpt_path is None:
        return {"name": name, "status": "skip_external_ref"}

    # Map ref WPT path to local path
    # The ref might be in a different directory (e.g., reference/ subdir)
    ref_name = ref_wpt_path.rsplit("/", 1)[-1]
    ref_subdir = ref_wpt_path[len(wpt_dir)+1:].rsplit("/", 1)[0] if "/" in ref_wpt_path[len(wpt_dir)+1:] else ""

    if ref_wpt_path.startswith(wpt_dir + "/"):
        # Same directory or subdirectory
        ref_local = local_dir / ref_wpt_path[len(wpt_dir)+1:]
    else:
        # Different directory — place in local_dir (flatten)
        ref_local = local_dir / ref_name

    ref_status = download_file(ref_wpt_path, ref_local)

    return {"name": name, "test": test_status, "ref": ref_status}

# WPT directory → local directory mapping
# Format: (wpt_path, local_subdir)
WPT_DIRS = [
    ("css/compositing",        "compositing"),
    ("css/css-align",          "css-align"),
    ("css/css-animations",     "css-animations"),
    ("css/css-backgrounds",    "css-backgrounds"),
    ("css/css-box",            "css-box"),
    ("css/css-cascade",        "css-cascade"),
    ("css/css-color",          "css-color"),
    ("css/css-color-adjust",   "css-color-adjust"),
    ("css/css-conditional",    "css-conditional"),
    ("css/css-contain",        "css-contain"),
    ("css/css-counter-styles", "css-counter-styles"),
    ("css/css-display",        "css-display"),
    ("css/css-env",            "css-env"),
    ("css/css-flexbox",        "css-flexbox"),
    ("css/css-fonts",          "css-fonts"),
    ("css/css-grid",           "css-grid"),
    ("css/css-hyphens",        "css-hyphens"),
    ("css/css-images",         "css-images"),
    ("css/css-inline",         "css-inline"),
    ("css/css-lists",          "css-lists"),
    ("css/css-logical",        "css-logical"),
    ("css/css-masking",        "css-masking"),
    ("css/css-multicol",       "css-multicol"),
    ("css/css-nesting",        "css-nesting"),
    ("css/css-overflow",       "css-overflow"),
    ("css/css-position",       "css-position"),
    ("css/css-pseudo",         "css-pseudo"),
    ("css/css-ruby",           "css-ruby"),
    ("css/css-scrollbars",     "css-scrollbars"),
    ("css/css-sizing",         "css-sizing"),
    ("css/css-tables",         "css-tables"),
    ("css/css-text",           "css-text"),
    ("css/css-text-decor",     "css-text-decor"),
    ("css/css-transforms",     "css-transforms"),
    ("css/css-transitions",    "css-transitions"),
    ("css/css-ui",             "css-ui"),
    ("css/css-values",         "css-values"),
    ("css/css-variables",      "css-variables"),
    ("css/css-will-change",    "css-will-change"),
    ("css/css-writing-modes",  "css-writing-modes"),
    ("css/filter-effects",     "filter-effects"),
    ("css/mediaqueries",       "mediaqueries"),
    ("css/selectors",          "selectors"),
]

def process_directory(wpt_dir, local_subdir):
    local_dir = BASE_LOCAL / local_subdir
    log(f"[{local_subdir}] Fetching directory listing...")

    items = get_dir_listing(wpt_dir)
    if not items:
        log(f"[{local_subdir}] Empty or error")
        return {"dir": local_subdir, "downloaded": 0, "exists": 0, "failed": 0, "not_reftests": 0}

    html_files = [i for i in items if i.get("type") == "file" and
                  any(i["name"].endswith(e) for e in [".html", ".xht", ".htm", ".xhtml"]) and
                  not is_ref_file(i["name"])]

    log(f"[{local_subdir}] {len(html_files)} candidate test files")

    stats = {"downloaded": 0, "exists": 0, "failed": 0, "not_reftests": 0}

    # Process files with limited parallelism to avoid rate limiting
    with ThreadPoolExecutor(max_workers=4) as ex:
        futures = {ex.submit(process_test_file, item, wpt_dir, local_dir): item["name"]
                   for item in html_files}
        for future in as_completed(futures):
            result = future.result()
            if result is None:
                stats["not_reftests"] += 1
            elif result.get("status") == "failed_download":
                stats["failed"] += 1
            elif result.get("status") == "skip_external_ref":
                stats["not_reftests"] += 1
            elif result.get("test") == "downloaded" or result.get("ref") == "downloaded":
                stats["downloaded"] += 1
            elif result.get("test") == "exists":
                stats["exists"] += 1

    log(f"[{local_subdir}] Done: {stats['downloaded']} new, {stats['exists']} existed, "
        f"{stats['failed']} failed, {stats['not_reftests']} non-reftests")
    return {"dir": local_subdir, **stats}

def main():
    # If specific dirs given as args, only process those
    target_dirs = sys.argv[1:] if len(sys.argv) > 1 else None

    dirs_to_process = [(wpt, local) for wpt, local in WPT_DIRS
                       if target_dirs is None or local in target_dirs]

    log(f"Processing {len(dirs_to_process)} WPT directories...")

    # Process directories sequentially to avoid GitHub rate limits
    # (each dir uses parallel downloads internally)
    total_downloaded = 0
    for wpt_dir, local_subdir in dirs_to_process:
        result = process_directory(wpt_dir, local_subdir)
        total_downloaded += result["downloaded"]
        time.sleep(0.5)  # Be polite to GitHub API

    log(f"\nTotal new files downloaded: {total_downloaded}")

if __name__ == "__main__":
    main()
