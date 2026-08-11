UPDATE votes SET vote = 'pending' WHERE vote = 'thinking';

ALTER TABLE votes
    DROP CONSTRAINT IF EXISTS votes_vote_check;

ALTER TABLE votes
    ADD CONSTRAINT votes_vote_check
    CHECK (vote IN ('pending', 'approved', 'rejected'));
