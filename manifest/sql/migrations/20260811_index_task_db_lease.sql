-- Explicit DB execution lease for Index Worker. Safe to re-run and forwards
-- stale started_at-only rows into the reclaimable (NULL lease) state.
SET @ws_index_lease_column_exists := (
    SELECT COUNT(*) FROM information_schema.columns
    WHERE table_schema = DATABASE() AND table_name = 'ws_index_task' AND column_name = 'lease_expires_at'
);
SET @ws_index_lease_column_sql := IF(
    @ws_index_lease_column_exists = 0,
    'ALTER TABLE ws_index_task ADD COLUMN lease_expires_at DATETIME NULL AFTER execution_token',
    'SELECT 1'
);
PREPARE ws_index_lease_column_stmt FROM @ws_index_lease_column_sql;
EXECUTE ws_index_lease_column_stmt;
DEALLOCATE PREPARE ws_index_lease_column_stmt;

SET @ws_index_lease_index_exists := (
    SELECT COUNT(*) FROM information_schema.statistics
    WHERE table_schema = DATABASE() AND table_name = 'ws_index_task' AND index_name = 'idx_index_lease_runnable'
);
SET @ws_index_lease_index_sql := IF(
    @ws_index_lease_index_exists = 0,
    'ALTER TABLE ws_index_task ADD KEY idx_index_lease_runnable (status, lease_expires_at, created_at)',
    'SELECT 1'
);
PREPARE ws_index_lease_index_stmt FROM @ws_index_lease_index_sql;
EXECUTE ws_index_lease_index_stmt;
DEALLOCATE PREPARE ws_index_lease_index_stmt;
