-- Durable, idempotent vector-GC outbox. CREATE TABLE IF NOT EXISTS is safe to
-- re-run after interrupted deployment and does not alter existing vectors.
CREATE TABLE IF NOT EXISTS ws_rag_vector_gc_task (
    id                BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id         VARCHAR(64)  NOT NULL,
    doc_id            VARCHAR(64)  NOT NULL,
    target_key        VARCHAR(96)  NOT NULL,
    target_kind       VARCHAR(32)  NOT NULL COMMENT 'generation|legacy|document_all',
    target_generation BIGINT UNSIGNED NOT NULL DEFAULT 0,
    reason            VARCHAR(128) NOT NULL DEFAULT '',
    status            VARCHAR(32)  NOT NULL DEFAULT 'pending',
    attempt_count     INT          NOT NULL DEFAULT 0,
    max_attempts      INT          NOT NULL DEFAULT 8,
    next_attempt_at   DATETIME,
    lease_expires_at  DATETIME,
    execution_token   VARCHAR(64)  NOT NULL DEFAULT '',
    last_error        TEXT,
    finished_at       DATETIME,
    created_at        DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at        DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_rag_gc_target (tenant_id, doc_id, target_key),
    KEY idx_rag_gc_runnable (status, next_attempt_at, lease_expires_at, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
