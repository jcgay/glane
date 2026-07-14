#!/usr/bin/env bash
# glane read-only guard (PreToolUse/Bash).
# The glane skill is search-only; this backs that at the tool layer by refusing
# any Bash call that runs a mutating or long-running glane subcommand.
# Only `glane search` and `glane tags` get through.

input=$(cat)

if command -v jq >/dev/null 2>&1; then
  cmd=$(printf '%s' "$input" | jq -r '.tool_input.command // empty' 2>/dev/null)
else
  # ponytail: no jq -> scan the raw stdin JSON. Worst case it over-blocks a
  # command that merely mentions the word (safe for a read-only guard).
  cmd=$input
fi

case "$cmd" in
  *glane*) ;;
  *) exit 0 ;;
esac

# Trailing [^a-zA-Z]/$ keeps it a whole word (blocks `sync`, not `synchronize`).
# Portable across BSD (macOS) and GNU grep — no \b.
if printf '%s' "$cmd" | grep -Eq 'glane[[:space:]]+(sync|enrich|summarize|update|import|serve)([^a-zA-Z]|$)'; then
  echo "glane plugin is read-only: only 'glane search' and 'glane tags' are allowed. Refusing a mutating/long-running glane command (sync/enrich/summarize/update/import/serve) — that is the user's own maintenance, not a step in answering." >&2
  exit 2
fi

exit 0
