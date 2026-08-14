-- Persist the durable approval reference attached to a tool invocation.
-- An empty value is valid for read-only calls; this is only a binding
-- reference, not proof that the referenced approval was approved or that a
-- side effect executed successfully.
SET @ws_tool_approval_column_exists := (
    SELECT COUNT(*)
    FROM information_schema.columns
    WHERE table_schema = DATABASE()
      AND table_name = 'ws_tool_call_record'
      AND column_name = 'approval_id'
);
SET @ws_tool_approval_column_sql := IF(
    @ws_tool_approval_column_exists = 0,
    'ALTER TABLE ws_tool_call_record ADD COLUMN approval_id VARCHAR(64) NOT NULL DEFAULT '''' COMMENT ''durable approval reference; empty for non-approval calls'' AFTER trace_id',
    'SELECT 1'
);
PREPARE ws_tool_approval_column_stmt FROM @ws_tool_approval_column_sql;
EXECUTE ws_tool_approval_column_stmt;
DEALLOCATE PREPARE ws_tool_approval_column_stmt;

SET @ws_tool_approval_index_exists := (
    SELECT COUNT(*)
    FROM information_schema.statistics
    WHERE table_schema = DATABASE()
      AND table_name = 'ws_tool_call_record'
      AND index_name = 'idx_approval'
);
SET @ws_tool_approval_index_sql := IF(
    @ws_tool_approval_index_exists = 0,
    'ALTER TABLE ws_tool_call_record ADD KEY idx_approval (tenant_id, approval_id, created_at)',
    'SELECT 1'
);
PREPARE ws_tool_approval_index_stmt FROM @ws_tool_approval_index_sql;
EXECUTE ws_tool_approval_index_stmt;
DEALLOCATE PREPARE ws_tool_approval_index_stmt;
