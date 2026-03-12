#!/usr/bin/env bash
set -euo pipefail

printf '[install] scripts/install.sh is no longer used for local development.\n' >&2
printf '[install] use scripts/build.sh to refresh the local binary.\n' >&2
printf '[install] the repository-root install.sh is reserved for release-based remote installation.\n' >&2
exit 1
