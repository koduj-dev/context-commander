package http

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/koduj-dev/context-commander/internal/contextpack"
)

// getContextPack serves the aggregated bootstrap read model — NEXT.md
// Phase 1's "one well-described operation that assembles the starting
// context".
//
// GET, not POST, despite taking several parameters: it is a pure read with
// no side effects, and an agent that can't re-issue it safely on a retry
// would have to reason about whether its own bootstrap changed anything.
// The parameters are therefore query arguments, which keeps the whole call
// expressible as a URL a human can paste into curl while debugging what a
// client actually received.
func (d Deps) getContextPack(w http.ResponseWriter, r *http.Request) {
	project := projectFrom(r.Context())
	principalID := principalIDFrom(r.Context())

	opts := contextpack.Options{}
	q := r.URL.Query()

	if raw := q.Get("budget"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "?budget= must be an integer number of bytes")
			return
		}
		opts.Budget = v
	}
	if raw := q.Get("event_limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "?event_limit= must be an integer")
			return
		}
		opts.EventLimit = v
	}
	// Repeated ?artifact_path= rather than one comma-separated value:
	// logical paths may contain commas, and a separator a caller has to
	// escape is a separator that will eventually be wrong.
	opts.ArtifactPaths = q["artifact_path"]
	opts.SkipSkills = boolParam(q, "skip_skills")
	opts.SkipArtifacts = boolParam(q, "skip_artifacts")

	pack, err := d.ContextPack.Bootstrap(r.Context(), principalID, project.ID, opts)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	// The pack's own JSON tags are the wire format: it is already a read
	// model shaped for a consumer, and a second adapter-level struct
	// mirroring it field for field would only be one more place for the
	// two to drift.
	writeJSON(w, http.StatusOK, pack)
}

// boolParam treats a bare flag (?skip_skills, no value) as true, which is
// what a caller hand-writing the URL expects — hence the presence check
// before the value check: an absent key and a key with an empty value mean
// opposite things here, and q.Get alone cannot tell them apart.
func boolParam(q url.Values, key string) bool {
	if !q.Has(key) {
		return false
	}
	switch strings.ToLower(q.Get(key)) {
	case "0", "false", "no":
		return false
	default:
		return true
	}
}
