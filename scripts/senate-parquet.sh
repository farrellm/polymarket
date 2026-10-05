#!/usr/bin/env bash
# senate-parquet.sh [DIR] - rebuilds the Parquet files of a dump in DIR
# (default: data/senate-midterms) from the store under DIR/store, fetching
# nothing, and copies senate-data.md to DIR/CLAUDE.md. senate-dump.sh and
# senate-extend.sh do the same at the end of every run; this is for a run
# whose build failed, or a change to midterms-parquet.sql.

CHAMBER=senate
# shellcheck source=scripts/midterms-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/midterms-common.sh"
rebuild
