-- P1: fence index task ownership. Idempotent for existing databases and for
-- fresh schema.sql installations that already contain these fields.
SET @ws_index_token_column_exists := (
    SELECT COUNT(*) FROM information_schema.columns
    WHERE table_schema = DATABASE() AND table_name = 'ws_index_task' AND column_name = 'execution_token'
);
SET @ws_index_token_column_sql := IF(
    @ws_index_token_column_exists = 0,
    'ALTER TABLE ws_index_task ADD COLUMN execution_token VARCHAR(64) NOT NULL DEFAULT '''' AFTER finished_at',
    'SELECT 1'
);
PREPARE ws_index_token_column_stmt FROM @ws_index_token_column_sql;
EXECUTE ws_index_token_column_stmt;
DEALLOCATE PREPARE ws_index_token_column_stmt;

SET @ws_index_running_index_exists := (
    SELECT COUNT(*) FROM information_schema.statistics
    WHERE table_schema = DATABASE() AND table_name = 'ws_index_task' AND index_name = 'idx_index_running'
);
SET @ws_index_running_index_sql := IF(
    @ws_index_running_index_exists = 0,
    'ALTER TABLE ws_index_task ADD KEY idx_index_running (status, started_at, created_at)',
    'SELECT 1'
);
PREPARE ws_index_running_index_stmt FROM @ws_index_running_index_sql;
EXECUTE ws_index_running_index_stmt;
DEALLOCATE PREPARE ws_index_running_index_stmt;
