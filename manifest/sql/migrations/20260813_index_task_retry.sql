-- Durable, bounded retries for transient knowledge-index dependencies.
SET @ws_index_attempt_count_exists := (
    SELECT COUNT(*) FROM information_schema.columns
    WHERE table_schema = DATABASE() AND table_name = 'ws_index_task' AND column_name = 'attempt_count'
);
SET @ws_index_attempt_count_sql := IF(
    @ws_index_attempt_count_exists = 0,
    'ALTER TABLE ws_index_task ADD COLUMN attempt_count INT NOT NULL DEFAULT 0 AFTER lease_expires_at',
    'SELECT 1'
);
PREPARE ws_index_attempt_count_stmt FROM @ws_index_attempt_count_sql;
EXECUTE ws_index_attempt_count_stmt;
DEALLOCATE PREPARE ws_index_attempt_count_stmt;

SET @ws_index_max_attempts_exists := (
    SELECT COUNT(*) FROM information_schema.columns
    WHERE table_schema = DATABASE() AND table_name = 'ws_index_task' AND column_name = 'max_attempts'
);
SET @ws_index_max_attempts_sql := IF(
    @ws_index_max_attempts_exists = 0,
    'ALTER TABLE ws_index_task ADD COLUMN max_attempts INT NOT NULL DEFAULT 3 AFTER attempt_count',
    'SELECT 1'
);
PREPARE ws_index_max_attempts_stmt FROM @ws_index_max_attempts_sql;
EXECUTE ws_index_max_attempts_stmt;
DEALLOCATE PREPARE ws_index_max_attempts_stmt;

SET @ws_index_next_attempt_exists := (
    SELECT COUNT(*) FROM information_schema.columns
    WHERE table_schema = DATABASE() AND table_name = 'ws_index_task' AND column_name = 'next_attempt_at'
);
SET @ws_index_next_attempt_sql := IF(
    @ws_index_next_attempt_exists = 0,
    'ALTER TABLE ws_index_task ADD COLUMN next_attempt_at DATETIME NULL AFTER max_attempts',
    'SELECT 1'
);
PREPARE ws_index_next_attempt_stmt FROM @ws_index_next_attempt_sql;
EXECUTE ws_index_next_attempt_stmt;
DEALLOCATE PREPARE ws_index_next_attempt_stmt;

SET @ws_index_retry_index_exists := (
    SELECT COUNT(*) FROM information_schema.statistics
    WHERE table_schema = DATABASE() AND table_name = 'ws_index_task' AND index_name = 'idx_index_retry_runnable'
);
SET @ws_index_retry_index_sql := IF(
    @ws_index_retry_index_exists = 0,
    'ALTER TABLE ws_index_task ADD KEY idx_index_retry_runnable (status, next_attempt_at, lease_expires_at, created_at)',
    'SELECT 1'
);
PREPARE ws_index_retry_index_stmt FROM @ws_index_retry_index_sql;
EXECUTE ws_index_retry_index_stmt;
DEALLOCATE PREPARE ws_index_retry_index_stmt;
