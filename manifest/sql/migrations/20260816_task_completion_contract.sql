-- Stage 1: persist the immutable task boundary and completion evidence.
SET @ws_ops_contract_column_exists := (
    SELECT COUNT(*) FROM information_schema.columns
    WHERE table_schema = DATABASE() AND table_name = 'ws_ops_task' AND column_name = 'task_contract_json'
);
SET @ws_ops_contract_sql := IF(@ws_ops_contract_column_exists = 0,
    'ALTER TABLE ws_ops_task ADD COLUMN task_contract_json JSON NULL COMMENT ''immutable stage-1 execution boundary'' AFTER config_version', 'SELECT 1');
PREPARE ws_ops_contract_stmt FROM @ws_ops_contract_sql;
EXECUTE ws_ops_contract_stmt;
DEALLOCATE PREPARE ws_ops_contract_stmt;

SET @ws_ops_completion_column_exists := (
    SELECT COUNT(*) FROM information_schema.columns
    WHERE table_schema = DATABASE() AND table_name = 'ws_ops_task' AND column_name = 'completion_json'
);
SET @ws_ops_completion_sql := IF(@ws_ops_completion_column_exists = 0,
    'ALTER TABLE ws_ops_task ADD COLUMN completion_json JSON NULL COMMENT ''completion proposal and runtime decision'' AFTER task_contract_json', 'SELECT 1');
PREPARE ws_ops_completion_stmt FROM @ws_ops_completion_sql;
EXECUTE ws_ops_completion_stmt;
DEALLOCATE PREPARE ws_ops_completion_stmt;
