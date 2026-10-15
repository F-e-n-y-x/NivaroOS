#!/bin/sh
# Files end-to-end suite (uploads, downloads, copy/move/rename/delete):
# the real core binary on a throwaway port with temp data - never the live
# server. See services/core/e2e/run.sh for options.
exec "$(dirname "$0")/../../services/core/e2e/run.sh" "$@"
