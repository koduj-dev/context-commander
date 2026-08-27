// Package projects defines Namespace, Project, and project Membership —
// see docs/adr/0001-project-namespace-not-global-slug.md.
//
// Project.ID (a UUID) is the canonical identity used in every internal
// reference. (Namespace slug, Project slug) is only the human-readable
// identity, resolved via Resolve("filip/es-migration").
package projects

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type NamespaceKind string

const (
	NamespaceKindPersonal     NamespaceKind = "personal"
	NamespaceKindOrganization NamespaceKind = "organization"
)

type Namespace struct {
	ID             uuid.UUID
	Kind           NamespaceKind
	Slug           string
	OwnerUserID    *uuid.UUID
	OrganizationID *uuid.UUID
	CreatedAt      time.Time
}

// Visibility is discovery metadata only — see docs/adr/0006-*: it must
// never be read as an authorization signal. Project content access is
// decided exclusively by Membership (project_members / cc_has_project_access).
type Visibility string

const (
	VisibilityPrivate      Visibility = "private"
	VisibilityOrganization Visibility = "organization"
	VisibilityPublic       Visibility = "public"
)

type Project struct {
	ID          uuid.UUID
	NamespaceID uuid.UUID
	Slug        string
	Name        string
	Visibility  Visibility
	CreatedAt   time.Time
}

type Role string

const (
	RoleOwner       Role = "OWNER"
	RoleAdmin       Role = "ADMIN"
	RoleContributor Role = "CONTRIBUTOR"
	RoleReader      Role = "READER"
)

type Membership struct {
	ProjectID   uuid.UUID
	PrincipalID uuid.UUID
	Role        Role
	CreatedAt   time.Time
}

var (
	ErrNotFound           = errors.New("projects: not found")
	ErrInvalidResolvePath = errors.New(`projects: expected "namespace/project-slug"`)
)

type Store struct{}

func NewStore() *Store { return &Store{} }

func (s *Store) CreatePersonalNamespace(ctx context.Context, tx pgx.Tx, ownerUserID uuid.UUID, slug string) (Namespace, error) {
	return s.createNamespace(ctx, tx, `
		INSERT INTO namespaces (kind, slug, owner_user_id) VALUES ('personal', $1, $2)
		RETURNING id, kind, slug, owner_user_id, organization_id, created_at`,
		slug, ownerUserID)
}

func (s *Store) CreateOrganizationNamespace(ctx context.Context, tx pgx.Tx, organizationID uuid.UUID, slug string) (Namespace, error) {
	return s.createNamespace(ctx, tx, `
		INSERT INTO namespaces (kind, slug, organization_id) VALUES ('organization', $1, $2)
		RETURNING id, kind, slug, owner_user_id, organization_id, created_at`,
		slug, organizationID)
}

func (s *Store) createNamespace(ctx context.Context, tx pgx.Tx, sql, slug string, ref uuid.UUID) (Namespace, error) {
	var n Namespace
	err := tx.QueryRow(ctx, sql, slug, ref).
		Scan(&n.ID, &n.Kind, &n.Slug, &n.OwnerUserID, &n.OrganizationID, &n.CreatedAt)
	if err != nil {
		return Namespace{}, fmt.Errorf("projects: create namespace: %w", err)
	}
	return n, nil
}

func (s *Store) GetNamespaceBySlug(ctx context.Context, tx pgx.Tx, slug string) (Namespace, error) {
	var n Namespace
	err := tx.QueryRow(ctx,
		`SELECT id, kind, slug, owner_user_id, organization_id, created_at FROM namespaces WHERE slug = $1`,
		slug,
	).Scan(&n.ID, &n.Kind, &n.Slug, &n.OwnerUserID, &n.OrganizationID, &n.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Namespace{}, ErrNotFound
	}
	if err != nil {
		return Namespace{}, fmt.Errorf("projects: get namespace: %w", err)
	}
	return n, nil
}

// Create inserts a project directly into its target namespace. Namespace
// transfer (filip/foo -> acme/foo) is explicitly out of scope for MVP — see
// docs/adr/0001 — a project's namespace is fixed at creation.
func (s *Store) Create(ctx context.Context, tx pgx.Tx, namespaceID uuid.UUID, slug, name string) (Project, error) {
	var p Project
	err := tx.QueryRow(ctx,
		`INSERT INTO projects (namespace_id, slug, name) VALUES ($1, $2, $3)
		 RETURNING id, namespace_id, slug, name, visibility, created_at`,
		namespaceID, slug, name,
	).Scan(&p.ID, &p.NamespaceID, &p.Slug, &p.Name, &p.Visibility, &p.CreatedAt)
	if err != nil {
		return Project{}, fmt.Errorf("projects: create: %w", err)
	}
	return p, nil
}

func (s *Store) Get(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Project, error) {
	var p Project
	err := tx.QueryRow(ctx,
		`SELECT id, namespace_id, slug, name, visibility, created_at FROM projects WHERE id = $1`, id,
	).Scan(&p.ID, &p.NamespaceID, &p.Slug, &p.Name, &p.Visibility, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("projects: get: %w", err)
	}
	return p, nil
}

// Resolve looks up a project by its human-readable "<namespace>/<slug>"
// path (docs/adr/0001) — e.g. "filip/es-migration" vs. "acme/es-migration"
// coexisting without conflict.
func (s *Store) Resolve(ctx context.Context, tx pgx.Tx, path string) (Project, error) {
	namespaceSlug, projectSlug, ok := strings.Cut(path, "/")
	if !ok || namespaceSlug == "" || projectSlug == "" {
		return Project{}, ErrInvalidResolvePath
	}

	var p Project
	err := tx.QueryRow(ctx,
		`SELECT p.id, p.namespace_id, p.slug, p.name, p.visibility, p.created_at
		 FROM projects p JOIN namespaces n ON n.id = p.namespace_id
		 WHERE n.slug = $1 AND p.slug = $2`,
		namespaceSlug, projectSlug,
	).Scan(&p.ID, &p.NamespaceID, &p.Slug, &p.Name, &p.Visibility, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("projects: resolve %q: %w", path, err)
	}
	return p, nil
}

func (s *Store) AddMember(ctx context.Context, tx pgx.Tx, projectID, principalID uuid.UUID, role Role) (Membership, error) {
	var m Membership
	err := tx.QueryRow(ctx,
		`INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, $3)
		 RETURNING project_id, principal_id, role, created_at`,
		projectID, principalID, role,
	).Scan(&m.ProjectID, &m.PrincipalID, &m.Role, &m.CreatedAt)
	if err != nil {
		return Membership{}, fmt.Errorf("projects: add member: %w", err)
	}
	return m, nil
}

func (s *Store) GetMembership(ctx context.Context, tx pgx.Tx, projectID, principalID uuid.UUID) (Membership, error) {
	var m Membership
	err := tx.QueryRow(ctx,
		`SELECT project_id, principal_id, role, created_at FROM project_members
		 WHERE project_id = $1 AND principal_id = $2`,
		projectID, principalID,
	).Scan(&m.ProjectID, &m.PrincipalID, &m.Role, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Membership{}, ErrNotFound
	}
	if err != nil {
		return Membership{}, fmt.Errorf("projects: get membership: %w", err)
	}
	return m, nil
}
