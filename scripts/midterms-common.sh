# shellcheck shell=bash
# Shared by the scripts of the two 2026 US midterms datasets, senate-*.sh and
# house-*.sh; not run on its own. A script sets CHAMBER (senate or house),
# sources this, and calls dump, extend or rebuild.
#
# A dataset is the events of one chamber's 2026 midterms on Polymarket, open
# or closed, ending on or after 2026-01-01, less the primaries; the markets
# are those of the events chosen. select_events, below, says which:
#
#   senate  the events filed under the senate-midterms tag (the races) and
#           the senate-elections tag (control of the chamber, its leaders,
#           and other Senate events), less the races for state legislatures
#           and the French Senate.
#   house   the events filed under the house-elections tag (the district
#           races), and those under the midterms tag whose title matches
#           "House" (control of the chamber, its seats by state, the
#           Speaker), less the races for state legislatures. No tag holds
#           the control of the House as senate-elections does the Senate's.
#
# Every export is made with --extend into a Parquet file under store/, which
# creates a file that is not there and merges into one that is: the dump and
# the extension differ only in what they expect to find. The store is the
# working copy, a file per market for history, trades and book; what is read
# is the six Parquet files combined from it at the end of a run
# (midterms-parquet.sql).
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
case ${CHAMBER:-} in
senate | house) ;;
*)
	printf 'CHAMBER must be senate or house, not "%s"\n' "${CHAMBER:-}" >&2
	exit 2
	;;
esac
OUT=${1:-data/$CHAMBER-midterms}
STORE=$OUT/store
# What the data is, for whoever (or whatever) reads it next: copied to
# $OUT/CLAUDE.md on every run.
DOC=$HERE/$CHAMBER-data.md
DATASETS=(events markets outcomes history trades book)
export PM OUT STORE

# Every export of the events takes these: open and closed, of this year,
# and no primaries (the tags miss some, so the title too).
SELECT=(
	--all --ends-after 2026-01-01
	--exclude-tag primaries --exclude-title primary
)

# select_events exports the events of the dataset into one file, the merge
# making the union of the exports.
select_events() {
	local events=$STORE/events.parquet
	case $CHAMBER in
	senate)
		local select=("${SELECT[@]}" --exclude-tag senate-primary
			--exclude-title "State Senate" --exclude-title "French Senate")
		local tag
		for tag in senate-midterms senate-elections; do
			say "events under $tag"
			"$PM" export events --tag "$tag" "${select[@]}" --extend -o "$events"
		done
		;;
	house)
		local select=("${SELECT[@]}" --exclude-tag house-primary --exclude-tag house-primaries
			--exclude-title "State House")
		say "events under house-elections"
		"$PM" export events --tag house-elections "${select[@]}" --extend -o "$events"
		say "events under midterms about the House"
		"$PM" export events --tag midterms --search House "${select[@]}" --extend -o "$events"
		;;
	esac
}

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
	(cd "$OUT" && "$DUCKDB" -bail -cmd "SET temp_directory = '$(basename "$scratch")'" -f "$HERE/midterms-parquet.sql") || ok=$?
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
	lock
	: >"$OUT/errors.log"
	cp "$DOC" "$OUT/CLAUDE.md"

	select_events

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

# dump makes the first dump, into an empty or absent directory.
dump() {
	if [[ -d $OUT && -n $(ls -A "$OUT") ]]; then
		say "$OUT is not empty: extend it with $CHAMBER-extend.sh, or name another directory"
		exit 1
	fi
	run_all
}

# extend brings a dump up to date.
extend() {
	need_dump
	run_all
}

# rebuild builds the Parquet files from the store, fetching nothing.
rebuild() {
	need_dump
	lock
	cp "$DOC" "$OUT/CLAUDE.md"
	parquet
	rows
}

# lock waits for any other run on the same directory to end, and holds it
# until this one does: the timer's extension may start while a dump or a run
# by hand is still going, and two runs merging into one store would lose rows.
lock() {
	exec 9>"$OUT/.lock"
	if ! flock -n 9; then
		say "waiting for another run on $OUT to end"
		flock 9
	fi
}

need_dump() {
	if [[ ! -f $STORE/markets.parquet ]]; then
		say "$STORE holds no dump: run $CHAMBER-dump.sh first"
		exit 1
	fi
}
