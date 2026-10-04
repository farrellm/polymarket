#!/usr/bin/env bash
# senate-parquet.sh [DIR] - rebuilds the Parquet files of a dump in DIR
# (default: data/senate-midterms) from the CSVs under DIR/csv, fetching
# nothing, and copies senate-data.md to DIR/CLAUDE.md. senate-dump.sh and
# senate-extend.sh do the same at the end of every run; this is for a run
# whose build failed, or a change to senate-parquet.sql.

# shellcheck source=scripts/senate-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/senate-common.sh"

if [[ ! -f $CSV/markets.csv ]]; then
	say "$CSV holds no dump: run senate-dump.sh first"
	exit 1
fi
cp "$HERE/senate-data.md" "$OUT/CLAUDE.md"
parquet
rows
