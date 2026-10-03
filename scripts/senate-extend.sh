#!/usr/bin/env bash
# senate-extend.sh [DIR] - brings a dump that senate-dump.sh made in DIR
# (default: data/senate-midterms) up to date. Events and markets are
# refreshed and new ones added; a new market gets the whole of its history,
# a known one its last week; trades are fetched from the newest one held;
# and every open market's book adds a snapshot. Nothing already held is lost.
#
# Run it at least weekly to keep the five-minute history unbroken.

# shellcheck source=scripts/senate-common.sh
source "$(dirname "${BASH_SOURCE[0]}")/senate-common.sh"

if [[ ! -f $OUT/markets.csv ]]; then
	say "$OUT holds no dump: run senate-dump.sh first"
	exit 1
fi
run_all
