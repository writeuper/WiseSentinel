-- WiseSentinel Phase 1 schema
-- File: manifest/sql/schema.sql

CREATE TABLE IF NOT EXISTS ws_tenant (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64)  NOT NULL UNIQUE COMMENT '业务租户ID',
    name          VARCHAR(128) NOT NULL,
    status        TINYINT      NOT NULL DEFAULT 1 COMMENT '1=active 0=disabled',
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ws_user (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64)  NOT NULL,
    user_id       VARCHAR(64)  NOT NULL COMMENT 'SSO sub 或本地ID',
    display_name  VARCHAR(128) NOT NULL DEFAULT '',
    email         VARCHAR(256) NOT NULL DEFAULT '',
    status        TINYINT      NOT NULL DEFAULT 1,
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_tenant_user (tenant_id, user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ws_user_role (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64) NOT NULL,
    user_id       VARCHAR(64) NOT NULL,
    role          VARCHAR(32) NOT NULL COMMENT 'viewer|operator|sre_admin|platform_admin',
    created_at    DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_tenant_user_role (tenant_id, user_id, role)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Optional database-backed service identity registry. key_hash stores only
-- SHA-256(key); raw secrets are issued by a Secret Manager and never stored.
CREATE TABLE IF NOT EXISTS ws_service_api_key (
    id          BIGINT PRIMARY KEY AUTO_INCREMENT,
    key_id      VARCHAR(64) NOT NULL,
    key_hash    CHAR(64) NOT NULL,
    tenant_id   VARCHAR(64) NOT NULL,
    user_id     VARCHAR(64) NOT NULL,
    roles_json  JSON NOT NULL,
    scopes_json JSON NOT NULL,
    status      VARCHAR(16) NOT NULL DEFAULT 'active' COMMENT 'active|revoked',
    expires_at  DATETIME NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    last_used_at DATETIME NULL,
    UNIQUE KEY uk_service_key_id (key_id),
    UNIQUE KEY uk_service_key_hash (key_hash),
    KEY idx_service_key_status (status, expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ws_session (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64)  NOT NULL,
    session_id    VARCHAR(64)  NOT NULL,
    user_id       VARCHAR(64)  NOT NULL,
    title         VARCHAR(256) NOT NULL DEFAULT '新对话',
    agent_type    VARCHAR(32)  NOT NULL DEFAULT 'chat',
    status        TINYINT      NOT NULL DEFAULT 1,
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_session (tenant_id, session_id),
    KEY idx_user (tenant_id, user_id, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Durable ownership and safe replay state for synchronous Chat requests that
-- supply Idempotency-Key. Payload bodies are never stored here.
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

CREATE TABLE IF NOT EXISTS ws_document (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64)  NOT NULL,
    doc_id        VARCHAR(64)  NOT NULL COMMENT 'UUID',
    name          VARCHAR(512) NOT NULL,
    source_uri    VARCHAR(1024) NOT NULL COMMENT '_file_path 或 oss key',
    mime_type     VARCHAR(64)  NOT NULL DEFAULT 'text/markdown',
    visibility    VARCHAR(32)  NOT NULL DEFAULT 'tenant' COMMENT 'tenant|team|private',
    secret_level  TINYINT      NOT NULL DEFAULT 1 COMMENT '1=内部 2=敏感',
    version       INT          NOT NULL DEFAULT 1,
    status        VARCHAR(32)  NOT NULL DEFAULT 'active',
    created_by    VARCHAR(64)  NOT NULL,
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_doc (tenant_id, doc_id),
    KEY idx_source (tenant_id, source_uri(255))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Durable publication state for staged document index generations. Vector-store
-- writes are external to MySQL, so this table is the authority for which
-- generation may be returned to a caller.
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

-- Durable outbox for eventually deleting superseded or legacy RAG vectors.
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

CREATE TABLE IF NOT EXISTS ws_index_task (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64)  NOT NULL,
    task_id       VARCHAR(64)  NOT NULL,
    doc_id        VARCHAR(64)  NOT NULL,
    source_uri    VARCHAR(1024) NOT NULL DEFAULT '',
    visibility    VARCHAR(32)  NOT NULL DEFAULT 'tenant',
    secret_level  TINYINT      NOT NULL DEFAULT 1,
    layer         VARCHAR(32)  NOT NULL DEFAULT 'static' COMMENT 'static|fault_case|temp',
    version       VARCHAR(64)  NOT NULL DEFAULT '',
    service       VARCHAR(128) NOT NULL DEFAULT '',
    status        VARCHAR(32)  NOT NULL DEFAULT 'pending',
    chunk_count   INT          NOT NULL DEFAULT 0,
    error_msg     TEXT,
    started_at    DATETIME,
    finished_at   DATETIME,
    execution_token VARCHAR(64) NOT NULL DEFAULT '',
	lease_expires_at DATETIME,
    attempt_count  INT NOT NULL DEFAULT 0,
    max_attempts   INT NOT NULL DEFAULT 3,
    next_attempt_at DATETIME,
    generation    BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_task (tenant_id, task_id),
    UNIQUE KEY uk_index_generation (tenant_id, doc_id, generation),
    KEY idx_status (status, created_at),
	KEY idx_index_running (status, lease_expires_at, next_attempt_at, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

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

CREATE TABLE IF NOT EXISTS ws_ops_task (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64)  NOT NULL,
    task_id       VARCHAR(64)  NOT NULL,
    trigger_type  VARCHAR(32)  NOT NULL COMMENT 'manual|webhook|schedule',
    input_query   MEDIUMTEXT   NOT NULL,
    result        MEDIUMTEXT,
    detail_json   JSON COMMENT 'Plan-Execute 步骤明细',
    status        VARCHAR(32)  NOT NULL DEFAULT 'pending',
    trace_id      VARCHAR(64)  NOT NULL DEFAULT '',
    config_version VARCHAR(64) NOT NULL DEFAULT '' COMMENT 'Agent config snapshot selected at task submission',
    created_by    VARCHAR(64)  NOT NULL DEFAULT '',
    started_at    DATETIME,
    finished_at   DATETIME,
    retry_count   INT          NOT NULL DEFAULT 0,
    max_retry     INT          NOT NULL DEFAULT 2,
    timeout_at    DATETIME,
    next_attempt_at DATETIME,
    execution_token VARCHAR(64) NOT NULL DEFAULT '',
    last_error    TEXT,
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_ops_task (tenant_id, task_id),
    KEY idx_status (tenant_id, status, created_at),
    KEY idx_ops_runnable (status, next_attempt_at, timeout_at, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ws_approval (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64)  NOT NULL,
    approval_id   VARCHAR(64)  NOT NULL,
    task_id       VARCHAR(64)  NOT NULL COMMENT '关联 ops_task 或 tool_invoke',
    approval_type VARCHAR(32)  NOT NULL COMMENT 'tool_invoke|ops_remediation',
    payload_json  JSON         NOT NULL,
    status        VARCHAR(32)  NOT NULL DEFAULT 'pending',
    approver_id   VARCHAR(64)  NOT NULL DEFAULT '',
    comment       VARCHAR(512) NOT NULL DEFAULT '',
    expired_at    DATETIME     NOT NULL,
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_approval (tenant_id, approval_id),
    KEY idx_pending (tenant_id, status, expired_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ws_audit_log (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64)  NOT NULL,
    trace_id      VARCHAR(64)  NOT NULL,
    user_id       VARCHAR(64)  NOT NULL DEFAULT '',
    action        VARCHAR(64)  NOT NULL COMMENT 'chat.invoke|tool.invoke|doc.upload|ops.analyze',
    resource_type VARCHAR(32)  NOT NULL DEFAULT '',
    resource_id   VARCHAR(255) NOT NULL DEFAULT '',
    request_json  JSON,
    response_code INT          NOT NULL DEFAULT 0,
    latency_ms    INT          NOT NULL DEFAULT 0,
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    KEY idx_trace (trace_id),
    KEY idx_tenant_time (tenant_id, created_at),
    KEY idx_user (tenant_id, user_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ws_agent_trace (
    id          BIGINT PRIMARY KEY AUTO_INCREMENT,
    trace_id    VARCHAR(64)  NOT NULL,
    tenant_id   VARCHAR(64)  NOT NULL,
    user_id     VARCHAR(64)  NOT NULL DEFAULT '',
    agent_type  VARCHAR(32)  NOT NULL,
    config_version VARCHAR(64) NOT NULL DEFAULT '' COMMENT 'tenant Agent config snapshot used by this trace',
    session_id  VARCHAR(64)  NOT NULL DEFAULT '',
    task_id     VARCHAR(64)  NOT NULL DEFAULT '',
    query_text  MEDIUMTEXT,
    status      VARCHAR(32)  NOT NULL DEFAULT 'running',
    latency_ms  BIGINT       NOT NULL DEFAULT 0,
    error_msg   TEXT,
    started_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at DATETIME,
    created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_trace (trace_id),
    KEY idx_trace_tenant_time (tenant_id, created_at),
    KEY idx_trace_task (tenant_id, task_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ws_agent_trace_step (
    id             BIGINT PRIMARY KEY AUTO_INCREMENT,
    trace_id       VARCHAR(64)  NOT NULL,
    tenant_id      VARCHAR(64)  NOT NULL,
    agent_type     VARCHAR(32)  NOT NULL,
    step_type      VARCHAR(32)  NOT NULL COMMENT 'rag|tool|planner|executor|replanner|model',
    step_name      VARCHAR(128) NOT NULL DEFAULT '',
    input_summary  TEXT,
    output_summary TEXT,
    evidence_doc_ids JSON COMMENT 'bounded authorized document IDs for Citation grounding',
    status         VARCHAR(32)  NOT NULL DEFAULT 'success',
    latency_ms     BIGINT       NOT NULL DEFAULT 0,
    error_msg      TEXT,
    created_at     DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    KEY idx_trace_step (trace_id, id),
    KEY idx_tenant_time (tenant_id, created_at),
    KEY idx_step_type (agent_type, step_type, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ws_tool_call_record (
    id          BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id   VARCHAR(64)  NOT NULL,
    trace_id    VARCHAR(64)  NOT NULL DEFAULT '',
    approval_id VARCHAR(64)  NOT NULL DEFAULT '' COMMENT 'durable approval reference; empty for non-approval calls',
    tool_name   VARCHAR(128) NOT NULL,
    agent_type  VARCHAR(32)  NOT NULL DEFAULT '',
    input_json  TEXT,
    output_text MEDIUMTEXT,
    status      VARCHAR(32)  NOT NULL DEFAULT 'success',
    latency_ms  BIGINT       NOT NULL DEFAULT 0,
    created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    KEY idx_trace_tool (trace_id, id),
    KEY idx_approval (tenant_id, approval_id, created_at),
    KEY idx_tenant_tool_time (tenant_id, tool_name, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ws_fault_knowledge (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64)  NOT NULL,
    card_id       VARCHAR(64)  NOT NULL,
    task_id       VARCHAR(64)  NOT NULL DEFAULT '',
    trace_id      VARCHAR(64)  NOT NULL DEFAULT '',
    title         VARCHAR(512) NOT NULL DEFAULT '',
    symptom       TEXT,
    impact        TEXT,
    root_cause    TEXT,
    workaround    TEXT,
    remediation   TEXT,
    evidence_json JSON,
    service       VARCHAR(128) NOT NULL DEFAULT '',
    version       VARCHAR(64)  NOT NULL DEFAULT '',
    status        VARCHAR(32)  NOT NULL DEFAULT 'draft' COMMENT 'draft|approved|rejected|archived',
    weight        DOUBLE       NOT NULL DEFAULT 1.0,
    doc_id        VARCHAR(64)  NOT NULL DEFAULT '',
    hit_count     INT          NOT NULL DEFAULT 0,
    useful_count  INT          NOT NULL DEFAULT 0,
    bad_count     INT          NOT NULL DEFAULT 0,
    created_by    VARCHAR(64)  NOT NULL DEFAULT '',
    reviewed_by   VARCHAR(64)  NOT NULL DEFAULT '',
    reviewed_at   DATETIME,
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_card (tenant_id, card_id),
    UNIQUE KEY uk_task (tenant_id, task_id),
    KEY idx_status_time (tenant_id, status, updated_at),
    KEY idx_service_version (tenant_id, service, version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ws_feedback (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64) NOT NULL,
    feedback_id   VARCHAR(64) NOT NULL,
    target_type   VARCHAR(32) NOT NULL COMMENT 'fault_knowledge|answer|tool_call',
    target_id     VARCHAR(64) NOT NULL,
    rating        VARCHAR(16) NOT NULL COMMENT 'useful|bad',
    comment       VARCHAR(1024) NOT NULL DEFAULT '',
    created_by    VARCHAR(64) NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_feedback (tenant_id, feedback_id),
    KEY idx_target (tenant_id, target_type, target_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS ws_agent_config (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64)  NOT NULL,
    agent_type    VARCHAR(32)  NOT NULL,
    version       VARCHAR(32)  NOT NULL,
    config_json   JSON         NOT NULL COMMENT 'prompt, tools, model profile, graph params',
    is_active     TINYINT      NOT NULL DEFAULT 0,
    created_by    VARCHAR(64)  NOT NULL DEFAULT '',
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_agent_ver (tenant_id, agent_type, version),
    KEY idx_active (tenant_id, agent_type, is_active)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO ws_tenant (tenant_id, name) VALUES ('default', 'Default Tenant');

INSERT IGNORE INTO ws_user (tenant_id, user_id, display_name, email)
VALUES ('default', 'dev_user', 'Dev User', 'sre@example.com');

-- M5 RBAC seed: provide one account per role for testing the matrix.
-- The dev account "sre@example.com" is granted every role so the
-- Phase 1 acceptance matrix can be exercised end-to-end.
INSERT IGNORE INTO ws_user (tenant_id, user_id, display_name, email)
VALUES ('default', 'viewer',   'Viewer Demo',        'viewer@example.com'),
       ('default', 'operator', 'Operator Demo',      'operator@example.com'),
       ('default', 'sre_admin','SRE Admin Demo',     'sreadmin@example.com'),
       ('default', 'platform_admin','Platform Admin', 'platformadmin@example.com');

INSERT IGNORE INTO ws_user_role (tenant_id, user_id, role) VALUES
    ('default', 'sre@example.com', 'viewer'),
    ('default', 'sre@example.com', 'operator'),
    ('default', 'sre@example.com', 'sre_admin'),
    ('default', 'sre@example.com', 'platform_admin'),
    ('default', 'viewer',          'viewer'),
    ('default', 'operator',        'operator'),
    ('default', 'operator',        'viewer'),
    ('default', 'sre_admin',       'sre_admin'),
    ('default', 'sre_admin',       'operator'),
    ('default', 'sre_admin',       'viewer'),
    ('default', 'platform_admin',  'platform_admin'),
    ('default', 'platform_admin',  'sre_admin'),
    ('default', 'platform_admin',  'operator'),
    ('default', 'platform_admin',  'viewer');

-- M5 seed Agent configurations: at least one version per agent type with
-- is_active=1 so the /admin/agent-configs page has something to render.
INSERT IGNORE INTO ws_agent_config (tenant_id, agent_type, version, config_json, is_active, created_by)
VALUES
    ('default', 'chat',     'v1', JSON_OBJECT('system_prompt', '你是 WiseSentinel 的智能助手 …', 'max_iterations', 25, 'tools', JSON_ARRAY('query_prometheus_alerts','query_internal_docs','get_current_time','query_logs')), 1, 'system'),
    ('default', 'chat',     'v2-beta', JSON_OBJECT('system_prompt', '你是 WiseSentinel 的智能助手 v2', 'max_iterations', 30, 'tools', JSON_ARRAY('query_prometheus_alerts','query_internal_docs','get_current_time','query_logs','mysql_readonly')), 0, 'system'),
    ('default', 'ops',      'v1', JSON_OBJECT('max_iterations', 20, 'system_prompt', '你是智能运维告警分析助手 …', 'tools', JSON_ARRAY('query_prometheus_alerts','query_internal_docs','get_current_time','query_logs')), 1, 'system'),
    ('default', 'knowledge','v1', JSON_OBJECT('chunk_size', 500, 'overlap', 50, 'splitter', 'markdown'), 1, 'system');
