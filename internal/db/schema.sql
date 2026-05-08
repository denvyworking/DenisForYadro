CREATE TABLE IF NOT EXISTS logs (
    id BIGSERIAL PRIMARY KEY,
    source_path TEXT NOT NULL,
    status TEXT NOT NULL,
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    parsed_at TIMESTAMPTZ,
    error_message TEXT NOT NULL DEFAULT '',
    nodes_count INTEGER NOT NULL DEFAULT 0,
    ports_count INTEGER NOT NULL DEFAULT 0,
    raw_size BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS nodes (
    id BIGSERIAL PRIMARY KEY,
    log_id BIGINT NOT NULL REFERENCES logs(id) ON DELETE CASCADE,
    external_id TEXT NOT NULL,
    name TEXT NOT NULL,
    node_type TEXT NOT NULL,
    group_name TEXT NOT NULL DEFAULT '',
    UNIQUE (log_id, external_id)
);

CREATE TABLE IF NOT EXISTS nodes_info (
    id BIGSERIAL PRIMARY KEY,
    node_id BIGINT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    vendor TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    details JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE IF NOT EXISTS ports (
    id BIGSERIAL PRIMARY KEY,
    log_id BIGINT NOT NULL REFERENCES logs(id) ON DELETE CASCADE,
    node_id BIGINT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    external_id TEXT NOT NULL,
    name TEXT NOT NULL,
    speed TEXT NOT NULL DEFAULT '',
    peer_node_id BIGINT REFERENCES nodes(id) ON DELETE SET NULL,
    peer_port_id BIGINT REFERENCES ports(id) ON DELETE SET NULL,
    UNIQUE (log_id, external_id)
);

CREATE INDEX IF NOT EXISTS idx_nodes_log_id ON nodes(log_id);
CREATE INDEX IF NOT EXISTS idx_ports_log_id ON ports(log_id);
CREATE INDEX IF NOT EXISTS idx_ports_node_id ON ports(node_id);
CREATE INDEX IF NOT EXISTS idx_nodes_info_node_id ON nodes_info(node_id);
