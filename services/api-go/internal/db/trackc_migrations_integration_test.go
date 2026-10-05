//go:build integration

package db_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pressly/goose/v3"

	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/trackctest"
)

// pgCode returns the SQLSTATE of err ("" when err is not a Postgres error).
func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func pgConstraint(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.ConstraintName
	}
	return ""
}

// wantViolation fails unless err is a Postgres error with SQLSTATE code (and, when constraint is
// not empty, that constraint name).
func wantViolation(t *testing.T, what string, err error, code, constraint string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: succeeded, want SQLSTATE %s %s", what, code, constraint)
	}
	if pgCode(err) != code || (constraint != "" && pgConstraint(err) != constraint) {
		t.Fatalf("%s: got %v (SQLSTATE %q, constraint %q), want SQLSTATE %s %s",
			what, err, pgCode(err), pgConstraint(err), code, constraint)
	}
}

func appExec(t *testing.T, e trackctest.Env, firm uuid.UUID, sql string, args ...any) error {
	t.Helper()
	return e.AppTx(context.Background(), firm, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), sql, args...)
		return err
	})
}

// F2: a CHECK passes when its expression is NULL, so the naive
// CHECK (status <> 'ready' OR approved_payload_version = payload_version) accepts this exact row.
func TestTrackCReadyNeedsApproval(t *testing.T) {
	e := trackctest.Setup(t)
	inv := e.SeedInvoice(t, e.FirmA, `{}`)

	err := appExec(t, e, e.FirmA, `UPDATE invoices SET status = 'ready' WHERE id = $1`, inv)
	wantViolation(t, "ready with approved_payload_version IS NULL", err, "23514", "invoices_ready_needs_approval")

	err = appExec(t, e, e.FirmA, `UPDATE invoices SET status = 'ready', approved_payload_version = payload_version + 1,
		approved_by = 'u', approved_at = now() WHERE id = $1`, inv)
	wantViolation(t, "ready with a stale approval", err, "23514", "invoices_ready_needs_approval")

	err = appExec(t, e, e.FirmA, `UPDATE invoices SET approved_by = 'u' WHERE id = $1`, inv)
	wantViolation(t, "approved_by without version and time", err, "23514", "invoices_approval_consistent")

	err = appExec(t, e, e.FirmA, `UPDATE invoices SET approved_payload_version = 1, approved_at = now() WHERE id = $1`, inv)
	wantViolation(t, "approval without approved_by", err, "23514", "invoices_approval_consistent")

	err = appExec(t, e, e.FirmA, `UPDATE invoices SET payload_version = 0 WHERE id = $1`, inv)
	wantViolation(t, "payload_version 0", err, "23514", "")

	if err := appExec(t, e, e.FirmA, `UPDATE invoices SET status = 'ready', approved_payload_version = payload_version,
		approved_by = 'u', approved_at = now() WHERE id = $1`, inv); err != nil {
		t.Fatalf("ready with a matching approval: %v", err)
	}
	err = appExec(t, e, e.FirmA, `UPDATE invoices SET payload_version = payload_version + 1 WHERE id = $1`, inv)
	wantViolation(t, "payload change keeps ready", err, "23514", "invoices_ready_needs_approval")
}

// seeded holds one row of every Track C table for one invoice of firm A.
type seeded struct {
	inv, run, issue, audit, export, fixTask uuid.UUID
}

// seedRaw inserts one row in every Track C table as compliance_app with plain SQL.
func seedRaw(t *testing.T, e trackctest.Env, firm uuid.UUID) seeded {
	t.Helper()
	ctx := context.Background()
	s := seeded{inv: e.SeedInvoice(t, firm, `{}`), issue: uuid.New(), export: uuid.New(), fixTask: uuid.New()}
	err := e.AppTx(ctx, firm, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO validation_runs (firm_id, invoice_id, payload_version, ruleset_version,
			trigger, error_count, warning_count, rules_evaluated, duration_us)
			VALUES ($1, $2, 1, 'pint-ae@1.0.4+r1', 'manual', 1, 0, 300, 42) RETURNING id`, firm, s.inv).Scan(&s.run); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO validation_issues (id, firm_id, run_id, invoice_id, seq, rule_id, severity, path, message)
			VALUES ($1, $2, $3, $4, 0, 'ibr-132-ae', 'error', 'seller_trn', 'm')`, s.issue, firm, s.run, s.inv); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO audit_events (firm_id, actor_type, actor_id, action, entity_type, entity_id, invoice_id)
			VALUES ($1, 'user', 'u1', 'invoice.approved', 'invoice', $2, $2) RETURNING id`, firm, s.inv).Scan(&s.audit); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO exports (id, firm_id, invoice_id, run_id, payload_version, ruleset_version, format,
			document_kind, object_key, sha256, size_bytes, created_by)
			VALUES ($1::uuid, $2::uuid, $3, $4, 1, 'pint-ae@1.0.4+r1', 'pint-ae-billing-1.0.4/ubl-2.1', 'invoice',
			'firms/' || $2::uuid::text || '/exports/' || $1::uuid::text || '.xml', repeat('a', 64), 10, 'u1')`,
			s.export, firm, s.inv, s.run); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO fix_tasks (id, firm_id, invoice_id, run_id, mode) VALUES ($1, $2, $3, $4, 'auto')`,
			s.fixTask, firm, s.inv, s.run)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var appendOnlyTables = []string{"validation_runs", "validation_issues", "audit_events", "exports"}

// AC-8 (E3): UPDATE, DELETE and TRUNCATE of an append-only table fail with 42501 for compliance_app
// (missing privilege) and for compliance_owner (trackc_forbid_mutation). Row triggers fire only on
// rows the statement touches; under FORCE RLS the owner sees a Firm's rows only with app.firm_id
// set, so every statement runs with the Firm set and targets the seeded rows.
func TestTrackCAppendOnly(t *testing.T) {
	e := trackctest.Setup(t)
	ctx := context.Background()
	seedRaw(t, e, e.FirmA)

	roles := []struct {
		name string
		tx   func(context.Context, uuid.UUID, func(pgx.Tx) error) error
		msg  string // the error message proves which mechanism refused
	}{
		{"compliance_app", e.AppTx, "permission denied"},
		{"compliance_owner", e.OwnerTx, "append-only"},
	}
	for _, table := range appendOnlyTables {
		for _, r := range roles {
			stmts := []string{
				`UPDATE ` + table + ` SET firm_id = firm_id WHERE firm_id = $1`,
				`DELETE FROM ` + table + ` WHERE firm_id = $1`,
				`TRUNCATE ` + table + ` CASCADE`,
			}
			// validation_runs is referenced by foreign keys, so a plain TRUNCATE by the owner is
			// refused earlier (0A000, below); CASCADE reaches the trigger.
			if table != "validation_runs" || r.name == "compliance_app" {
				stmts = append(stmts, `TRUNCATE `+table)
			}
			for _, stmt := range stmts {
				err := r.tx(ctx, e.FirmA, func(tx pgx.Tx) error {
					var args []any
					if !strings.HasPrefix(stmt, "TRUNCATE") {
						args = append(args, e.FirmA)
					}
					_, err := tx.Exec(ctx, stmt, args...)
					return err
				})
				if pgCode(err) != "42501" || !strings.Contains(err.Error(), r.msg) {
					t.Errorf("%s: %s: got %v, want SQLSTATE 42501 (%q)", r.name, stmt, err, r.msg)
				}
			}
		}
	}

	err := e.OwnerTx(ctx, e.FirmA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `TRUNCATE validation_runs`)
		return err
	})
	if pgCode(err) != "0A000" {
		t.Errorf("owner plain TRUNCATE validation_runs: got %v, want 0A000 (referenced by foreign keys)", err)
	}

	for _, table := range appendOnlyTables {
		var n int
		if err := e.AppTx(ctx, e.FirmA, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE firm_id = $1`, e.FirmA).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%s: %d rows after the refused mutations, want 1", table, n)
		}
	}
}

// No DELETE or TRUNCATE for compliance_app on any table; append-only tables get SELECT and INSERT
// only, fix_tasks SELECT, INSERT and UPDATE; PUBLIC has nothing on any Track C table.
func TestTrackCGrants(t *testing.T) {
	e := trackctest.Setup(t)
	ctx := context.Background()

	rows, err := e.Owner.Query(ctx, `SELECT c.relname FROM pg_class c JOIN pg_namespace ns ON ns.oid = c.relnamespace
		WHERE ns.nspname = 'public' AND c.relkind = 'r'
		  AND (has_table_privilege('compliance_app', c.oid, 'DELETE') OR has_table_privilege('compliance_app', c.oid, 'TRUNCATE'))`)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) > 0 {
		t.Errorf("compliance_app may DELETE or TRUNCATE %v", bad)
	}

	want := map[string]string{
		"validation_runs": "SELECT,INSERT", "validation_issues": "SELECT,INSERT", "audit_events": "SELECT,INSERT",
		"exports": "SELECT,INSERT", "fix_tasks": "SELECT,INSERT,UPDATE",
	}
	for table, privs := range want {
		var got string
		if err := e.Owner.QueryRow(ctx, `SELECT coalesce(string_agg(p, ',' ORDER BY ord), '')
			FROM unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE','TRUNCATE','REFERENCES','TRIGGER']) WITH ORDINALITY AS u(p, ord)
			WHERE has_table_privilege('compliance_app', $1::regclass, p)`, table).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != privs {
			t.Errorf("%s: compliance_app has %q, want %q", table, got, privs)
		}
		var publicGrants int
		if err := e.Owner.QueryRow(ctx, `SELECT count(*) FROM pg_class c, aclexplode(c.relacl) a
			WHERE c.oid = $1::regclass AND a.grantee = 0`, table).Scan(&publicGrants); err != nil {
			t.Fatal(err)
		}
		if publicGrants != 0 {
			t.Errorf("%s: PUBLIC holds %d privileges", table, publicGrants)
		}
	}
}

var trackCTables = []string{"invoices", "validation_runs", "validation_issues", "audit_events", "exports", "fix_tasks"}

// §3.3: the existing catalogue check in rls_integration_test.go covers every table with a firm_id
// column; this pins that the Track C tables are among them and carry the firm_isolation policy.
func TestTrackCRLSCatalogue(t *testing.T) {
	e := trackctest.Setup(t)
	ctx := context.Background()
	for _, table := range trackCTables {
		var rls, force, hasFirmID bool
		var using, check string
		err := e.Owner.QueryRow(ctx, `SELECT c.relrowsecurity, c.relforcerowsecurity,
			EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'firm_id' AND a.attnotnull),
			coalesce(p.qual, ''), coalesce(p.with_check, '')
			FROM pg_class c LEFT JOIN pg_policies p ON p.tablename = c.relname AND p.policyname = 'firm_isolation'
			WHERE c.oid = $1::regclass`, table).Scan(&rls, &force, &hasFirmID, &using, &check)
		if err != nil {
			t.Fatal(err)
		}
		if !rls || !force || !hasFirmID {
			t.Errorf("%s: rowsecurity=%v force=%v firm_id NOT NULL=%v", table, rls, force, hasFirmID)
		}
		for _, expr := range []string{using, check} {
			if !strings.Contains(expr, "current_setting('app.firm_id'::text, true)") {
				t.Errorf("%s: firm_isolation expression %q does not use app.firm_id", table, expr)
			}
		}
	}
}

// AC-9: Firm B neither reads nor writes Firm A's rows; composite foreign keys keep every row's
// Firm equal to its parent's.
func TestTrackCIsolationAndCompositeFKs(t *testing.T) {
	e := trackctest.Setup(t)
	ctx := context.Background()
	a := seedRaw(t, e, e.FirmA)
	b := seedRaw(t, e, e.FirmB)

	for _, table := range trackCTables {
		var n int
		if err := e.AppTx(ctx, e.FirmB, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE firm_id = $1`, e.FirmA).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("firm B sees %d of firm A's %s rows", n, table)
		}
	}

	runSQL := `INSERT INTO validation_runs (firm_id, invoice_id, payload_version, ruleset_version, trigger,
		error_count, warning_count, rules_evaluated, duration_us) VALUES ($1, $2, 1, 'r', 'manual', 0, 0, 0, 0)`
	wantViolation(t, "firm B inserts a firm A row", appExec(t, e, e.FirmB, runSQL, e.FirmA, a.inv), "42501", "")
	wantViolation(t, "firm B run on firm A's invoice", appExec(t, e, e.FirmB, runSQL, e.FirmB, a.inv),
		"23503", "validation_runs_invoice_fk")

	issueSQL := `INSERT INTO validation_issues (id, firm_id, run_id, invoice_id, seq, rule_id, severity, path, message)
		VALUES (gen_random_uuid(), $1, $2, $3, 9, 'ibr-001', 'error', 'p', 'm')`
	wantViolation(t, "firm B issue on firm A's run", appExec(t, e, e.FirmB, issueSQL, e.FirmB, a.run, a.inv),
		"23503", "validation_issues_run_fk")
	other := e.SeedInvoice(t, e.FirmA, `{}`)
	wantViolation(t, "issue whose run belongs to another invoice", appExec(t, e, e.FirmA, issueSQL, e.FirmA, a.run, other),
		"23503", "validation_issues_run_fk")

	wantViolation(t, "firm B audit event on firm A's invoice", appExec(t, e, e.FirmB, `INSERT INTO audit_events
		(firm_id, actor_type, actor_id, action, entity_type, entity_id, invoice_id)
		VALUES ($1, 'system', 'api-go', 'invoice.approved', 'invoice', $2, $2)`, e.FirmB, a.inv), "23503", "audit_events_invoice_fk")

	exportSQL := `INSERT INTO exports (id, firm_id, invoice_id, run_id, payload_version, ruleset_version, format,
		document_kind, object_key, sha256, size_bytes, created_by)
		VALUES ($1::uuid, $2::uuid, $3, $4, 1, 'r', 'f', 'invoice',
		'firms/' || $2::uuid::text || '/exports/' || $1::uuid::text || '.xml', repeat('a', 64), 1, 'u')`
	wantViolation(t, "export whose run belongs to another invoice",
		appExec(t, e, e.FirmA, exportSQL, uuid.New(), e.FirmA, other, a.run), "23503", "exports_run_fk")
	wantViolation(t, "firm B export of firm A's invoice",
		appExec(t, e, e.FirmB, exportSQL, uuid.New(), e.FirmB, a.inv, b.run), "23503", "exports_invoice_fk")

	fixSQL := `INSERT INTO fix_tasks (id, firm_id, invoice_id, run_id, mode) VALUES (gen_random_uuid(), $1, $2, $3, 'on_demand')`
	wantViolation(t, "fix task whose run belongs to another invoice",
		appExec(t, e, e.FirmA, fixSQL, e.FirmA, other, a.run), "23503", "fix_tasks_run_fk")
	wantViolation(t, "firm B fix task on firm A's invoice",
		appExec(t, e, e.FirmB, fixSQL, e.FirmB, a.inv, b.run), "23503", "fix_tasks_invoice_fk")

	wantViolation(t, "latest_run_id of another invoice's run",
		appExec(t, e, e.FirmA, `UPDATE invoices SET latest_run_id = $2 WHERE id = $1`, other, a.run), "23503", "invoices_latest_run_fk")
	if err := appExec(t, e, e.FirmA, `UPDATE invoices SET latest_run_id = $2 WHERE id = $1`, a.inv, a.run); err != nil {
		t.Fatalf("latest_run_id of the invoice's own run: %v", err)
	}
}

// Column CHECKs of the append-only tables and fix_tasks.
func TestTrackCColumnChecks(t *testing.T) {
	e := trackctest.Setup(t)
	s := seedRaw(t, e, e.FirmA)
	f := e.FirmA

	runSQL := `INSERT INTO validation_runs (firm_id, invoice_id, payload_version, ruleset_version, trigger,
		error_count, warning_count, rules_evaluated, duration_us) VALUES ($1, $2, 1, 'r', $3, $4, 0, 0, 0)`
	wantViolation(t, "unknown trigger", appExec(t, e, f, runSQL, f, s.inv, "cron", 0), "23514", "")
	wantViolation(t, "negative error_count", appExec(t, e, f, runSQL, f, s.inv, "manual", -1), "23514", "")

	issueSQL := `INSERT INTO validation_issues (id, firm_id, run_id, invoice_id, seq, rule_id, severity, path, message, message_args)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, 'p', 'm', $7::jsonb)`
	for i, id := range []string{"ibr-132-ae", "aligned-ibrp-s-08", "ibr-co-10", "ibr-cl-01", "AE-FMT-001", "AE-TRN-001"} {
		if err := appExec(t, e, f, issueSQL, f, s.run, s.inv, 100+i, id, "warning", `{}`); err != nil {
			t.Errorf("rule_id %q rejected: %v", id, err)
		}
	}
	for i, id := range []string{"IBR-132", "ibr", "AE-FMT-01", "AE-fmt-001", "ibr_132", "ibr-132 ", ""} {
		wantViolation(t, "rule_id "+id, appExec(t, e, f, issueSQL, f, s.run, s.inv, 200+i, id, "error", `{}`), "23514", "")
	}
	wantViolation(t, "severity info", appExec(t, e, f, issueSQL, f, s.run, s.inv, 300, "ibr-001", "info", `{}`), "23514", "")
	wantViolation(t, "message_args array", appExec(t, e, f, issueSQL, f, s.run, s.inv, 301, "ibr-001", "error", `[]`), "23514", "")
	wantViolation(t, "duplicate seq", appExec(t, e, f, issueSQL, f, s.run, s.inv, 0, "ibr-001", "error", `{}`),
		"23505", "validation_issues_run_seq_uniq")

	auditSQL := `INSERT INTO audit_events (firm_id, actor_type, actor_id, agent, action, entity_type, entity_id, changes)
		VALUES ($1, $2, $3, $4, $5, $6, gen_random_uuid(), $7::jsonb)`
	if err := appExec(t, e, f, auditSQL, f, "agent", "fix", "fix", "proposal.accepted", "proposal", `[]`); err != nil {
		t.Errorf("valid audit event rejected: %v", err)
	}
	for name, args := range map[string][]any{
		"actor_type bot":    {"bot", "x", "", "invoice.approved", "invoice", `[]`},
		"empty actor_id":    {"user", "", "", "invoice.approved", "invoice", `[]`},
		"agent Fix":         {"agent", "x", "Fix", "invoice.approved", "invoice", `[]`},
		"action no dot":     {"user", "x", "", "approved", "invoice", `[]`},
		"action upper":      {"user", "x", "", "Invoice.Approved", "invoice", `[]`},
		"entity_type run":   {"user", "x", "", "invoice.approved", "validation_run", `[]`},
		"changes an object": {"user", "x", "", "invoice.approved", "invoice", `{}`},
	} {
		wantViolation(t, name, appExec(t, e, f, auditSQL, append([]any{f}, args...)...), "23514", "")
	}

	exportSQL := `INSERT INTO exports (id, firm_id, invoice_id, run_id, payload_version, ruleset_version, format,
		document_kind, object_key, sha256, size_bytes, created_by) VALUES ($1, $2, $3, $4, 1, 'r', 'f', $5, $6, $7, $8, 'u')`
	id := uuid.New()
	key := "firms/" + f.String() + "/exports/" + id.String() + ".xml"
	sha := strings.Repeat("0f", 32)
	wantViolation(t, "object_key of another export", appExec(t, e, f, exportSQL, id, f, s.inv, s.run, "invoice",
		"firms/"+f.String()+"/exports/"+uuid.NewString()+".xml", sha, 1), "23514", "exports_object_key_shape")
	wantViolation(t, "object_key with ../", appExec(t, e, f, exportSQL, id, f, s.inv, s.run, "invoice",
		"firms/"+f.String()+"/exports/../"+id.String()+".xml", sha, 1), "23514", "exports_object_key_shape")
	wantViolation(t, "upper-case sha256", appExec(t, e, f, exportSQL, id, f, s.inv, s.run, "invoice", key,
		strings.ToUpper(sha), 1), "23514", "")
	wantViolation(t, "size 0", appExec(t, e, f, exportSQL, id, f, s.inv, s.run, "invoice", key, sha, 0), "23514", "")
	wantViolation(t, "document_kind order", appExec(t, e, f, exportSQL, id, f, s.inv, s.run, "order", key, sha, 1), "23514", "")
	if err := appExec(t, e, f, exportSQL, id, f, s.inv, s.run, "credit_note", key, sha, 1); err != nil {
		t.Fatalf("valid export rejected: %v", err)
	}

	fixSQL := `INSERT INTO fix_tasks (id, firm_id, invoice_id, run_id, mode, status, outcome, completed_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7)`
	wantViolation(t, "mode manual", appExec(t, e, f, fixSQL, f, s.inv, s.run, "manual", "requested", "", nil), "23514", "")
	wantViolation(t, "status done", appExec(t, e, f, fixSQL, f, s.inv, s.run, "on_demand", "done", "", time.Now()), "23514", "")
	wantViolation(t, "outcome fixed", appExec(t, e, f, fixSQL, f, s.inv, s.run, "on_demand", "requested", "fixed", nil), "23514", "")
	wantViolation(t, "requested with completed_at", appExec(t, e, f, fixSQL, f, s.inv, s.run, "on_demand", "requested", "", time.Now()),
		"23514", "fix_tasks_completed_consistent")
	wantViolation(t, "terminal without completed_at", appExec(t, e, f, fixSQL, f, s.inv, s.run, "on_demand", "failed", "", nil),
		"23514", "fix_tasks_completed_consistent")
	wantViolation(t, "second task for the same run and mode", appExec(t, e, f, fixSQL, f, s.inv, s.run, "auto", "requested", "", nil),
		"23505", "fix_tasks_run_mode_uniq")
}

// The fix_tasks guard allows one requested -> terminal transition, publishing while requested, and
// nothing else.
func TestTrackCFixTaskGuard(t *testing.T) {
	e := trackctest.Setup(t)
	s := seedRaw(t, e, e.FirmA)
	f := e.FirmA

	for name, stmt := range map[string]string{
		"change run_id":               `UPDATE fix_tasks SET run_id = gen_random_uuid() WHERE id = $1`,
		"change mode":                 `UPDATE fix_tasks SET mode = 'on_demand' WHERE id = $1`,
		"change requested_by":         `UPDATE fix_tasks SET requested_by = 'x' WHERE id = $1`,
		"change requested_at":         `UPDATE fix_tasks SET requested_at = requested_at - interval '1 hour' WHERE id = $1`,
		"outcome while requested":     `UPDATE fix_tasks SET outcome = 'proposed' WHERE id = $1`,
		"error_code while requested":  `UPDATE fix_tasks SET error_code = 'x' WHERE id = $1`,
		"proposal_id while requested": `UPDATE fix_tasks SET proposal_id = gen_random_uuid() WHERE id = $1`,
	} {
		wantViolation(t, name, appExec(t, e, f, stmt, s.fixTask), "23514", "")
	}
	if err := appExec(t, e, f, `UPDATE fix_tasks SET published_at = now() WHERE id = $1`, s.fixTask); err != nil {
		t.Fatalf("publish while requested: %v", err)
	}
	if err := appExec(t, e, f, `UPDATE fix_tasks SET status = 'succeeded', outcome = 'proposed', proposal_id = gen_random_uuid(),
		agent_run_id = gen_random_uuid(), completed_at = now() WHERE id = $1`, s.fixTask); err != nil {
		t.Fatalf("requested -> succeeded: %v", err)
	}
	for name, stmt := range map[string]string{
		"terminal -> requested": `UPDATE fix_tasks SET status = 'requested', completed_at = NULL WHERE id = $1`,
		"terminal -> terminal":  `UPDATE fix_tasks SET status = 'failed' WHERE id = $1`,
		"terminal outcome":      `UPDATE fix_tasks SET outcome = 'no_fix' WHERE id = $1`,
		"terminal published_at": `UPDATE fix_tasks SET published_at = now() WHERE id = $1`,
		"terminal no-op":        `UPDATE fix_tasks SET status = status WHERE id = $1`,
	} {
		wantViolation(t, name, appExec(t, e, f, stmt, s.fixTask), "23514", "")
	}
	err := e.OwnerTx(context.Background(), f, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE fix_tasks SET status = 'failed' WHERE id = $1`, s.fixTask)
		return err
	})
	wantViolation(t, "owner terminal -> terminal", err, "23514", "")
}

// recordingDBTX is a sqlc.DBTX that records every statement it forwards.
type recordingDBTX struct {
	tx    pgx.Tx
	stmts []string
}

func (r *recordingDBTX) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	r.stmts = append(r.stmts, sql)
	return r.tx.Exec(ctx, sql, args...)
}

func (r *recordingDBTX) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	r.stmts = append(r.stmts, sql)
	return r.tx.Query(ctx, sql, args...)
}

func (r *recordingDBTX) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	r.stmts = append(r.stmts, sql)
	return r.tx.QueryRow(ctx, sql, args...)
}

func issueParams(firm, run, inv uuid.UUID, n int) sqlc.TrackCInsertIssuesParams {
	p := sqlc.TrackCInsertIssuesParams{FirmID: firm, RunID: run, InvoiceID: inv}
	for i := range n {
		p.Seqs = append(p.Seqs, int32(i))
		p.RuleIds = append(p.RuleIds, fmt.Sprintf("ibr-%03d", i%1000))
		p.Severities = append(p.Severities, []string{"error", "warning"}[i%2])
		p.Paths = append(p.Paths, fmt.Sprintf("lines[%d].net_amount", i))
		p.BusinessTerms = append(p.BusinessTerms, "IBT-131")
		p.Messages = append(p.Messages, fmt.Sprintf("message %d", i))
		p.MessagesAr = append(p.MessagesAr, fmt.Sprintf("رسالة %d", i))
		p.MessageArgs = append(p.MessageArgs, fmt.Sprintf(`{"expected": "%d.50"}`, i))
		p.Fixables = append(p.Fixables, i%3 == 0)
		p.SuggestedValues = append(p.SuggestedValues, fmt.Sprintf("%d.50", i))
	}
	return p
}

// F1: COPY FROM is refused on a table with row-level security, so TrackCInsertIssues writes all of a
// run's issues with one INSERT ... SELECT unnest(...) statement.
func TestTrackCInsertIssuesUnnest(t *testing.T) {
	e := trackctest.Setup(t)
	ctx := context.Background()
	inv := e.SeedInvoice(t, e.FirmA, `{}`)
	run := e.SeedRun(t, e.FirmA, inv, "pint-ae@1.0.4+r1", 1, 1)
	const n = 250

	rec := &recordingDBTX{}
	if err := e.AppTx(ctx, e.FirmA, func(tx pgx.Tx) error {
		rec.tx = tx
		return sqlc.New(rec).TrackCInsertIssues(ctx, issueParams(e.FirmA, run, inv, n))
	}); err != nil {
		t.Fatal(err)
	}
	if len(rec.stmts) != 1 || !strings.Contains(rec.stmts[0], "INSERT INTO validation_issues") ||
		!strings.Contains(rec.stmts[0], "unnest(") || strings.Contains(strings.ToUpper(rec.stmts[0]), "COPY") {
		t.Fatalf("TrackCInsertIssues sent %d statements: %q", len(rec.stmts), rec.stmts)
	}

	var got []sqlc.TrackCListIssuesRow
	if err := db.WithFirm(ctx, e.App, e.FirmA, func(q *sqlc.Queries) error {
		var err error
		got, err = q.TrackCListIssues(ctx, run)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Fatalf("got %d issues, want %d", len(got), n)
	}
	want := issueParams(e.FirmA, run, inv, n)
	ids := map[uuid.UUID]bool{}
	for i, g := range got {
		ids[g.ID] = true
		var args map[string]string
		if err := json.Unmarshal(g.MessageArgs, &args); err != nil {
			t.Fatal(err)
		}
		if g.Seq != int32(i) || g.RuleID != want.RuleIds[i] || g.Severity != want.Severities[i] || g.Path != want.Paths[i] ||
			g.BusinessTerm != "IBT-131" || g.Message != want.Messages[i] || g.MessageAr != want.MessagesAr[i] ||
			args["expected"] != fmt.Sprintf("%d.50", i) || g.Fixable != want.Fixables[i] || g.SuggestedValue != want.SuggestedValues[i] {
			t.Fatalf("issue %d = %+v", i, g)
		}
	}
	if len(ids) != n {
		t.Fatalf("%d distinct ids for %d issues", len(ids), n)
	}

	// Parallel arrays of different lengths: the shorter one yields NULLs, which NOT NULL rejects.
	short := issueParams(e.FirmA, run, inv, 3)
	short.Seqs = []int32{1000, 1001, 1002}
	short.Paths = short.Paths[:2]
	err := db.WithFirm(ctx, e.App, e.FirmA, func(q *sqlc.Queries) error { return q.TrackCInsertIssues(ctx, short) })
	wantViolation(t, "arrays of different lengths", err, "23502", "")

	// The reason for F1, for both roles.
	for _, tx := range []func(context.Context, uuid.UUID, func(pgx.Tx) error) error{e.AppTx, e.OwnerTx} {
		err := tx(ctx, e.FirmA, func(tx pgx.Tx) error {
			_, err := tx.CopyFrom(ctx, pgx.Identifier{"validation_issues"},
				[]string{"id", "firm_id", "run_id", "invoice_id", "seq", "rule_id", "severity", "path", "message"},
				pgx.CopyFromRows([][]any{{uuid.New(), e.FirmA, run, inv, int32(5000), "ibr-001", "error", "p", "m"}}))
			return err
		})
		if pgCode(err) != "0A000" || !strings.Contains(err.Error(), "row-level security") {
			t.Fatalf("COPY FROM into validation_issues: got %v, want 0A000 (row-level security)", err)
		}
	}
}

func withFirm(t *testing.T, e trackctest.Env, firm uuid.UUID, fn func(q *sqlc.Queries) error) {
	t.Helper()
	if err := db.WithFirm(context.Background(), e.App, firm, fn); err != nil {
		t.Fatal(err)
	}
}

func TestTrackCInvoiceQueries(t *testing.T) {
	e := trackctest.Setup(t)
	ctx := context.Background()
	inv := e.SeedInvoice(t, e.FirmA, `{"invoice_number": "INV-001", "issue_date": "2026-01-02", "currency": "AED",
		"total_amount": "1050.00", "invoice_type_code": "380", "seller": {"name": "Seller LLC"}, "buyer": {"name": "Buyer"}}`)

	var g sqlc.TrackCGetInvoiceRow
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) { g, err = q.TrackCGetInvoice(ctx, inv); return })
	if g.ID != inv || g.Status != "uploaded" || g.PayloadVersion != 1 || g.LatestRunID != uuid.Nil ||
		g.ApprovedPayloadVersion.Valid || g.ApprovedBy.Valid || g.ApprovedAt.Valid || g.RulesetVersion.Valid {
		t.Fatalf("new invoice = %+v", g)
	}
	err := db.WithFirm(ctx, e.App, e.FirmB, func(q *sqlc.Queries) error { _, err := q.TrackCGetInvoice(ctx, inv); return err })
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("firm B TrackCGetInvoice: %v", err)
	}

	run := e.SeedRun(t, e.FirmA, inv, "pint-ae@1.0.4+r1", 0, 2)
	set := func(status string, version int32) int64 {
		var n int64
		withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) {
			if _, err = q.TrackCLockInvoice(ctx, inv); err != nil {
				return err
			}
			n, err = q.TrackCSetValidationResult(ctx, sqlc.TrackCSetValidationResultParams{
				Status: status, RunID: run, RulesetVersion: "pint-ae@1.0.4+r1", Issues: []byte(`[]`), ID: inv, PayloadVersion: version})
			return err
		})
		return n
	}
	if n := set("validated", 2); n != 0 {
		t.Fatalf("TrackCSetValidationResult at a stale payload_version updated %d rows", n)
	}
	if n := set("validated", 1); n != 1 {
		t.Fatalf("TrackCSetValidationResult updated %d rows", n)
	}

	approve := func(version int32) error {
		return db.WithFirm(ctx, e.App, e.FirmA, func(q *sqlc.Queries) error {
			_, err := q.TrackCApproveInvoice(ctx, sqlc.TrackCApproveInvoiceParams{ApprovedBy: "user-1", ID: inv, PayloadVersion: version})
			return err
		})
	}
	if err := approve(2); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("approve at a stale payload_version: %v", err)
	}
	if err := approve(1); err != nil {
		t.Fatal(err)
	}
	if err := approve(1); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("approve an invoice that is not validated: %v", err)
	}
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) { g, err = q.TrackCGetInvoice(ctx, inv); return })
	if g.Status != "ready" || g.LatestRunID != run || g.RulesetVersion.String != "pint-ae@1.0.4+r1" ||
		g.ApprovedPayloadVersion.Int32 != 1 || g.ApprovedBy.String != "user-1" || !g.ApprovedAt.Valid {
		t.Fatalf("approved invoice = %+v", g)
	}

	// A clean re-run keeps 'ready' and the approval; any other status clears it.
	if n := set("ready", 1); n != 1 {
		t.Fatal("re-run as ready")
	}
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) { g, err = q.TrackCGetInvoice(ctx, inv); return })
	if g.Status != "ready" || !g.ApprovedBy.Valid {
		t.Fatalf("ready re-run lost the approval: %+v", g)
	}
	if n := set("has_issues", 1); n != 1 {
		t.Fatal("re-run as has_issues")
	}
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) { g, err = q.TrackCGetInvoice(ctx, inv); return })
	if g.Status != "has_issues" || g.ApprovedPayloadVersion.Valid || g.ApprovedBy.Valid || g.ApprovedAt.Valid {
		t.Fatalf("has_issues kept the approval: %+v", g)
	}

	set("validated", 1)
	if err := approve(1); err != nil {
		t.Fatal(err)
	}
	var v int32
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) {
		v, err = q.TrackCApplyPayload(ctx, sqlc.TrackCApplyPayloadParams{Payload: []byte(`{"invoice_number": "INV-001b"}`),
			ID: inv, ExpectedPayloadVersion: 1})
		return
	})
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) { g, err = q.TrackCGetInvoice(ctx, inv); return })
	if v != 2 || g.PayloadVersion != 2 || g.Status != "fixed" || g.ApprovedBy.Valid || string(g.Payload) != `{"invoice_number": "INV-001b"}` {
		t.Fatalf("TrackCApplyPayload: version %d, invoice %+v", v, g)
	}
	err = db.WithFirm(ctx, e.App, e.FirmA, func(q *sqlc.Queries) error {
		_, err := q.TrackCApplyPayload(ctx, sqlc.TrackCApplyPayloadParams{Payload: []byte(`{}`), ID: inv, ExpectedPayloadVersion: 1})
		return err
	})
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("TrackCApplyPayload at a stale version: %v", err)
	}

	// Sweeper and revalidation listings.
	var fixed []sqlc.TrackCListFixedInvoicesRow
	future := pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}
	past := pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) {
		fixed, err = q.TrackCListFixedInvoices(ctx, sqlc.TrackCListFixedInvoicesParams{UpdatedBefore: future, MaxRows: 100})
		return
	})
	if len(fixed) != 1 || fixed[0].ID != inv || fixed[0].PayloadVersion != 2 {
		t.Fatalf("TrackCListFixedInvoices = %+v", fixed)
	}
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) {
		fixed, err = q.TrackCListFixedInvoices(ctx, sqlc.TrackCListFixedInvoicesParams{UpdatedBefore: past, MaxRows: 100})
		return
	})
	if len(fixed) != 0 {
		t.Fatalf("TrackCListFixedInvoices before MinAge = %+v", fixed)
	}

	var reval []sqlc.TrackCListInvoicesForRevalidationRow
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) {
		reval, err = q.TrackCListInvoicesForRevalidation(ctx, sqlc.TrackCListInvoicesForRevalidationParams{
			RulesetVersion: "pint-ae@1.0.4+r2", AfterID: uuid.Nil, MaxRows: 100})
		return
	})
	if len(reval) != 1 || reval[0].ID != inv || reval[0].RunID != run || reval[0].RulesetVersion != "pint-ae@1.0.4+r1" {
		t.Fatalf("TrackCListInvoicesForRevalidation = %+v", reval)
	}
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) {
		reval, err = q.TrackCListInvoicesForRevalidation(ctx, sqlc.TrackCListInvoicesForRevalidationParams{
			RulesetVersion: "pint-ae@1.0.4+r1", AfterID: uuid.Nil, MaxRows: 100})
		return
	})
	if len(reval) != 0 {
		t.Fatalf("invoices already on the RuleSet listed for revalidation: %+v", reval)
	}

	var firms []uuid.UUID
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) { firms, err = q.TrackCListFirmIDs(ctx); return })
	if !slices.Contains(firms, e.FirmA) || !slices.Contains(firms, e.FirmB) {
		t.Fatalf("TrackCListFirmIDs misses the seeded Firms: %v", firms)
	}
}

func TestTrackCListInvoices(t *testing.T) {
	e := trackctest.Setup(t)
	ctx := context.Background()
	var ids []uuid.UUID // oldest first
	for _, num := range []string{"A-100", "a-200", "B-300", "x%_1", "A-500"} {
		ids = append(ids, e.SeedInvoice(t, e.FirmA, `{"invoice_number": "`+num+`", "total_amount": "10.10"}`))
	}
	e.SeedInvoice(t, e.FirmB, `{"invoice_number": "A-999"}`)
	run := e.SeedRun(t, e.FirmA, ids[0], "r1", 3, 1)
	if err := appExec(t, e, e.FirmA, `UPDATE invoices SET latest_run_id = $2, status = 'has_issues' WHERE id = $1`, ids[0], run); err != nil {
		t.Fatal(err)
	}

	list := func(p sqlc.TrackCListInvoicesParams) []sqlc.TrackCListInvoicesRow {
		var rows []sqlc.TrackCListInvoicesRow
		if p.PageLimit == 0 {
			p.PageLimit = 100
		}
		withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) { rows, err = q.TrackCListInvoices(ctx, p); return })
		return rows
	}
	numbers := func(rows []sqlc.TrackCListInvoicesRow) string {
		var s []string
		for _, r := range rows {
			s = append(s, r.InvoiceNumber)
		}
		return strings.Join(s, ",")
	}

	all := list(sqlc.TrackCListInvoicesParams{})
	if got := numbers(all); got != "A-500,x%_1,B-300,a-200,A-100" {
		t.Fatalf("list = %s", got)
	}
	last := all[4]
	if last.ID != ids[0] || last.LatestRunID != run || last.ErrorCount != 3 || last.WarningCount != 1 ||
		last.TotalAmount != "10.10" || last.Status != "has_issues" || all[0].LatestRunID != uuid.Nil || all[0].ErrorCount != 0 {
		t.Fatalf("row = %+v / %+v", last, all[0])
	}
	if got := numbers(list(sqlc.TrackCListInvoicesParams{Q: pgtype.Text{String: "a-", Valid: true}})); got != "A-500,a-200,A-100" {
		t.Fatalf("q=a- = %s", got)
	}
	if got := numbers(list(sqlc.TrackCListInvoicesParams{Q: pgtype.Text{String: "%_", Valid: true}})); got != "x%_1" {
		t.Fatalf("q=%%_ (literal) = %s", got)
	}
	if got := numbers(list(sqlc.TrackCListInvoicesParams{Status: pgtype.Text{String: "has_issues", Valid: true}})); got != "A-100" {
		t.Fatalf("status=has_issues = %s", got)
	}
	page1 := list(sqlc.TrackCListInvoicesParams{PageLimit: 2})
	cur := page1[1]
	page2 := list(sqlc.TrackCListInvoicesParams{PageLimit: 2, BeforeCreatedAt: cur.CreatedAt, BeforeID: cur.ID})
	if numbers(page1) != "A-500,x%_1" || numbers(page2) != "B-300,a-200" {
		t.Fatalf("pages = %s | %s", numbers(page1), numbers(page2))
	}
}

func TestTrackCRunAuditExportFixTaskQueries(t *testing.T) {
	e := trackctest.Setup(t)
	ctx := context.Background()
	inv := e.SeedInvoice(t, e.FirmA, `{"invoice_number": "INV-7"}`)

	// Runs: newest first; the run before a run is TrackCListRuns with that run as the cursor.
	var runs []uuid.UUID
	for i := range 3 {
		runs = append(runs, e.SeedRun(t, e.FirmA, inv, "r1", int32(i), 0))
	}
	var listed []sqlc.TrackCListRunsRow
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) {
		listed, err = q.TrackCListRuns(ctx, sqlc.TrackCListRunsParams{InvoiceID: inv, PageLimit: 10})
		return
	})
	if len(listed) != 3 || listed[0].ID != runs[2] || listed[2].ID != runs[0] || listed[0].ErrorCount != 2 || listed[0].Trigger != "manual" {
		t.Fatalf("TrackCListRuns = %+v", listed)
	}
	var prev []sqlc.TrackCListRunsRow
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) {
		prev, err = q.TrackCListRuns(ctx, sqlc.TrackCListRunsParams{InvoiceID: inv, BeforeCreatedAt: listed[0].CreatedAt,
			BeforeID: listed[0].ID, PageLimit: 1})
		return
	})
	if len(prev) != 1 || prev[0].ID != runs[1] {
		t.Fatalf("previous run = %+v", prev)
	}
	var r sqlc.TrackCGetRunRow
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) {
		r, err = q.TrackCGetRun(ctx, sqlc.TrackCGetRunParams{ID: runs[1], InvoiceID: inv})
		return
	})
	if r.ID != runs[1] || r.ErrorCount != 1 || r.RulesetVersion != "r1" {
		t.Fatalf("TrackCGetRun = %+v", r)
	}
	other := e.SeedInvoice(t, e.FirmA, `{}`)
	for name, f := range map[string]uuid.UUID{"another invoice": e.FirmA, "another firm": e.FirmB} {
		invID := inv
		if name == "another invoice" {
			invID = other
		}
		err := db.WithFirm(ctx, e.App, f, func(q *sqlc.Queries) error {
			_, err := q.TrackCGetRun(ctx, sqlc.TrackCGetRunParams{ID: runs[1], InvoiceID: invID})
			return err
		})
		if !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("TrackCGetRun via %s: %v", name, err)
		}
	}

	// Audit events: uuid.Nil is NULL in both directions.
	proposal := uuid.New()
	insertAudit := func(p sqlc.TrackCInsertAuditEventParams) uuid.UUID {
		var row sqlc.TrackCInsertAuditEventRow
		withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) { row, err = q.TrackCInsertAuditEvent(ctx, p); return })
		return row.ID
	}
	a1 := insertAudit(sqlc.TrackCInsertAuditEventParams{FirmID: e.FirmA, ActorType: "system", ActorID: "api-go",
		Action: "invoice.approval_revoked", EntityType: "invoice", EntityID: inv, InvoiceID: inv, Changes: []byte(`[]`)})
	a2 := insertAudit(sqlc.TrackCInsertAuditEventParams{FirmID: e.FirmA, ActorType: "user", ActorID: "user-1", Agent: "fix",
		ProposalID: proposal, Action: "invoice.fields_changed", EntityType: "invoice", EntityID: inv, InvoiceID: inv,
		Changes: []byte(`[{"path": "seller_trn", "old_value": "1", "new_value": "2"}]`), Before: []byte(`{"status": "has_issues"}`),
		After: []byte(`{"status": "fixed"}`), Reason: "r", TraceID: "t"})
	insertAudit(sqlc.TrackCInsertAuditEventParams{FirmID: e.FirmA, ActorType: "user", ActorID: "user-1",
		Action: "proposal.rejected", EntityType: "proposal", EntityID: proposal, Changes: []byte(`[]`)})
	var nulls int
	if err := e.AppTx(ctx, e.FirmA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE firm_id = $1 AND proposal_id IS NULL
			AND (id = $2 OR invoice_id IS NULL) AND before IS NULL`, e.FirmA, a1).Scan(&nulls)
	}); err != nil {
		t.Fatal(err)
	}
	if nulls != 2 {
		t.Fatalf("%d audit events with NULL proposal_id/before, want 2", nulls)
	}
	var trail []sqlc.TrackCListInvoiceAuditRow
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) {
		trail, err = q.TrackCListInvoiceAudit(ctx, sqlc.TrackCListInvoiceAuditParams{InvoiceID: inv, PageLimit: 10})
		return
	})
	if len(trail) != 2 || trail[0].ID != a2 || trail[1].ID != a1 || trail[0].ProposalID != proposal || trail[1].ProposalID != uuid.Nil ||
		trail[0].Agent != "fix" || string(trail[0].After) != `{"status": "fixed"}` || trail[1].Before != nil {
		t.Fatalf("TrackCListInvoiceAudit = %+v", trail)
	}
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) {
		trail, err = q.TrackCListInvoiceAudit(ctx, sqlc.TrackCListInvoiceAuditParams{InvoiceID: inv,
			BeforeOccurredAt: trail[0].OccurredAt, BeforeID: trail[0].ID, PageLimit: 10})
		return
	})
	if len(trail) != 1 || trail[0].ID != a1 {
		t.Fatalf("audit page 2 = %+v", trail)
	}

	// Exports.
	exportID := uuid.New()
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) error {
		_, err := q.TrackCInsertExport(ctx, sqlc.TrackCInsertExportParams{ID: exportID, FirmID: e.FirmA, InvoiceID: inv,
			RunID: runs[0], PayloadVersion: 1, RulesetVersion: "r1", Format: "pint-ae-billing-1.0.4/ubl-2.1",
			DocumentKind: "invoice", ObjectKey: "firms/" + e.FirmA.String() + "/exports/" + exportID.String() + ".xml",
			Sha256: strings.Repeat("ab", 32), SizeBytes: 1234, CreatedBy: "user-1"})
		return err
	})
	var x sqlc.TrackCGetExportRow
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) { x, err = q.TrackCGetExport(ctx, exportID); return })
	if x.ID != exportID || x.InvoiceNumber != "INV-7" || x.SizeBytes != 1234 || x.RunID != runs[0] || !x.CreatedAt.Valid {
		t.Fatalf("TrackCGetExport = %+v", x)
	}
	var xs []sqlc.TrackCListExportsRow
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) { xs, err = q.TrackCListExports(ctx, inv); return })
	if len(xs) != 1 || xs[0].ID != exportID {
		t.Fatalf("TrackCListExports = %+v", xs)
	}
	err := db.WithFirm(ctx, e.App, e.FirmB, func(q *sqlc.Queries) error { _, err := q.TrackCGetExport(ctx, exportID); return err })
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("firm B TrackCGetExport: %v", err)
	}

	// Fix tasks: idempotent insert, publish, one completion, sweeper queries.
	task := uuid.New()
	insertTask := func(id, run uuid.UUID, mode string) int64 {
		var n int64
		withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) {
			n, err = q.TrackCInsertFixTask(ctx, sqlc.TrackCInsertFixTaskParams{ID: id, FirmID: e.FirmA, InvoiceID: inv,
				RunID: run, Mode: mode, RequestedBy: "user-1"})
			return
		})
		return n
	}
	if insertTask(task, runs[2], "auto") != 1 || insertTask(uuid.New(), runs[2], "auto") != 0 {
		t.Fatal("TrackCInsertFixTask is not idempotent on (run_id, mode)")
	}
	var ft sqlc.TrackCGetFixTaskByRunModeRow
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) {
		ft, err = q.TrackCGetFixTaskByRunMode(ctx, sqlc.TrackCGetFixTaskByRunModeParams{RunID: runs[2], Mode: "auto"})
		return
	})
	if ft.ID != task || ft.Status != "requested" || ft.ProposalID != uuid.Nil || ft.PublishedAt.Valid || ft.CompletedAt.Valid {
		t.Fatalf("TrackCGetFixTaskByRunMode = %+v", ft)
	}
	var unpublished []sqlc.TrackCListUnpublishedFixTasksRow
	future := pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) {
		unpublished, err = q.TrackCListUnpublishedFixTasks(ctx, sqlc.TrackCListUnpublishedFixTasksParams{RequestedBefore: future, MaxRows: 10})
		return
	})
	if len(unpublished) != 1 || unpublished[0].ID != task {
		t.Fatalf("TrackCListUnpublishedFixTasks = %+v", unpublished)
	}
	var n int64
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) { n, err = q.TrackCMarkFixTaskPublished(ctx, task); return })
	if n != 1 {
		t.Fatal("TrackCMarkFixTaskPublished")
	}
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) {
		unpublished, err = q.TrackCListUnpublishedFixTasks(ctx, sqlc.TrackCListUnpublishedFixTasksParams{RequestedBefore: future, MaxRows: 10})
		return
	})
	if len(unpublished) != 0 {
		t.Fatalf("published task still listed: %+v", unpublished)
	}
	proposalID, agentRun := uuid.New(), uuid.New()
	complete := func() int64 {
		var n int64
		withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) {
			n, err = q.TrackCCompleteFixTask(ctx, sqlc.TrackCCompleteFixTaskParams{Status: "succeeded", Outcome: "proposed",
				ProposalID: proposalID, AgentRunID: agentRun, ID: task})
			return
		})
		return n
	}
	if complete() != 1 || complete() != 0 {
		t.Fatal("TrackCCompleteFixTask must update exactly once")
	}
	var got sqlc.TrackCGetFixTaskRow
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) { got, err = q.TrackCGetFixTask(ctx, task); return })
	if got.Status != "succeeded" || got.Outcome != "proposed" || got.ProposalID != proposalID || got.AgentRunID != agentRun ||
		!got.PublishedAt.Valid || !got.CompletedAt.Valid || got.RequestedBy != "user-1" {
		t.Fatalf("completed task = %+v", got)
	}

	stale := uuid.New()
	insertTask(stale, runs[2], "on_demand")
	var latest sqlc.TrackCLatestFixTaskRow
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) { latest, err = q.TrackCLatestFixTask(ctx, inv); return })
	if latest.ID != stale {
		t.Fatalf("TrackCLatestFixTask = %+v", latest)
	}
	var timedOut []sqlc.TrackCTimeOutFixTasksRow
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) { timedOut, err = q.TrackCTimeOutFixTasks(ctx, future); return })
	if len(timedOut) != 1 || timedOut[0].ID != stale || timedOut[0].InvoiceID != inv {
		t.Fatalf("TrackCTimeOutFixTasks = %+v", timedOut)
	}
	withFirm(t, e, e.FirmA, func(q *sqlc.Queries) (err error) { got, err = q.TrackCGetFixTask(ctx, stale); return })
	if got.Status != "failed" || got.ErrorCode != "timeout" || !got.CompletedAt.Valid {
		t.Fatalf("timed-out task = %+v", got)
	}
	err = db.WithFirm(ctx, e.App, e.FirmB, func(q *sqlc.Queries) error { _, err := q.TrackCGetFixTask(ctx, task); return err })
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("firm B TrackCGetFixTask: %v", err)
	}
}

// The Down sections of 00030-00034 undo exactly Track C's schema, and Up restores it. Only versions
// above 29 are rolled back, so other tracks' migrations are untouched. The schema disappears for a
// moment, which is why integration tests run with -p 1 (one package at a time).
func TestTrackCMigrationsDownUp(t *testing.T) {
	trackctest.Setup(t)
	ctx := context.Background()
	sqldb, err := sql.Open("pgx", os.Getenv("TEST_OWNER_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqldb.Close() }()
	p, err := goose.NewProvider(goose.DialectPostgres, sqldb, os.DirFS("migrations"))
	if err != nil {
		t.Fatal(err)
	}
	present := func() (n int) {
		if err := sqldb.QueryRowContext(ctx, `SELECT
			(to_regclass('public.validation_runs') IS NOT NULL)::int + (to_regclass('public.validation_issues') IS NOT NULL)::int +
			(to_regclass('public.audit_events') IS NOT NULL)::int + (to_regclass('public.exports') IS NOT NULL)::int +
			(to_regclass('public.fix_tasks') IS NOT NULL)::int +
			(SELECT count(*)::int FROM pg_proc WHERE proname IN ('trackc_forbid_mutation', 'trackc_fix_tasks_guard')) +
			(SELECT count(*)::int FROM information_schema.columns WHERE table_name = 'invoices'
			   AND column_name IN ('payload_version', 'latest_run_id', 'approved_payload_version', 'approved_by', 'approved_at'))`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := present(); got != 12 {
		t.Fatalf("%d of 12 Track C objects before down", got)
	}
	if _, err := p.DownTo(ctx, 29); err != nil {
		t.Fatalf("down to 29: %v", err)
	}
	if got := present(); got != 0 {
		t.Fatalf("%d Track C objects left after down", got)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if got := present(); got != 12 {
		t.Fatalf("%d of 12 Track C objects after up", got)
	}
}
