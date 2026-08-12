-- P0: fence asynchronous Ops task ownership and schedule retries safely.
-- Each DDL is guarded so deployments can retry after interruption.
SET @ws_ops_token_column_exists := (
    SELECT COUNT(*) FROM information_schema.columns
    WHERE table_schema = DATABASE() AND table_name = 'ws_ops_task' AND column_name = 'execution_token'
);
SET @ws_ops_token_column_sql := IF(
    @ws_ops_token_column_exists = 0,
    'ALTER TABLE ws_ops_task ADD COLUMN execution_token VARCHAR(64) NOT NULL DEFAULT '''' AFTER timeout_at',
    'SELECT 1'
);
PREPARE ws_ops_token_column_stmt FROM @ws_ops_token_column_sql;
EXECUTE ws_ops_token_column_stmt;
DEALLOCATE PREPARE ws_ops_token_column_stmt;

SET @ws_ops_retry_column_exists := (
    SELECT COUNT(*) FROM information_schema.columns
    WHERE table_schema = DATABASE() AND table_name = 'ws_ops_task' AND column_name = 'next_attempt_at'
);
SET @ws_ops_retry_column_sql := IF(
    @ws_ops_retry_column_exists = 0,
    'ALTER TABLE ws_ops_task ADD COLUMN next_attempt_at DATETIME NULL AFTER execution_token',
    'SELECT 1'
);
PREPARE ws_ops_retry_column_stmt FROM @ws_ops_retry_column_sql;
EXECUTE ws_ops_retry_column_stmt;
DEALLOCATE PREPARE ws_ops_retry_column_stmt;

SET @ws_ops_runnable_index_exists := (
    SELECT COUNT(*) FROM information_schema.statistics
    WHERE table_schema = DATABASE() AND table_name = 'ws_ops_task' AND index_name = 'idx_ops_runnable'
);
SET @ws_ops_runnable_index_sql := IF(
    @ws_ops_runnable_index_exists = 0,
    'ALTER TABLE ws_ops_task ADD KEY idx_ops_runnable (status, next_attempt_at, timeout_at, created_at)',
    'SELECT 1'
);
PREPARE ws_ops_runnable_index_stmt FROM @ws_ops_runnable_index_sql;
EXECUTE ws_ops_runnable_index_stmt;
DEALLOCATE PREPARE ws_ops_runnable_index_stmt;
