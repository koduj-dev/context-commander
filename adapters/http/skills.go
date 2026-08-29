package http

import (
	"encoding/json"
	"io"
	"net/http"

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

func (d Deps) publishSkill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SkillID              *uuid.UUID `json:"skill_id"`
		Name                 string     `json:"name"`
		Author               string     `json:"author"`
		Origin               string     `json:"origin"`
		Content              string     `json:"content"`
		DeclaredCapabilities []string   `json:"declared_capabilities"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "could not decode request body")
		return
	}

	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	v, err := d.Skills.Publish(r.Context(), principalID, project.ID, body.SkillID, body.Name, body.Author, body.Origin, []byte(body.Content), body.DeclaredCapabilities)
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
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
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
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
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
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
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

	var orgID *uuid.UUID
	if raw := r.URL.Query().Get("organization_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "?organization_id= must be a UUID")
			return
		}
		orgID = &id
	}

	list, err := d.Skills.ListEffective(r.Context(), principalID, project.ID, orgID)
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

// The following case block still needs to be merged into errors.go's
// writeDomainError switch (a shared file this fork does not edit):
//
//	case errors.Is(err, skills.ErrNotFound):
//		writeError(w, http.StatusNotFound, "not_found", "not found")
//	case errors.Is(err, skills.ErrForbidden):
//		writeError(w, http.StatusForbidden, "forbidden", "forbidden")
//	case errors.Is(err, skills.ErrNotOverridable):
//		writeError(w, http.StatusConflict, "conflict", "shadowed binding is not overridable")
//	case errors.Is(err, skills.ErrUnsupportedScope):
//		writeError(w, http.StatusBadRequest, "invalid_request", "installation-scope bindings are operator-seeded only")
