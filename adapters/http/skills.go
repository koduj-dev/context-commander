package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/skills"
)

type skillVersionResponse struct {
	ID                   uuid.UUID `json:"id"`
	SkillID              uuid.UUID `json:"skill_id"`
	Version              int       `json:"version"`
	Checksum             string    `json:"checksum"`
	DeclaredCapabilities []string  `json:"declared_capabilities"`
	TrustStatus          string    `json:"trust_status"`
}

type skillBindingResponse struct {
	ID             uuid.UUID  `json:"id"`
	Scope          string     `json:"scope"`
	ScopeRef       *uuid.UUID `json:"scope_ref,omitempty"`
	SkillID        uuid.UUID  `json:"skill_id"`
	SkillVersionID uuid.UUID  `json:"skill_version_id"`
	Pinned         bool       `json:"pinned"`
	Managed        bool       `json:"managed"`
	Overridable    bool       `json:"overridable"`
}

func toSkillVersionResponse(v skills.SkillVersion) skillVersionResponse {
	return skillVersionResponse{
		ID: v.ID, SkillID: v.SkillID, Version: v.Version, Checksum: v.Checksum,
		DeclaredCapabilities: v.DeclaredCapabilities, TrustStatus: v.TrustStatus,
	}
}

func toSkillBindingResponse(b skills.SkillBinding) skillBindingResponse {
	return skillBindingResponse{
		ID: b.ID, Scope: string(b.Scope), ScopeRef: b.ScopeRef,
		SkillID: b.SkillID, SkillVersionID: b.SkillVersionID,
		Pinned: b.Pinned, Managed: b.Managed, Overridable: b.Overridable,
	}
}

// publishSkill puts a package into a scope. It is a top-level registry
// operation, not a project one: an installation admin publishing an
// instance-wide rule may belong to no project at all (docs/adr/0017,
// docs/adr/0018), so demanding a project in the path would have excluded
// exactly the caller a self-hosted instance most wants publishing.
func (d Deps) publishSkill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Scope                string     `json:"scope"`
		ScopeRef             *uuid.UUID `json:"scope_ref"`
		SkillID              *uuid.UUID `json:"skill_id"`
		Name                 string     `json:"name"`
		Author               string     `json:"author"`
		Origin               string     `json:"origin"`
		Content              string     `json:"content"`
		DeclaredCapabilities []string   `json:"declared_capabilities"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}
	if body.Scope == "" {
		body.Scope = string(skills.ScopeProject)
	}

	principalID := principalIDFrom(r.Context())
	userID, err := resolveUserID(r.Context(), d.Pool, principalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}

	v, err := d.Skills.Publish(r.Context(), principalID, userID, skills.Scope(body.Scope), body.ScopeRef,
		body.SkillID, body.Name, body.Author, body.Origin, []byte(body.Content), body.DeclaredCapabilities)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSkillVersionResponse(v))
}

func (d Deps) installSkill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Scope          string     `json:"scope"`
		ScopeRef       *uuid.UUID `json:"scope_ref"`
		SkillID        uuid.UUID  `json:"skill_id"`
		SkillVersionID uuid.UUID  `json:"skill_version_id"`
		Managed        bool       `json:"managed"`
		Overridable    bool       `json:"overridable"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())
	userID, err := resolveUserID(r.Context(), d.Pool, principalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}

	b, err := d.Skills.Install(r.Context(), principalID, userID, project.ID, skills.Scope(body.Scope), body.ScopeRef, body.SkillID, body.SkillVersionID, body.Managed, body.Overridable)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSkillBindingResponse(b))
}

func (d Deps) overrideSkill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SkillID        uuid.UUID `json:"skill_id"`
		SkillVersionID uuid.UUID `json:"skill_version_id"`
		Managed        bool      `json:"managed"`
		Overridable    bool      `json:"overridable"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	b, err := d.Skills.Override(r.Context(), principalID, project.ID, body.SkillID, body.SkillVersionID, body.Managed, body.Overridable)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSkillBindingResponse(b))
}

func (d Deps) pinSkillBinding(w http.ResponseWriter, r *http.Request) {
	d.setSkillPinned(w, r, true)
}

func (d Deps) unpinSkillBinding(w http.ResponseWriter, r *http.Request) {
	d.setSkillPinned(w, r, false)
}

func (d Deps) setSkillPinned(w http.ResponseWriter, r *http.Request, pinned bool) {
	var body struct {
		BindingID uuid.UUID `json:"binding_id"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	var err error
	if pinned {
		err = d.Skills.Pin(r.Context(), principalID, project.ID, body.BindingID)
	} else {
		err = d.Skills.Unpin(r.Context(), principalID, project.ID, body.BindingID)
	}
	if err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) listEffectiveSkills(w http.ResponseWriter, r *http.Request) {
	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	list, err := d.Skills.ListEffective(r.Context(), principalID, project.ID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	out := make([]skillBindingResponse, len(list))
	for i, b := range list {
		out[i] = toSkillBindingResponse(b)
	}
	writeJSON(w, http.StatusOK, out)
}

// skillContentResponse is skillVersionResponse plus what an agent actually
// needs to load the thing: the skill's name and its packaged bytes.
//
// Content travels as a JSON string, not a binary download: a skill package
// is instruction text, every other endpoint in this API is JSON, and a
// client that has to branch on content type to read a skill is a client
// that will get it wrong.
type skillContentResponse struct {
	skillVersionResponse
	SkillName   string `json:"skill_name"`
	SkillAuthor string `json:"skill_author,omitempty"`
	SkillOrigin string `json:"skill_origin,omitempty"`
	Content     string `json:"content"`
}

// getSkillVersion closes the gap NEXT.md calls the most glaring hole in the
// surface: until now every adapter could install a skill and none could
// read one back.
func (d Deps) getSkillVersion(w http.ResponseWriter, r *http.Request) {
	skillVersionID, err := uuid.Parse(chi.URLParam(r, "skillVersionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "skillVersionID must be a UUID")
		return
	}

	principalID := principalIDFrom(r.Context())

	v, err := d.Skills.GetVersion(r.Context(), principalID, skillVersionID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, skillContentResponse{
		skillVersionResponse: toSkillVersionResponse(v.Version),
		SkillName:            v.Skill.Name,
		SkillAuthor:          v.Skill.Author,
		SkillOrigin:          v.Skill.Origin,
		Content:              string(v.Content),
	})
}

type catalogEntryResponse struct {
	SkillID  string     `json:"skill_id"`
	Name     string     `json:"name"`
	Author   string     `json:"author,omitempty"`
	Origin   string     `json:"origin,omitempty"`
	Scope    string     `json:"scope"`
	ScopeRef *uuid.UUID `json:"scope_ref,omitempty"`

	LatestVersionID string `json:"latest_version_id"`
	LatestVersion   int    `json:"latest_version"`
	LatestChecksum  string `json:"latest_checksum"`
	TrustStatus     string `json:"trust_status"`
}

// searchSkills browses the registry. ?q= matches names case-insensitively;
// what comes back is what the caller may see (docs/adr/0018), which is not
// limited to the project in the path — that only proves membership.
func (d Deps) searchSkills(w http.ResponseWriter, r *http.Request) {
	principalID := principalIDFrom(r.Context())

	opts, ok := pagingFrom(w, r)
	if !ok {
		return
	}
	page, err := d.Skills.Search(r.Context(), principalID, r.URL.Query().Get("q"), opts)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	out := make([]catalogEntryResponse, len(page.Items))
	for i, e := range page.Items {
		out[i] = catalogEntryResponse{
			SkillID: e.SkillID.String(), Name: e.Name, Author: e.Author, Origin: e.Origin,
			Scope: string(e.Scope), ScopeRef: e.ScopeRef,
			LatestVersionID: e.LatestVersionID.String(), LatestVersion: e.LatestVersion,
			LatestChecksum: e.LatestChecksum, TrustStatus: e.TrustStatus,
		}
	}
	writeList(w, out, page.NextCursor)
}

func (d Deps) listSkillVersions(w http.ResponseWriter, r *http.Request) {
	skillID, err := uuid.Parse(chi.URLParam(r, "skillID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "skillID must be a UUID")
		return
	}

	principalID := principalIDFrom(r.Context())

	opts, ok := pagingFrom(w, r)
	if !ok {
		return
	}
	page, err := d.Skills.ListVersions(r.Context(), principalID, skillID, opts)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	out := make([]skillVersionResponse, len(page.Items))
	for i, v := range page.Items {
		out[i] = toSkillVersionResponse(v)
	}
	writeList(w, out, page.NextCursor)
}

// promoteSkill widens where a skill lives. A POST on the skill rather than a
// PATCH of a "scope" field: it is a one-way, authorized move, not an
// assignable attribute (see internal/skills.Service.Promote).
func (d Deps) promoteSkill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SkillID  uuid.UUID  `json:"skill_id"`
		Scope    string     `json:"scope"`
		ScopeRef *uuid.UUID `json:"scope_ref"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}

	principalID := principalIDFrom(r.Context())
	userID, err := resolveUserID(r.Context(), d.Pool, principalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}

	sk, err := d.Skills.Promote(r.Context(), principalID, userID, body.SkillID, skills.Scope(body.Scope), body.ScopeRef)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"skill_id": sk.ID.String(), "scope": string(sk.Scope), "scope_ref": sk.ScopeRef,
	})
}
