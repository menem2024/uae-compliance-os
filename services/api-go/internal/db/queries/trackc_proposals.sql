-- Track C read of one proposal for GET /v1/proposals/{id} (a plain read: LockProposal is FOR UPDATE and
-- belongs to proposals.Decide). Added with Task 19; Track B's proposals.sql stays untouched.

-- name: TrackCGetProposal :one
SELECT * FROM proposals WHERE id = @id;
