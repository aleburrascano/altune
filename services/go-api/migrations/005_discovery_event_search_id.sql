-- discovery: the search_id keystone.
-- A UUID minted per search_performed and threaded onto every downstream
-- engagement event (results_shown, result_clicked, play, skip, completed) so a
-- show-conditioned join — CTR@position, MRR/NDCG, counterfactual replay — is
-- computable. A real column (not JSONB) because it is the join key; nullable
-- because non-search-originated events (a play from the library) carry none.
ALTER TABLE discovery_events ADD COLUMN IF NOT EXISTS search_id UUID;

-- Join engagement events back to their originating search.
CREATE INDEX IF NOT EXISTS idx_discovery_events_search_id
    ON discovery_events (search_id)
    WHERE search_id IS NOT NULL;
