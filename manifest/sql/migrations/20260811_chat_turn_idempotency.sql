-- Durable, tenant-scoped ownership for synchronous chat idempotency keys.
-- CREATE TABLE IF NOT EXISTS is safe after an interrupted deployment; the
-- migration ledger records the exact file checksum separately.
CREATE TABLE IF NOT EXISTS ws_chat_turn (
    id                BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id         VARCHAR(64)  NOT NULL,
    user_id           VARCHAR(64)  NOT NULL,
    session_id        VARCHAR(64)  NOT NULL,
    idempotency_key   VARCHAR(128) NOT NULL,
    request_hash      CHAR(64)     NOT NULL,
    status            VARCHAR(16)  NOT NULL DEFAULT 'running',
    execution_token   VARCHAR(64)  NOT NULL DEFAULT '',
    response_json     MEDIUMTEXT,
    trace_id          VARCHAR(64)  NOT NULL DEFAULT '',
    error_class       VARCHAR(64)  NOT NULL DEFAULT '',
    created_at        DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at       DATETIME,
    updated_at        DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_chat_turn (tenant_id, user_id, session_id, idempotency_key),
    KEY idx_chat_turn_status (status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
