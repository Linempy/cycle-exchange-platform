CREATE TABLE chain_deadline_events (
    chain_id   BIGINT NOT NULL REFERENCES chains(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason     VARCHAR(50) NOT NULL CHECK (reason IN ('deadline_expired')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, user_id, reason)
);

CREATE INDEX idx_chain_deadline_events_user
    ON chain_deadline_events (user_id, created_at DESC);
