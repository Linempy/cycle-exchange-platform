DROP INDEX IF EXISTS idx_votes_target_request;

ALTER TABLE votes
    DROP CONSTRAINT IF EXISTS votes_chain_request_target_key;

-- The old schema allowed only one vote per source request in a chain.
WITH duplicates AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY chain_id, request_id
               ORDER BY voted_at DESC NULLS LAST, id DESC
           ) AS row_number
    FROM votes
)
DELETE FROM votes
WHERE id IN (
    SELECT id
    FROM duplicates
    WHERE row_number > 1
);

ALTER TABLE votes
    DROP COLUMN target_request_id,
    ADD CONSTRAINT votes_chain_id_request_id_key UNIQUE (chain_id, request_id);
