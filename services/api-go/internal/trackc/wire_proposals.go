package trackc

import (
	"github.com/menem2024/uae-platform/services/api-go/internal/fixapply"
	"github.com/menem2024/uae-platform/services/api-go/internal/proposals"
)

// wireProposals builds the Registry that serves every proposal kind of /v1/proposals* (PC 6 gives the
// whole prefix to Track C): Track B's document.attribution and Track C's invoice.field_fix, each with
// its Applier, and mounts the routes. The Decide transaction runs the Applier and fixapply.AuditWriter
// together, so a decision and its audit events commit or roll back as one.
func wireProposals(m *Module) {
	reg := proposals.NewRegistry()
	reg.Register(proposals.KindDocumentAttribution, proposals.AttributionApplier{})
	reg.Register(fixapply.KindFieldFix, fixapply.Applier{})
	m.Proposals = reg
	m.Mounts = append(m.Mounts, proposalMount(m, reg))
}
