package security

import (
	"context"

	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/auth"
)

// denyRecorder adapts Recorder to auth.DenyRecorder. It lives here rather
// than in internal/auth because the dependency only points this way:
// internal/security may import internal/auth, never the reverse.
type denyRecorder struct{ rec *Recorder }

// NewDenyRecorder returns the hook to pass to
// auth.AuthzService.SetDenyRecorder.
func NewDenyRecorder(rec *Recorder) auth.DenyRecorder { return denyRecorder{rec: rec} }

func (d denyRecorder) RecordDeny(ctx context.Context, principalID, projectID uuid.UUID, capability auth.Capability, reason string) {
	d.rec.Record(ctx, Event{
		Kind:        KindAuthorizationDenied,
		PrincipalID: &principalID,
		ProjectID:   &projectID,
		Capability:  string(capability),
		Reason:      reason,
	})
}
