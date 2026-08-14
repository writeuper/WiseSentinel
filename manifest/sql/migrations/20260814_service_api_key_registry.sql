-- Optional service API Key registry. Secrets are represented only by SHA-256
-- digests; the auth middleware remains opt-in via configuration.
CREATE TABLE IF NOT EXISTS ws_service_api_key (
    id           BIGINT PRIMARY KEY AUTO_INCREMENT,
    key_id       VARCHAR(64) NOT NULL,
    key_hash     CHAR(64) NOT NULL,
    tenant_id    VARCHAR(64) NOT NULL,
    user_id      VARCHAR(64) NOT NULL,
    roles_json   JSON NOT NULL,
    scopes_json  JSON NOT NULL,
    status       VARCHAR(16) NOT NULL DEFAULT 'active',
    expires_at   DATETIME NULL,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    last_used_at DATETIME NULL,
    UNIQUE KEY uk_service_key_id (key_id),
    UNIQUE KEY uk_service_key_hash (key_hash),
    KEY idx_service_key_status (status, expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
