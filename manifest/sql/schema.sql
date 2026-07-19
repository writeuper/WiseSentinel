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

CREATE TABLE IF NOT EXISTS ws_index_task (
    id            BIGINT PRIMARY KEY AUTO_INCREMENT,
    tenant_id     VARCHAR(64)  NOT NULL,
    task_id       VARCHAR(64)  NOT NULL,
    doc_id        VARCHAR(64)  NOT NULL,
    status        VARCHAR(32)  NOT NULL DEFAULT 'pending',
    chunk_count   INT          NOT NULL DEFAULT 0,
    error_msg     TEXT,
    started_at    DATETIME,
    finished_at   DATETIME,
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_task (tenant_id, task_id),
    KEY idx_status (status, created_at)
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
    created_by    VARCHAR(64)  NOT NULL DEFAULT '',
    started_at    DATETIME,
    finished_at   DATETIME,
    retry_count   INT          NOT NULL DEFAULT 0,
    max_retry     INT          NOT NULL DEFAULT 2,
    timeout_at    DATETIME,
    last_error    TEXT,
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_ops_task (tenant_id, task_id),
    KEY idx_status (tenant_id, status, created_at)
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
    resource_id   VARCHAR(64)  NOT NULL DEFAULT '',
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
