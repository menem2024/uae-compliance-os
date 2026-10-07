-- Track C audit events (spec §5.5, §5.6.3). Append-only. proposal_id and invoice_id are nullable;
-- uuid.Nil stands for NULL in both directions.

-- name: TrackCInsertAuditEvent :one
INSERT INTO audit_events (firm_id, actor_type, actor_id, agent, proposal_id, action, entity_type, entity_id,
                          invoice_id, changes, before, after, reason, trace_id)
VALUES (@firm_id, @actor_type, @actor_id, @agent,
        NULLIF(@proposal_id::uuid, '00000000-0000-0000-0000-000000000000'::uuid),
        @action, @entity_type, @entity_id,
        NULLIF(@invoice_id::uuid, '00000000-0000-0000-0000-000000000000'::uuid),
        @changes, @before, @after, @reason, @trace_id)
RETURNING id, occurred_at;

-- name: TrackCListInvoiceAudit :many
-- An invoice's audit trail, newest first, keyset cursor (before_occurred_at, before_id).
SELECT id, occurred_at, actor_type, actor_id, agent,
       COALESCE(proposal_id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS proposal_id,
       action, entity_type, entity_id, changes, before, after, reason, trace_id
FROM audit_events
WHERE invoice_id = @invoice_id::uuid
  AND (sqlc.narg('before_occurred_at')::timestamptz IS NULL
       OR (occurred_at, id) < (sqlc.narg('before_occurred_at')::timestamptz, @before_id::uuid))
ORDER BY occurred_at DESC, id DESC
LIMIT @page_limit;
