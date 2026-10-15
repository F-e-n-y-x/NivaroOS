#!/usr/bin/env bash
# Hand-made dashboard copies from before nivaroos-deploy kept www.prev
# (www.bak-2026092*, ~60 MB each). Rollback only ever uses www.prev.
set -eu
rm -rf "${NV_ROOT:-}"/var/lib/nivaroos/www.bak-*
