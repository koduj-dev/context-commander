package skills_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/identity"
	"github.com/koduj-dev/context-commander/internal/skills"
)

// person adds a user to the fixture's project with role and gives them a
// browser session and a token, returning principal, user, session and token
// credential ids.
func (f *fixture) person(role string) (principal, user, session, token uuid.UUID) {
	f.t.Helper()
	principal, user = uuid.New(), uuid.New()
	err := db.WithoutPrincipal(f.ctx, f.ownerPool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(f.ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, 'Person')`, user, user.String()+"@example.test"); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO principals (id, kind, user_id) VALUES ($1, 'user', $2)`, principal, user); err != nil {
			return err
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO project_members (project_id, principal_id, role) VALUES ($1, $2, $3)`, f.projectID, principal, role); err != nil {
			return err
		}
		st := identity.NewStore()
		_, s, err := st.IssueCredential(f.ctx, tx, principal, identity.SessionClientType, nil)
		if err != nil {
			return err
		}
		_, tk, err := st.IssueCredential(f.ctx, tx, principal, "cli", nil)
		session, token = s.ID, tk.ID
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return principal, user, session, token
}

func (f *fixture) trust(versionID uuid.UUID) string {
	f.t.Helper()
	v, err := f.svc.GetVersion(f.ctx, f.principalID, versionID)
	if err != nil {
		f.t.Fatal(err)
	}
	return v.Version.TrustStatus
}

func sign(priv ed25519.PrivateKey, name string, content []byte) []byte {
	sum := sha256.Sum256(content)
	return ed25519.Sign(priv, skills.SigningMessage(name, hex.EncodeToString(sum[:])))
}

// Signing: only an installation administrator registers keys; a signature
// from a registered key makes the version signed, any other is a refusal,
// and a revoked key signs nothing new.
func TestSignedSkillVersions(t *testing.T) {
	f := newFixture(t)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	if _, err := f.svc.AddSigner(f.ctx, f.principalID, f.userID, "release", pubB64); !errors.Is(err, skills.ErrForbidden) {
		t.Fatalf("a non-admin adding a signer: %v", err)
	}
	f.grantInstallationAdmin()
	if _, err := f.svc.AddSigner(f.ctx, f.principalID, f.userID, "bad", "c2hvcnQ="); !errors.Is(err, skills.ErrBadSigner) {
		t.Errorf("a short key: %v", err)
	}
	signer, err := f.svc.AddSigner(f.ctx, f.principalID, f.userID, "release", pubB64)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AddSigner(f.ctx, f.principalID, f.userID, "release", pubB64); !errors.Is(err, skills.ErrSignerExists) {
		t.Errorf("the same key twice: %v", err)
	}

	content := []byte("review every diff")
	v, err := f.svc.PublishSigned(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil,
		"code-review", "", "", content, nil, sign(priv, "code-review", content))
	if err != nil || v.TrustStatus != skills.TrustSigned || f.trust(v.ID) != skills.TrustSigned {
		t.Fatalf("signed publish: %+v, %v", v, err)
	}
	// A new version of the same skill is checked against the skill's name.
	v2, err := f.svc.PublishSigned(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, &v.SkillID,
		"ignored", "", "", []byte("v2"), nil, sign(priv, "code-review", []byte("v2")))
	if err != nil || v2.TrustStatus != skills.TrustSigned {
		t.Errorf("signed second version: %+v, %v", v2, err)
	}
	// Signed for another name, or over other content: refused, not stored.
	if _, err := f.svc.PublishSigned(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil,
		"other", "", "", content, nil, sign(priv, "code-review", content)); !errors.Is(err, skills.ErrBadSignature) {
		t.Errorf("a signature for another name: %v", err)
	}
	if _, err := f.svc.PublishSigned(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil,
		"code-review-2", "", "", []byte("tampered"), nil, sign(priv, "code-review-2", content)); !errors.Is(err, skills.ErrBadSignature) {
		t.Errorf("a signature over other content: %v", err)
	}

	if err := f.svc.RevokeSigner(f.ctx, f.principalID, f.userID, signer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.PublishSigned(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, &v.SkillID,
		"", "", "", []byte("v3"), nil, sign(priv, "code-review", []byte("v3"))); !errors.Is(err, skills.ErrBadSignature) {
		t.Errorf("a revoked key: %v", err)
	}
	if f.trust(v.ID) != skills.TrustSigned {
		t.Error("a version signed before the revocation lost its status")
	}
}

// Approval: a person in the interface, not the publisher, allowed for the
// skill's scope; and with the policy on, only trusted versions install.
func TestApprovingAndRequiringTrust(t *testing.T) {
	f := newFixture(t)
	v, err := f.svc.Publish(f.ctx, f.principalID, f.userID, skills.ScopeProject, &f.projectID, nil, "deploy", "", "", []byte("deploy carefully"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.trust(v.ID) != skills.TrustUnverified {
		t.Fatalf("a fresh version is %s", f.trust(v.ID))
	}

	owner, ownerUser, ownerSession, ownerToken := f.person("OWNER")
	contrib, contribUser, contribSession, _ := f.person("CONTRIBUTOR")

	if err := f.svc.Approve(f.ctx, owner, ownerUser, &ownerToken, v.ID, ""); !errors.Is(err, skills.ErrNeedsPerson) {
		t.Errorf("approving with a token: %v", err)
	}
	if err := f.svc.Approve(f.ctx, contrib, contribUser, &contribSession, v.ID, ""); !errors.Is(err, skills.ErrForbidden) {
		t.Errorf("a CONTRIBUTOR approving: %v", err)
	}

	// With the policy on, the unverified version does not install.
	f.svc.SetRequireTrust(true)
	if _, err := f.svc.Install(f.ctx, f.principalID, f.userID, f.projectID, skills.ScopeProject, &f.projectID, v.SkillID, v.ID, false, false); !errors.Is(err, skills.ErrUntrusted) {
		t.Errorf("installing an unverified version under the policy: %v", err)
	}

	if err := f.svc.Approve(f.ctx, owner, ownerUser, &ownerSession, v.ID, "read it, fine"); err != nil {
		t.Fatalf("an OWNER approving: %v", err)
	}
	if f.trust(v.ID) != skills.TrustApproved {
		t.Errorf("after approval: %s", f.trust(v.ID))
	}
	if err := f.svc.Approve(f.ctx, owner, ownerUser, &ownerSession, v.ID, ""); !errors.Is(err, skills.ErrAlreadyTrusted) {
		t.Errorf("approving twice: %v", err)
	}
	if _, err := f.svc.Install(f.ctx, f.principalID, f.userID, f.projectID, skills.ScopeProject, &f.projectID, v.SkillID, v.ID, false, false); err != nil {
		t.Errorf("installing an approved version: %v", err)
	}

	// The publisher cannot approve their own version.
	mine, err := f.svc.Publish(f.ctx, owner, ownerUser, skills.ScopeProject, &f.projectID, nil, "mine", "", "", []byte("x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Approve(f.ctx, owner, ownerUser, &ownerSession, mine.ID, ""); !errors.Is(err, skills.ErrSelfApproval) {
		t.Errorf("approving one's own version: %v", err)
	}
}
