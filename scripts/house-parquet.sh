#!/usr/bin/env bash
# house-parquet.sh [DIR] - rebuilds the Parquet files of a dump in DIR
# (default: data/house-midterms) from the store under DIR/store, fetching
# nothing, and copies house-data.md to DIR/CLAUDE.md. house-dump.sh and
# house-extend.sh do the same at the end of every run; this is for a run
# whose build failed, or a change to midterms-parquet.sql.

CHAMBER=house
# shellcheck source=scripts/midterms-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/midterms-common.sh"
rebuild
