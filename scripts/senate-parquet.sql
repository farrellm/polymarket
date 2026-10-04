-- senate-parquet.sql - builds the Parquet files of the Senate midterms dataset
-- from the CSVs that the exports extend under csv/. Run by parquet() in
-- senate-common.sh with the dataset's directory as the working directory;
-- each file is written as <dataset>.parquet.tmp, which parquet() moves into
-- place once all six are written.
--
-- The columns are named with their types, never guessed: DuckDB would read
-- outcome as BOOLEAN (Yes/No) and token_id as DOUBLE. The types follow the
-- kinds of internal/export/dataset.go: Text is VARCHAR (IDs included), Bool
-- BOOLEAN, Time TIMESTAMPTZ, Number DOUBLE but BIGINT for the counts and
-- indexes. An empty cell is NULL. The dialect is named too, as that of Go's
-- encoding/csv: the sniffer takes a quote doubled inside a cell for the end
-- of an unterminated one.

SET TimeZone = 'UTC';

COPY (
	FROM read_csv('csv/events.csv', header = true, auto_detect = false,
		delim = ',', quote = '"', escape = '"', columns = {
		'id': 'VARCHAR', 'slug': 'VARCHAR', 'title': 'VARCHAR',
		'active': 'BOOLEAN', 'closed': 'BOOLEAN', 'neg_risk': 'BOOLEAN',
		'start_date': 'TIMESTAMPTZ', 'end_date': 'TIMESTAMPTZ',
		'markets': 'BIGINT', 'volume': 'DOUBLE', 'volume_24h': 'DOUBLE',
		'volume_1w': 'DOUBLE', 'volume_1m': 'DOUBLE', 'liquidity': 'DOUBLE',
		'open_interest': 'DOUBLE', 'comment_count': 'BIGINT',
		'tags': 'VARCHAR', 'url': 'VARCHAR'})
) TO 'events.parquet.tmp' (FORMAT parquet, COMPRESSION zstd);

COPY (
	FROM read_csv('csv/markets.csv', header = true, auto_detect = false,
		delim = ',', quote = '"', escape = '"', columns = {
		'id': 'VARCHAR', 'slug': 'VARCHAR', 'question': 'VARCHAR',
		'event_id': 'VARCHAR', 'event_slug': 'VARCHAR', 'event_title': 'VARCHAR',
		'condition_id': 'VARCHAR',
		'active': 'BOOLEAN', 'closed': 'BOOLEAN', 'accepting_orders': 'BOOLEAN', 'neg_risk': 'BOOLEAN',
		'start_date': 'TIMESTAMPTZ', 'end_date': 'TIMESTAMPTZ',
		'outcome_1': 'VARCHAR', 'price_1': 'DOUBLE', 'token_id_1': 'VARCHAR',
		'outcome_2': 'VARCHAR', 'price_2': 'DOUBLE', 'token_id_2': 'VARCHAR',
		'best_bid': 'DOUBLE', 'best_ask': 'DOUBLE', 'last_trade_price': 'DOUBLE', 'spread': 'DOUBLE',
		'change_1h': 'DOUBLE', 'change_1d': 'DOUBLE', 'change_1w': 'DOUBLE',
		'volume': 'DOUBLE', 'volume_24h': 'DOUBLE', 'volume_1w': 'DOUBLE', 'volume_1m': 'DOUBLE',
		'liquidity': 'DOUBLE', 'tags': 'VARCHAR', 'url': 'VARCHAR'})
) TO 'markets.parquet.tmp' (FORMAT parquet, COMPRESSION zstd);

COPY (
	FROM read_csv('csv/outcomes.csv', header = true, auto_detect = false,
		delim = ',', quote = '"', escape = '"', columns = {
		'market_id': 'VARCHAR', 'market_slug': 'VARCHAR', 'question': 'VARCHAR',
		'event_id': 'VARCHAR', 'event_slug': 'VARCHAR', 'event_title': 'VARCHAR',
		'condition_id': 'VARCHAR', 'active': 'BOOLEAN', 'closed': 'BOOLEAN',
		'end_date': 'TIMESTAMPTZ', 'outcome_index': 'BIGINT', 'outcome': 'VARCHAR',
		'price': 'DOUBLE', 'token_id': 'VARCHAR'})
) TO 'outcomes.parquet.tmp' (FORMAT parquet, COMPRESSION zstd);

COPY (
	FROM read_csv('csv/by-market/history/*.csv', header = true, auto_detect = false,
		delim = ',', quote = '"', escape = '"', columns = {
		'market_id': 'VARCHAR', 'market_slug': 'VARCHAR', 'question': 'VARCHAR',
		'condition_id': 'VARCHAR', 'outcome_index': 'BIGINT', 'outcome': 'VARCHAR',
		'token_id': 'VARCHAR', 'timestamp': 'TIMESTAMPTZ', 'price': 'DOUBLE',
		'resolution_seconds': 'BIGINT'})
	ORDER BY market_id, outcome_index, timestamp, resolution_seconds
) TO 'history.parquet.tmp' (FORMAT parquet, COMPRESSION zstd);

COPY (
	FROM read_csv('csv/by-market/trades/*.csv', header = true, auto_detect = false,
		delim = ',', quote = '"', escape = '"', columns = {
		'timestamp': 'TIMESTAMPTZ', 'condition_id': 'VARCHAR', 'market_slug': 'VARCHAR',
		'event_slug': 'VARCHAR', 'question': 'VARCHAR', 'side': 'VARCHAR',
		'outcome_index': 'BIGINT', 'outcome': 'VARCHAR', 'token_id': 'VARCHAR',
		'price': 'DOUBLE', 'size': 'DOUBLE', 'proxy_wallet': 'VARCHAR',
		'name': 'VARCHAR', 'pseudonym': 'VARCHAR', 'transaction_hash': 'VARCHAR'})
	ORDER BY condition_id, timestamp DESC
) TO 'trades.parquet.tmp' (FORMAT parquet, COMPRESSION zstd);

COPY (
	FROM read_csv('csv/by-market/book/*.csv', header = true, auto_detect = false,
		delim = ',', quote = '"', escape = '"', columns = {
		'market_id': 'VARCHAR', 'market_slug': 'VARCHAR', 'question': 'VARCHAR',
		'condition_id': 'VARCHAR', 'outcome_index': 'BIGINT', 'outcome': 'VARCHAR',
		'token_id': 'VARCHAR', 'timestamp': 'TIMESTAMPTZ', 'side': 'VARCHAR',
		'level': 'BIGINT', 'price': 'DOUBLE', 'size': 'DOUBLE'})
	-- side DESC: an outcome's bids before its asks, as in the CSV
	ORDER BY market_id, timestamp, outcome_index, side DESC, level
) TO 'book.parquet.tmp' (FORMAT parquet, COMPRESSION zstd);
