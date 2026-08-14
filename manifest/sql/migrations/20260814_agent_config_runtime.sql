-- Persist the immutable Agent configuration version used by each Trace and
-- async Ops task. This makes activation changes auditable and reproducible.
SET @ws_trace_config_column_exists := (
    SELECT COUNT(*) FROM information_schema.columns
    WHERE table_schema = DATABASE() AND table_name = 'ws_agent_trace'
      AND column_name = 'config_version'
);
SET @ws_trace_config_sql := IF(
    @ws_trace_config_column_exists = 0,
    'ALTER TABLE ws_agent_trace ADD COLUMN config_version VARCHAR(64) NOT NULL DEFAULT '''' COMMENT ''tenant Agent config snapshot used by this trace'' AFTER agent_type',
    'SELECT 1'
);
PREPARE ws_trace_config_stmt FROM @ws_trace_config_sql;
EXECUTE ws_trace_config_stmt;
DEALLOCATE PREPARE ws_trace_config_stmt;

SET @ws_ops_config_column_exists := (
    SELECT COUNT(*) FROM information_schema.columns
    WHERE table_schema = DATABASE() AND table_name = 'ws_ops_task'
      AND column_name = 'config_version'
);
SET @ws_ops_config_sql := IF(
    @ws_ops_config_column_exists = 0,
    'ALTER TABLE ws_ops_task ADD COLUMN config_version VARCHAR(64) NOT NULL DEFAULT '''' COMMENT ''Agent config snapshot selected at task submission'' AFTER trace_id',
    'SELECT 1'
);
PREPARE ws_ops_config_stmt FROM @ws_ops_config_sql;
EXECUTE ws_ops_config_stmt;
DEALLOCATE PREPARE ws_ops_config_stmt;
