-- RAG staged-generation publication state. The migration only adds state and
-- a task generation column; existing vectors remain legacy generation 0 until
-- a document is successfully reindexed and published.
CREATE TABLE IF NOT EXISTS ws_document_index_state (
    tenant_id           VARCHAR(64)  NOT NULL,
    doc_id              VARCHAR(64)  NOT NULL,
    next_generation     BIGINT UNSIGNED NOT NULL DEFAULT 0,
    desired_generation  BIGINT UNSIGNED NOT NULL DEFAULT 0,
    active_generation   BIGINT UNSIGNED NOT NULL DEFAULT 0,
    active_task_id      VARCHAR(64)  NOT NULL DEFAULT '',
    legacy_allowed      TINYINT(1)   NOT NULL DEFAULT 1,
    published_at        DATETIME,
    created_at          DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, doc_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- MySQL 8 deployments differ in support for ALTER ... IF NOT EXISTS. Use
-- information_schema-driven dynamic DDL so operators can safely re-run this
-- migration after an interrupted deployment.
SET @ws_generation_column_exists := (
    SELECT COUNT(*)
    FROM information_schema.columns
    WHERE table_schema = DATABASE()
      AND table_name = 'ws_index_task'
      AND column_name = 'generation'
);
SET @ws_generation_column_sql := IF(
    @ws_generation_column_exists = 0,
    'ALTER TABLE ws_index_task ADD COLUMN generation BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER execution_token',
    'SELECT 1'
);
PREPARE ws_generation_column_stmt FROM @ws_generation_column_sql;
EXECUTE ws_generation_column_stmt;
DEALLOCATE PREPARE ws_generation_column_stmt;

SET @ws_generation_index_exists := (
    SELECT COUNT(*)
    FROM information_schema.statistics
    WHERE table_schema = DATABASE()
      AND table_name = 'ws_index_task'
      AND index_name = 'uk_index_generation'
);
SET @ws_generation_index_sql := IF(
    @ws_generation_index_exists = 0,
    'ALTER TABLE ws_index_task ADD UNIQUE INDEX uk_index_generation (tenant_id, doc_id, generation)',
    'SELECT 1'
);
PREPARE ws_generation_index_stmt FROM @ws_generation_index_sql;
EXECUTE ws_generation_index_stmt;
DEALLOCATE PREPARE ws_generation_index_stmt;
