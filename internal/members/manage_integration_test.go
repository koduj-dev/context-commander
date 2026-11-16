package members_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/members"
	"github.com/koduj-dev/context-commander/internal/projects"
)

func (f *fixture) roleOf(projectID, principalID uuid.UUID) projects.Role {
	f.t.Helper()
	var role projects.Role
	f.asOwner(func(tx pgx.Tx) error {
		m, err := f.projects.GetMembership(f.ctx, tx, projectID, principalID)
		if errors.Is(err, projects.ErrNotFound) {
			return nil
		}
		role = m.Role
		return err
	})
	return role
}

type team struct {
	project                             projects.Project
	owner, admin, contributor, outsider uuid.UUID
}

func newTeam(t *testing.T, f *fixture, ns string) team {
	t.Helper()
	owner := f.newUserPrincipal(ns + "-owner@example.test")
	admin := f.newUserPrincipal(ns + "-admin@example.test")
	contributor := f.newUserPrincipal(ns + "-contributor@example.test")
	outsider := f.newUserPrincipal(ns + "-outsider@example.test")
	project := f.newProject(*owner.UserID, ns, "proj")
	f.seedMember(project.ID, owner.ID, projects.RoleOwner)
	f.seedMember(project.ID, admin.ID, projects.RoleAdmin)
	f.seedMember(project.ID, contributor.ID, projects.RoleContributor)
	return team{project: project, owner: owner.ID, admin: admin.ID, contributor: contributor.ID, outsider: outsider.ID}
}

// Who may change whose role: member.manage for any change, OWNER for
// anything that grants or takes away OWNER, and never the last OWNER.
func TestSetRole(t *testing.T) {
	f := newFixture(t)
	tm := newTeam(t, f, "setrole")
	p := tm.project.ID

	if err := f.svc.SetRole(f.ctx, tm.contributor, p, tm.admin, projects.RoleReader); !errors.Is(err, members.ErrForbidden) {
		t.Errorf("CONTRIBUTOR changing a role: %v", err)
	}
	if err := f.svc.SetRole(f.ctx, tm.admin, p, tm.contributor, "BOSS"); !errors.Is(err, members.ErrInvalidRole) {
		t.Errorf("unknown role: %v", err)
	}
	if err := f.svc.SetRole(f.ctx, tm.admin, p, tm.outsider, projects.RoleReader); !errors.Is(err, members.ErrNotMember) {
		t.Errorf("a non-member: %v", err)
	}
	// ADMIN may move a CONTRIBUTOR, but neither crown nor dethrone anyone.
	if err := f.svc.SetRole(f.ctx, tm.admin, p, tm.contributor, projects.RoleReader); err != nil {
		t.Fatalf("ADMIN demoting a CONTRIBUTOR: %v", err)
	}
	if got := f.roleOf(p, tm.contributor); got != projects.RoleReader {
		t.Errorf("role after change = %s", got)
	}
	if err := f.svc.SetRole(f.ctx, tm.admin, p, tm.contributor, projects.RoleOwner); !errors.Is(err, members.ErrForbidden) {
		t.Errorf("ADMIN granting OWNER: %v", err)
	}
	if err := f.svc.SetRole(f.ctx, tm.admin, p, tm.owner, projects.RoleReader); !errors.Is(err, members.ErrForbidden) {
		t.Errorf("ADMIN demoting the OWNER: %v", err)
	}
	if got := f.roleOf(p, tm.owner); got != projects.RoleOwner {
		t.Errorf("OWNER after a refused demotion = %s", got)
	}

	// The last OWNER stays one, even demoting themselves.
	if err := f.svc.SetRole(f.ctx, tm.owner, p, tm.owner, projects.RoleAdmin); !errors.Is(err, members.ErrLastOwner) {
		t.Errorf("last OWNER stepping down: %v", err)
	}
	// With a second OWNER, the first may step down.
	if err := f.svc.SetRole(f.ctx, tm.owner, p, tm.admin, projects.RoleOwner); err != nil {
		t.Fatalf("OWNER granting OWNER: %v", err)
	}
	if err := f.svc.SetRole(f.ctx, tm.owner, p, tm.owner, projects.RoleAdmin); err != nil {
		t.Fatalf("an OWNER stepping down with another left: %v", err)
	}
	// The same role again is not an error.
	if err := f.svc.SetRole(f.ctx, tm.admin, p, tm.admin, projects.RoleOwner); err != nil {
		t.Errorf("setting the role a member has: %v", err)
	}
}

// Two OWNERs demoting each other at the same moment: one wins, the project
// keeps an OWNER.
func TestSetRoleKeepsAnOwnerUnderRace(t *testing.T) {
	f := newFixture(t)
	tm := newTeam(t, f, "race")
	p := tm.project.ID
	f.asOwner(func(tx pgx.Tx) error {
		return f.projects.SetMemberRole(f.ctx, tx, p, tm.admin, projects.RoleOwner)
	})

	for range 10 {
		var wg sync.WaitGroup
		errs := make([]error, 2)
		pairs := [][2]uuid.UUID{{tm.owner, tm.admin}, {tm.admin, tm.owner}}
		for i, pair := range pairs {
			wg.Go(func() { errs[i] = f.svc.SetRole(f.ctx, pair[0], p, pair[1], projects.RoleAdmin) })
		}
		wg.Wait()
		owners := 0
		for _, who := range []uuid.UUID{tm.owner, tm.admin} {
			if f.roleOf(p, who) == projects.RoleOwner {
				owners++
			}
		}
		if owners != 1 {
			t.Fatalf("after a race: %d owners, errors %v", owners, errs)
		}
		// Restore two owners for the next round.
		f.asOwner(func(tx pgx.Tx) error {
			if err := f.projects.SetMemberRole(f.ctx, tx, p, tm.owner, projects.RoleOwner); err != nil {
				return err
			}
			return f.projects.SetMemberRole(f.ctx, tx, p, tm.admin, projects.RoleOwner)
		})
	}
}

// Removing: member.manage to remove someone else, OWNER to remove an OWNER,
// any member may leave, the last OWNER may not, and the presence goes too.
func TestRemove(t *testing.T) {
	f := newFixture(t)
	tm := newTeam(t, f, "remove")
	p := tm.project.ID
	f.asOwner(func(tx pgx.Tx) error {
		_, cred, err := f.identity.IssueCredential(f.ctx, tx, tm.contributor, "claude-code", nil)
		if err != nil {
			return err
		}
		_, err = tx.Exec(f.ctx,
			`INSERT INTO work_presence (client_id, principal_id, project_id) VALUES ($1, $2, $3)`,
			cred.ID, tm.contributor, p)
		return err
	})

	if err := f.svc.Remove(f.ctx, tm.contributor, p, tm.admin); !errors.Is(err, members.ErrForbidden) {
		t.Errorf("CONTRIBUTOR removing ADMIN: %v", err)
	}
	if err := f.svc.Remove(f.ctx, tm.admin, p, tm.owner); !errors.Is(err, members.ErrForbidden) {
		t.Errorf("ADMIN removing the OWNER: %v", err)
	}
	if err := f.svc.Remove(f.ctx, tm.owner, p, tm.owner); !errors.Is(err, members.ErrLastOwner) {
		t.Errorf("the last OWNER leaving: %v", err)
	}
	if err := f.svc.Remove(f.ctx, tm.admin, p, tm.outsider); !errors.Is(err, members.ErrNotMember) {
		t.Errorf("removing a non-member: %v", err)
	}

	if err := f.svc.Remove(f.ctx, tm.admin, p, tm.contributor); err != nil {
		t.Fatalf("ADMIN removing a CONTRIBUTOR: %v", err)
	}
	if got := f.roleOf(p, tm.contributor); got != "" {
		t.Errorf("still a member as %s", got)
	}
	var presence int
	f.asOwner(func(tx pgx.Tx) error {
		return tx.QueryRow(f.ctx, `SELECT count(*) FROM work_presence WHERE project_id = $1 AND principal_id = $2`,
			p, tm.contributor).Scan(&presence)
	})
	if presence != 0 {
		t.Errorf("%d presence rows survived the removal", presence)
	}
	// Gone means gone: the removed member no longer sees the list.
	if _, err := f.svc.List(f.ctx, tm.contributor, p); !errors.Is(err, members.ErrForbidden) {
		t.Errorf("a removed member listing members: %v", err)
	}

	// An ADMIN may leave without member.manage being the reason.
	if err := f.svc.Remove(f.ctx, tm.admin, p, tm.admin); err != nil {
		t.Fatalf("ADMIN leaving: %v", err)
	}
	var actions []string
	f.asOwner(func(tx pgx.Tx) error {
		rows, err := tx.Query(f.ctx, `SELECT action FROM audit_log WHERE project_id = $1 ORDER BY id`, p)
		if err != nil {
			return err
		}
		actions, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if len(actions) != 2 || actions[0] != "member.remove" || actions[1] != "member.leave" {
		t.Errorf("audit actions = %v", actions)
	}
}
