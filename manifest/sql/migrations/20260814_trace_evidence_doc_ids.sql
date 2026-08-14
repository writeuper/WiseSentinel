-- Preserve only bounded document identifiers as structured Trace evidence.
-- Arbitrary prompts, document bodies and tool payloads remain suppressed.
-- MySQL versions used by the platform do not accept `ADD COLUMN IF NOT
-- EXISTS`. Use a metadata guard so a database initialized from the newer
-- schema.sql and an older database upgraded in place are both safe.
SET @ws_trace_evidence_column_exists := (
    SELECT COUNT(*)
    FROM information_schema.columns
    WHERE table_schema = DATABASE()
      AND table_name = 'ws_agent_trace_step'
      AND column_name = 'evidence_doc_ids'
);
SET @ws_trace_evidence_sql := IF(
    @ws_trace_evidence_column_exists = 0,
    'ALTER TABLE ws_agent_trace_step ADD COLUMN evidence_doc_ids JSON COMMENT ''bounded authorized document IDs for Citation grounding'' AFTER output_summary',
    'SELECT 1'
);
PREPARE ws_trace_evidence_stmt FROM @ws_trace_evidence_sql;
EXECUTE ws_trace_evidence_stmt;
DEALLOCATE PREPARE ws_trace_evidence_stmt;
