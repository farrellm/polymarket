# shellcheck shell=bash
# Shared by senate-dump.sh and senate-extend.sh; not run on its own.
#
# The dataset is the 2026 US Senate midterms on Polymarket: the events
# filed under the senate-midterms tag (the races) and the senate-elections
# tag (control of the chamber, its leaders, and other Senate events), open
# or closed, ending on or after 2026-01-01, less the primaries, the races
# for state legislatures and the French Senate. The markets are those of
# the events chosen.
#
# Every export is made with --extend, which creates a file that is not
# there and merges into one that is: the dump and the extension differ
# only in what they expect to find.
#
# Environment:
#   POLYMARKET  the binary (default: polymarket on the PATH)
#
# A market listed as open that has no book is not counted as a failure.
#   JOBS        markets fetched at once (default: 2). Each process keeps to
#               10 requests a second, and the tightest documented limit is
#               200 per 10 s (the price history), so 2 stays inside it.

set -euo pipefail

HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
PM=${POLYMARKET:-polymarket}
JOBS=${JOBS:-2}
OUT=${1:-data/senate-midterms}
export PM OUT

TAGS=(senate-midterms senate-elections)
SELECT=(
	--all --ends-after 2026-01-01
	--exclude-tag primaries --exclude-tag senate-primary --exclude-title primary
	--exclude-title "State Senate"
	--exclude-title "French Senate"
)

say() { printf '%s\n' "$*" >&2; }

# ids lists the IDs in the first column of an export, which are digits and
# never quoted, so a line of a quoted cell that runs over is not taken for one.
ids() { tail -n +2 "$1" | grep -oE '^[0-9]+,' | tr -d , || true; }

# fetch runs one export of a market into its file under by-market/, and logs
# the error of one that fails rather than stopping the run: a market that has
# not opened has no tokens to ask about.
fetch() {
	local id=$1 dataset=$2
	shift 2
	local file=$OUT/by-market/$dataset/$id.csv err
	if ! err=$("$PM" export "$dataset" --market "$id" --extend -o "$file" "$@" 2>&1 >/dev/null); then
		err=$(tr -s ' \n' ' ' <<<"$err")
		# A market listed as open may not be taking orders yet (a "candidate
		# not listed above" before anyone is), and so have no book.
		[[ $dataset == book && $err == *"not trading"* ]] && return 0
		printf '%s %s %s:%s\n' "$id" "$dataset" "$*" "$err" >>"$OUT/errors.log"
	fi
}

# market fetches everything about one market. The whole history, a point
# every 12 hours, is fetched once; the last week, a point every 5 minutes,
# every time, so a run at least weekly keeps the fine history unbroken.
market() {
	local id=$1 open=$2
	[[ -f $OUT/by-market/history/$id.csv ]] || fetch "$id" history --interval max
	fetch "$id" history --interval 1w
	fetch "$id" trades
	if [[ $open == open ]]; then
		fetch "$id" book
	fi
}
export -f fetch market

# combine rebuilds a dataset's file from the files of its markets: the
# header once, then the rows of each.
combine() {
	local dataset=$1 files
	files=("$OUT"/by-market/"$dataset"/*.csv)
	[[ -e ${files[0]} ]] || return 0
	{
		head -n 1 "${files[0]}"
		for f in "${files[@]}"; do tail -n +2 "$f"; done
	} >"$OUT/$dataset.csv.tmp"
	mv "$OUT/$dataset.csv.tmp" "$OUT/$dataset.csv"
}

run_all() {
	local started=$SECONDS
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	mkdir -p "$OUT"/by-market/{history,trades,book}
	: >"$OUT/errors.log"
	# What the data is, for whoever (or whatever) reads it next.
	cp "$HERE/senate-data.md" "$OUT/CLAUDE.md"

	for tag in "${TAGS[@]}"; do
		say "events under $tag"
		"$PM" export events --tag "$tag" "${SELECT[@]}" --extend -o "$OUT/events.csv"
	done

	local events=()
	while read -r id; do events+=(--event "$id"); done < <(ids "$OUT/events.csv")
	say "markets of $((${#events[@]} / 2)) events"
	"$PM" export markets "${events[@]}" --all --extend -o "$OUT/markets.csv"
	"$PM" export outcomes "${events[@]}" --all --extend -o "$OUT/outcomes.csv"
	"$PM" export markets "${events[@]}" -o "$tmp/open.csv" >/dev/null 2>&1

	ids "$tmp/open.csv" | LC_ALL=C sort >"$tmp/open"
	ids "$OUT/markets.csv" | LC_ALL=C sort >"$tmp/all"
	local total
	total=$(wc -l <"$tmp/all")
	say "history, trades and book of $total markets, $JOBS at a time"
	# shellcheck disable=SC2016 # expanded by the bash that xargs starts
	{
		LC_ALL=C join "$tmp/all" "$tmp/open" | sed 's/$/ open/'
		LC_ALL=C join -v 1 "$tmp/all" "$tmp/open" | sed 's/$/ closed/'
	} | xargs -P "$JOBS" -L 1 bash -c 'market "$0" "$1"'

	say "combining the markets' files"
	for dataset in history trades book; do combine "$dataset"; done

	local failed
	failed=$(wc -l <"$OUT/errors.log")
	for f in events markets outcomes history trades book; do
		[[ -f $OUT/$f.csv ]] && say "$(printf '%-9s %8d lines' "$f.csv" "$(wc -l <"$OUT/$f.csv")")"
	done
	say "done in $(((SECONDS - started) / 60))m $(((SECONDS - started) % 60))s; $failed exports failed (see $OUT/errors.log)"
}
