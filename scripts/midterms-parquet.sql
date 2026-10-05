-- midterms-parquet.sql - builds the Parquet files of a midterms dataset (Senate
-- or House) from the store that the exports extend under store/. Run by parquet() in
-- midterms-common.sh with the dataset's directory as the working directory;
-- each file is written as <dataset>.parquet.tmp, which parquet() moves into
-- place once all six are written.
--
-- The store is Parquet already, typed by the exports (internal/export), so
-- this only puts the files of the markets together and sorts them.
--
-- Time always runs forward: within a market (and outcome), rows are oldest
-- first, whatever order the store holds them in (the trades' are newest first).

SET TimeZone = 'UTC';
-- The House's history (21.6M rows on 2026-10-05) took 8 GB to sort, and the
-- OOM killer ended the run: kept to 2 GB, the sort spills to temp_directory,
-- which parquet() puts on the dataset's disk.
SET memory_limit = '2GB';
SET preserve_insertion_order = false;

COPY (FROM 'store/events.parquet') TO 'events.parquet.tmp' (FORMAT parquet, COMPRESSION zstd);

COPY (FROM 'store/markets.parquet') TO 'markets.parquet.tmp' (FORMAT parquet, COMPRESSION zstd);

COPY (FROM 'store/outcomes.parquet') TO 'outcomes.parquet.tmp' (FORMAT parquet, COMPRESSION zstd);

COPY (
	FROM read_parquet('store/by-market/history/*.parquet')
	ORDER BY market_id, outcome_index, timestamp, resolution_seconds
) TO 'history.parquet.tmp' (FORMAT parquet, COMPRESSION zstd);

COPY (
	FROM read_parquet('store/by-market/trades/*.parquet')
	-- the fills of one moment by transaction, for an order that is the same
	-- from one build to the next
	ORDER BY condition_id, timestamp, transaction_hash, outcome_index, side, price, size
) TO 'trades.parquet.tmp' (FORMAT parquet, COMPRESSION zstd);

COPY (
	FROM read_parquet('store/by-market/book/*.parquet')
	-- side DESC: an outcome's bids before its asks, as in the export
	ORDER BY market_id, timestamp, outcome_index, side DESC, level
) TO 'book.parquet.tmp' (FORMAT parquet, COMPRESSION zstd);
