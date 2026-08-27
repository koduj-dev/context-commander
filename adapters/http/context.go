package http

import (
	"context"

	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/projects"
)

type ctxKey int

const (
	ctxKeyPrincipalID ctxKey = iota
	ctxKeyClientID
	ctxKeyProject
)

func principalIDFrom(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(ctxKeyPrincipalID).(uuid.UUID)
	return id
}

func clientIDFrom(ctx context.Context) *uuid.UUID {
	id, ok := ctx.Value(ctxKeyClientID).(uuid.UUID)
	if !ok {
		return nil
	}
	return &id
}

func projectFrom(ctx context.Context) projects.Project {
	p, _ := ctx.Value(ctxKeyProject).(projects.Project)
	return p
}
