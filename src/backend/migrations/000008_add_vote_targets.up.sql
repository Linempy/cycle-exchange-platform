ALTER TABLE votes
    DROP CONSTRAINT IF EXISTS votes_chain_id_request_id_key;

ALTER TABLE votes
    ADD COLUMN target_request_id BIGINT REFERENCES exchange_offers(id) ON DELETE CASCADE;

ALTER TABLE votes
    ALTER COLUMN target_request_id SET NOT NULL,
    ADD CONSTRAINT votes_chain_request_target_key
        UNIQUE (chain_id, request_id, target_request_id);

CREATE INDEX idx_votes_target_request ON votes(target_request_id);
