-- Task 20, plan finding F14: AgentTaskRequested.client_company_id comes from Track B's
-- invoices.client_company_id (migration 00013). The column is nullable; uuid.Nil stands for NULL.

-- name: TrackCFixInvoiceContext :one
SELECT COALESCE(client_company_id, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS client_company_id
FROM invoices
WHERE id = @id;
