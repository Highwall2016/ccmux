CREATE TABLE IF NOT EXISTS devices (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    device_token TEXT NOT NULL,
    platform     TEXT NOT NULL,
    last_seen    TIMESTAMPTZ,
    created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_devices_user_id ON devices(user_id);
