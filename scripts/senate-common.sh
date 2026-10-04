# shellcheck shell=bash
# Shared by senate-dump.sh, senate-extend.sh and senate-parquet.sh; not run
# on its own.
#
# The dataset is the 2026 US Senate midterms on Polymarket: the events
# filed under the senate-midterms tag (the races) and the senate-elections
# tag (control of the chamber, its leaders, and other Senate events), open
# or closed, ending on or after 2026-01-01, less the primaries, the races
# for state legislatures and the French Senate. The markets are those of
# the events chosen.
#
# Every export is made with --extend into a Parquet file under store/, which
# creates a file that is not there and merges into one that is: the dump and
# the extension differ only in what they expect to find. The store is the
# working copy, a file per market for history, trades and book; what is read
# is the six Parquet files combined from it at the end of a run
# (senate-parquet.sql).
#
# Environment:
#   POLYMARKET  the binary (default: polymarket on the PATH)
#   DUCKDB      the DuckDB CLI (default: duckdb on the PATH)
#   JOBS        markets fetched at once (default: 2). Each process keeps to
#               10 requests a second, and the tightest documented limit is
#               200 per 10 s (the price history), so 2 stays inside it.

set -euo pipefail

HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
PM=${POLYMARKET:-polymarket}
DUCKDB=${DUCKDB:-duckdb}
JOBS=${JOBS:-2}
OUT=${1:-data/senate-midterms}
STORE=$OUT/store
DATASETS=(events markets outcomes history trades book)
export PM OUT STORE

TAGS=(senate-midterms senate-elections)
SELECT=(
	--all --ends-after 2026-01-01
	--exclude-tag primaries --exclude-tag senate-primary --exclude-title primary
	--exclude-title "State Senate"
	--exclude-title "French Senate"
)

say() { printf '%s\n' "$*" >&2; }

# ids lists the IDs of the markets or events in a Parquet export.
ids() { "$DUCKDB" -noheader -list -c "SELECT id FROM '$1'"; }

# fetch runs one export of a market into its file under by-market/, and logs
# the error of one that fails rather than stopping the run: a market that has
# not opened has no tokens to ask about.
fetch() {
	local id=$1 dataset=$2
	shift 2
	local file=$STORE/by-market/$dataset/$id.parquet err
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
	[[ -f $STORE/by-market/history/$id.parquet ]] || fetch "$id" history --interval max
	fetch "$id" history --interval 1w
	fetch "$id" trades
	if [[ $open == open ]]; then
		fetch "$id" book
	fi
}
export -f fetch market

# parquet rebuilds the Parquet file of every dataset from the store, each
# written beside its old one and moved into place only once all are written,
# so a failure leaves the old files.
parquet() {
	local scratch ok=0
	# Where DuckDB spills the sort of the history: on the dataset's disk, as
	# /tmp may be memory.
	scratch=$(mktemp -d "$OUT/.duckdb.XXXXXX")
	(cd "$OUT" && "$DUCKDB" -bail -cmd "SET temp_directory = '$(basename "$scratch")'" -f "$HERE/senate-parquet.sql") || ok=$?
	rm -rf "$scratch"
	if ((ok != 0)); then
		rm -f "$OUT"/*.parquet.tmp
		return "$ok"
	fi
	for dataset in "${DATASETS[@]}"; do mv "$OUT/$dataset.parquet.tmp" "$OUT/$dataset.parquet"; done
}

# rows says how many rows each Parquet file holds.
rows() {
	for dataset in "${DATASETS[@]}"; do
		say "$(printf '%-16s %9d rows' "$dataset.parquet" \
			"$("$DUCKDB" -noheader -list -c "SELECT count(*) FROM '$OUT/$dataset.parquet'")")"
	done
}

run_all() {
	local started=$SECONDS
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	mkdir -p "$STORE"/by-market/{history,trades,book}
	: >"$OUT/errors.log"
	# What the data is, for whoever (or whatever) reads it next.
	cp "$HERE/senate-data.md" "$OUT/CLAUDE.md"

	for tag in "${TAGS[@]}"; do
		say "events under $tag"
		"$PM" export events --tag "$tag" "${SELECT[@]}" --extend -o "$STORE/events.parquet"
	done

	local events=()
	while read -r id; do events+=(--event "$id"); done < <(ids "$STORE/events.parquet")
	say "markets of $((${#events[@]} / 2)) events"
	"$PM" export markets "${events[@]}" --all --extend -o "$STORE/markets.parquet"
	"$PM" export outcomes "${events[@]}" --all --extend -o "$STORE/outcomes.parquet"
	"$PM" export markets "${events[@]}" -o "$tmp/open.parquet" >/dev/null 2>&1

	ids "$tmp/open.parquet" | LC_ALL=C sort >"$tmp/open"
	ids "$STORE/markets.parquet" | LC_ALL=C sort >"$tmp/all"
	local total
	total=$(wc -l <"$tmp/all")
	say "history, trades and book of $total markets, $JOBS at a time"
	# shellcheck disable=SC2016 # expanded by the bash that xargs starts
	{
		LC_ALL=C join "$tmp/all" "$tmp/open" | sed 's/$/ open/'
		LC_ALL=C join -v 1 "$tmp/all" "$tmp/open" | sed 's/$/ closed/'
	} | xargs -P "$JOBS" -L 1 bash -c 'market "$0" "$1"'

	say "building the Parquet files"
	parquet

	local failed
	failed=$(wc -l <"$OUT/errors.log")
	rows
	say "done in $(((SECONDS - started) / 60))m $(((SECONDS - started) % 60))s; $failed exports failed (see $OUT/errors.log)"
}
