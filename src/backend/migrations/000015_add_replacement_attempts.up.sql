CREATE TABLE chain_replacement_attempts (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    chain_id   BIGINT NOT NULL REFERENCES chains(id) ON DELETE CASCADE,
    request_id BIGINT NOT NULL REFERENCES exchange_offers(id) ON DELETE CASCADE,
    status     VARCHAR(10) NOT NULL
               CHECK (status IN ('INVITED', 'DECLINED', 'ACCEPTED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (chain_id, request_id)
);

CREATE UNIQUE INDEX ux_chain_replacement_attempts_active
    ON chain_replacement_attempts (chain_id)
    WHERE status = 'INVITED';
