-- Alertmanager delivery ledger used for tenant-scoped idempotency and
-- firing/resolved incident correlation. CREATE IF NOT EXISTS keeps this safe
-- for databases initialized from a newer schema.sql.
CREATE TABLE IF NOT EXISTS ws_alert_event (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64) NOT NULL,
    event_id      VARCHAR(128) NOT NULL,
    incident_key  VARCHAR(512) NOT NULL DEFAULT '',
    receiver      VARCHAR(256) NOT NULL DEFAULT '',
    group_key     VARCHAR(512) NOT NULL DEFAULT '',
    status        VARCHAR(32) NOT NULL DEFAULT 'firing',
    payload_json  JSON NOT NULL,
    task_id       VARCHAR(64) NOT NULL DEFAULT '',
    received_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at   DATETIME,
    UNIQUE KEY uk_alert_event (tenant_id, event_id),
    KEY idx_alert_incident (tenant_id, incident_key, received_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
