package validation

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestNextStatus is the whole state machine of spec §5.6.1: one case per table row plus the edges
// between rows.
func TestNextStatus(t *testing.T) {
	all := []Trigger{TriggerExtracted, TriggerManual, TriggerFixAccepted, TriggerCorrection, TriggerSweeper, TriggerRulesetUpgrade}
	system := []Trigger{TriggerExtracted, TriggerSweeper, TriggerRulesetUpgrade}
	human := []Trigger{TriggerManual, TriggerFixAccepted, TriggerCorrection}
	statuses := []string{"uploaded", "extracted", "needs_review", "validated", "has_issues", "fixed", "ready"}

	// Row 1: needs_review is sticky for the system triggers, with or without errors.
	for _, tr := range system {
		for _, errs := range []int{0, 1, 7} {
			for _, ok := range []bool{false, true} {
				if got := NextStatus("needs_review", tr, errs, ok); got != "needs_review" {
					t.Errorf("NextStatus(needs_review, %s, %d, %v) = %s, want needs_review", tr, errs, ok, got)
				}
			}
		}
	}
	// A human leaves needs_review: errors -> has_issues, none -> validated.
	for _, tr := range human {
		if got := NextStatus("needs_review", tr, 0, false); got != "validated" {
			t.Errorf("NextStatus(needs_review, %s, 0) = %s, want validated", tr, got)
		}
		if got := NextStatus("needs_review", tr, 2, false); got != "has_issues" {
			t.Errorf("NextStatus(needs_review, %s, 2) = %s, want has_issues", tr, got)
		}
	}
	// Row 2: errors win over everything else.
	for _, tr := range all {
		for _, cur := range statuses {
			if cur == "needs_review" && contains(system, tr) {
				continue // row 1
			}
			for _, ok := range []bool{false, true} {
				if got := NextStatus(cur, tr, 1, ok); got != "has_issues" {
					t.Errorf("NextStatus(%s, %s, 1, %v) = %s, want has_issues", cur, tr, ok, got)
				}
			}
		}
	}
	// Row 3: ready with a matching approval and no errors stays ready, for any trigger.
	for _, tr := range all {
		if got := NextStatus("ready", tr, 0, true); got != "ready" {
			t.Errorf("NextStatus(ready, %s, 0, true) = %s, want ready", tr, got)
		}
		// ... but not without the approval.
		if got := NextStatus("ready", tr, 0, false); got != "validated" {
			t.Errorf("NextStatus(ready, %s, 0, false) = %s, want validated", tr, got)
		}
	}
	// Row 4: everything else with zero errors is validated (warnings never block).
	for _, tr := range all {
		for _, cur := range []string{"uploaded", "extracted", "validated", "has_issues", "fixed"} {
			for _, ok := range []bool{false, true} {
				if got := NextStatus(cur, tr, 0, ok); got != "validated" {
					t.Errorf("NextStatus(%s, %s, 0, %v) = %s, want validated", cur, tr, ok, got)
				}
			}
		}
	}
}

func contains(ts []Trigger, t Trigger) bool {
	for _, x := range ts {
		if x == t {
			return true
		}
	}
	return false
}

// TestTriggersMatchDatabaseCheck keeps the Go constants equal to the validation_runs.trigger CHECK.
func TestTriggersMatchDatabaseCheck(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "db", "migrations", "00031_validation_runs.sql"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`trigger\s+text NOT NULL CHECK \(trigger IN\s*\(([^)]*)\)`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("trigger CHECK not found in 00031")
	}
	want := map[string]bool{}
	for _, q := range regexp.MustCompile(`'([a-z_]+)'`).FindAllSubmatch(m[1], -1) {
		want[string(q[1])] = true
	}
	got := Triggers()
	if len(got) != len(want) {
		t.Fatalf("Triggers() = %v, CHECK has %v", got, want)
	}
	for _, tr := range got {
		if !want[string(tr)] {
			t.Errorf("trigger %q is not in the CHECK list", tr)
		}
	}
}

// TestNoFloatInTrackCPackages is the G2 grep test for the Track C Go packages that exist at this
// point (spec §3.6, plan Global Constraints): no float32/float64 in non-test sources.
func TestNoFloatInTrackCPackages(t *testing.T) {
	re := regexp.MustCompile(`\bfloat(32|64)\b`)
	for _, pkg := range []string{"validation", "fieldpath", "audit", "invoicefix", "exports", "fixtasks", "fixapply", "trackc"} {
		dir := filepath.Join("..", pkg)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // package not written yet
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || filepath.Ext(name) != ".go" || filepath.Base(name) != name || len(name) > 8 && name[len(name)-8:] == "_test.go" {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			if loc := re.FindIndex(raw); loc != nil {
				t.Errorf("%s/%s uses a float type at byte %d (G2: no float money)", pkg, name, loc[0])
			}
		}
	}
}
