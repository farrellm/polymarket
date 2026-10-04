#!/usr/bin/env bash
# senate-dump.sh [DIR] - the first dump of the 2026 US Senate midterms
# dataset into DIR (default: data/senate-midterms), which must be empty or
# absent. senate-extend.sh brings it up to date afterwards.
#
# DIR ends up holding events, markets, outcomes, history, trades and book,
# each a .parquet combined from the store under store/ (events.parquet,
# markets.parquet, outcomes.parquet and by-market/<dataset>/<id>.parquet),
# which is what the runs extend; errors.log, the exports that failed; and CLAUDE.md, which describes
# it all (copied from senate-data.md). See senate-common.sh for what is
# selected, and for POLYMARKET, DUCKDB and JOBS.

# shellcheck source=scripts/senate-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/senate-common.sh"

if [[ -d $OUT && -n $(ls -A "$OUT") ]]; then
	say "$OUT is not empty: extend it with senate-extend.sh, or name another directory"
	exit 1
fi
run_all
