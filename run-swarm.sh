#!/bin/bash
# Launches 10 parallel Claude agents, each implementing one CSS feature from
# PLAN-real-web-features.md. Each agent works in its own git worktree to
# avoid conflicts on shared files like pkg/css/style.go.
#
# Usage: ./run-swarm.sh [feature-numbers]
#   ./run-swarm.sh          # run all 10 features
#   ./run-swarm.sh 1 4 10   # run only features 1, 4, and 10
set -e
cd /Users/iansmith/louis14

# Clear CLAUDECODE env var so child agents don't think they're nested
unset CLAUDECODE

FEATURES=(
  ""  # placeholder so index starts at 1
  "Feature 1: CSS Native Nesting (the & selector and implicit nesting). The plan is in /Users/iansmith/louis14/PLAN-real-web-features.md."
  "Feature 2: Dynamic Viewport Units (dvh, svh, lvh, dvw, svw, lvw, dvmin, dvmax, cqw, cqh). The plan is in /Users/iansmith/louis14/PLAN-real-web-features.md."
  "Feature 3: ch, ex, lh, rlh, ic, cap length units. The plan is in /Users/iansmith/louis14/PLAN-real-web-features.md."
  "Feature 4: env() CSS function (safe-area-inset-* and other environment variables). The plan is in /Users/iansmith/louis14/PLAN-real-web-features.md."
  "Feature 5: @media interaction and additional feature queries (pointer, hover, orientation, print type, forced-colors, scripting). The plan is in /Users/iansmith/louis14/PLAN-real-web-features.md."
  "Feature 6: image-set() function for responsive background images. The plan is in /Users/iansmith/louis14/PLAN-real-web-features.md."
  "Feature 7: color() function with named color spaces (srgb, display-p3, a98-rgb, rec2020, xyz-d65). The plan is in /Users/iansmith/louis14/PLAN-real-web-features.md."
  "Feature 8: CSS Grid Subgrid (grid-template-columns: subgrid). The plan is in /Users/iansmith/louis14/PLAN-real-web-features.md."
  "Feature 9: ::marker pseudo-element for list item bullet/number styling. The plan is in /Users/iansmith/louis14/PLAN-real-web-features.md."
  "Feature 10: Vendor-prefixed width values (-webkit-fill-available etc.) and missing pseudo-classes (:any-link, :focus-visible, :focus-within, :target, :placeholder-shown). The plan is in /Users/iansmith/louis14/PLAN-real-web-features.md."
)

# Determine which features to run
if [ $# -gt 0 ]; then
  TARGETS=("$@")
else
  TARGETS=(1 2 3 4 5 6 7 8 9 10)
fi

# Create worktrees for selected features
echo "=== Creating git worktrees ==="
for i in "${TARGETS[@]}"; do
  BRANCH="feature-$i-impl"
  WORKTREE=".worktrees/feature-$i"
  if [ -d "$WORKTREE" ]; then
    echo "  Worktree $WORKTREE already exists, skipping"
  else
    git worktree add "$WORKTREE" -b "$BRANCH"
    echo "  Created $WORKTREE on branch $BRANCH"
  fi
  # Copy fonts: Atkinson Hyperlegible and Liberation are not tracked in git,
  # so worktrees only get Ahem.ttf. Without these fonts the renderer falls
  # back to a system font with different metrics, breaking pixel tests.
  if [ -d "fonts" ] && [ -d "$WORKTREE/fonts" ]; then
    cp fonts/AtkinsonHyperlegible*.ttf "$WORKTREE/fonts/" 2>/dev/null || true
    cp fonts/AtkinsonHyperlegibleMono*.otf "$WORKTREE/fonts/" 2>/dev/null || true
    cp fonts/Liberation*.ttf "$WORKTREE/fonts/" 2>/dev/null || true
    echo "  Copied fonts to $WORKTREE"
  fi
done

mkdir -p /tmp/swarm-logs

# Launch each agent in background
echo ""
echo "=== Launching agents ==="
PIDS=()
for i in "${TARGETS[@]}"; do
  WORKTREE="/Users/iansmith/louis14/.worktrees/feature-$i"
  LOG="/tmp/swarm-logs/feature-$i.log"

  PROMPT="You are implementing ${FEATURES[$i]}
Read /Users/iansmith/louis14/PLAN-real-web-features.md carefully and implement ONLY that feature number.
Follow the implementation steps in the plan exactly. Create all WPT test files described in the plan.
The WPT tests are the primary success criterion: each test must render correctly and PASS.
After implementing, run the full CSS3 test suite to verify no regressions:
  /opt/homebrew/Cellar/go/1.25.5/libexec/bin/go test ./pkg/visualtest/ -run 'TestWPTCSS3Reftests|TestWPTReftests' -count=1 2>&1 | tail -5
MaxDifferentPercent must be 0.1 (never higher). FuzzyRadius must be 0 (never use fuzzy matching).
Do not implement any other features. Do not commit — just make the code changes and leave them staged or unstaged."

  ~/.local/bin/claude --allowedTools "Bash,Read,Write,Edit,Glob,Grep,TodoWrite,Task,WebFetch" \
         -c "$WORKTREE" \
         -p "$PROMPT" \
         > "$LOG" 2>&1 &
  PID=$!
  PIDS+=($PID)
  echo "  Feature $i: pid $PID → $LOG"
done

echo ""
echo "=== All ${#TARGETS[@]} agents launched ==="
echo "Monitor logs:"
echo "  tail -f /tmp/swarm-logs/feature-*.log"
echo "  # or one at a time: tail -f /tmp/swarm-logs/feature-7.log"
echo ""
echo "Waiting for all agents to finish..."

# Wait and report exit codes
FAILED=()
for idx in "${!TARGETS[@]}"; do
  i="${TARGETS[$idx]}"
  PID="${PIDS[$idx]}"
  if wait "$PID"; then
    echo "  Feature $i: DONE (pid $PID)"
  else
    echo "  Feature $i: FAILED (pid $PID) — check /tmp/swarm-logs/feature-$i.log"
    FAILED+=($i)
  fi
done

echo ""
if [ ${#FAILED[@]} -eq 0 ]; then
  echo "=== All agents completed successfully ==="
else
  echo "=== ${#FAILED[@]} agent(s) failed: ${FAILED[*]} ==="
  echo "Check logs in /tmp/swarm-logs/ for details"
  exit 1
fi

echo ""
echo "Next steps — commit and merge worktrees back to current branch:"
echo "  For each feature N, commit in .worktrees/feature-N then:"
echo "  git merge feature-N-impl --no-edit"
echo ""
echo "Or to merge all automatically (after committing in each worktree):"
echo "  for i in \$(seq 1 10); do git merge feature-\$i-impl --no-edit || echo \"Conflict on \$i\"; done"
