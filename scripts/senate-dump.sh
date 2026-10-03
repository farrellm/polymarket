#!/usr/bin/env bash
# senate-dump.sh [DIR] - the first dump of the 2026 US Senate midterms
# dataset into DIR (default: data/senate-midterms), which must be empty or
# absent. senate-extend.sh brings it up to date afterwards.
#
# DIR ends up holding events.csv, markets.csv and outcomes.csv; history.csv,
# trades.csv and book.csv, combined from by-market/<dataset>/<id>.csv; and
# errors.log, the exports that failed; and CLAUDE.md, which describes it all
# (copied from senate-data.md). See senate-common.sh for what is
# selected, and for POLYMARKET and JOBS.

# shellcheck source=scripts/senate-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/senate-common.sh"

if [[ -d $OUT && -n $(ls -A "$OUT") ]]; then
	say "$OUT is not empty: extend it with senate-extend.sh, or name another directory"
	exit 1
fi
run_all
