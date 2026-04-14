CREATE TABLE IF NOT EXISTS terminal_sessions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id     UUID REFERENCES devices(id) ON DELETE CASCADE,
    name          TEXT,
    command       TEXT NOT NULL,
    status        TEXT DEFAULT 'active',
    exit_code     INTEGER,
    cols          INTEGER DEFAULT 220,
    rows          INTEGER DEFAULT 50,
    started_at    TIMESTAMPTZ DEFAULT NOW(),
    ended_at      TIMESTAMPTZ,
    last_activity TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sessions_device_id ON terminal_sessions(device_id);
