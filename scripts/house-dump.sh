#!/usr/bin/env bash
# house-dump.sh [DIR] - the first dump of the 2026 US House midterms dataset into
# DIR (default: data/house-midterms), which must be empty or absent.
# house-extend.sh brings it up to date afterwards.
#
# DIR ends up holding events, markets, outcomes, history, trades and book,
# each a .parquet combined from the store under store/ (events.parquet,
# markets.parquet, outcomes.parquet and by-market/<dataset>/<id>.parquet),
# which is what the runs extend; errors.log, the exports that failed; and
# CLAUDE.md, which describes it all (copied from house-data.md). See
# midterms-common.sh for what is selected, and for POLYMARKET, DUCKDB and JOBS.

CHAMBER=house
# shellcheck source=scripts/midterms-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/midterms-common.sh"
dump
