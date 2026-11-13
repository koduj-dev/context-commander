package mcp

import (
	"context"
	"time"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/koduj-dev/context-commander/internal/memories"
)

// Memories (docs/adr/0025). What an agent learned about the project and
// wants the next agent — its own next session, or a colleague's — to know,
// tagged with the branch, the ticket and who solved it.

type memoryResult struct {
	ID        string   `json:"id"`
	Project   string   `json:"project,omitempty"`
	Kind      string   `json:"kind"`
	Status    string   `json:"status"`
	Version   int      `json:"version"`
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	Branch    string   `json:"branch,omitempty"`
	TicketKey string   `json:"ticket_key,omitempty"`
	TicketURL string   `json:"ticket_url,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Assignee  string   `json:"assignee,omitempty"`
	// AssignedToYou is what an agent should branch on: a principal id alone
	// does not say whether this session is the one meant.
	AssignedToYou bool   `json:"assigned_to_you,omitempty"`
	AssigneeName  string `json:"assignee_name,omitempty"`
	AuthorName    string `json:"author_name,omitempty"`
	PlanID        string `json:"plan_id,omitempty"`
	PlanItemID    string `json:"plan_item_id,omitempty"`
	SupersededBy  string `json:"superseded_by,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

func toMemory(m memories.Memory, principalID uuid.UUID, people map[uuid.UUID]memories.Person) memoryResult {
	r := memoryResult{
		ID: m.ID.String(), Kind: m.Kind, Status: m.Status, Version: m.Version, Title: m.Title, Body: m.Body,
		Branch: m.Branch, TicketKey: m.TicketKey, TicketURL: m.TicketURL, Tags: m.Tags,
		AuthorName: people[m.CreatedBy].Name,
		CreatedAt:  m.CreatedAt.Format(time.RFC3339), UpdatedAt: m.UpdatedAt.Format(time.RFC3339),
	}
	if m.Assignee != nil {
		r.Assignee, r.AssignedToYou, r.AssigneeName = m.Assignee.String(), *m.Assignee == principalID, people[*m.Assignee].Name
	}
	str := func(id *uuid.UUID) string {
		if id == nil {
			return ""
		}
		return id.String()
	}
	r.PlanID, r.PlanItemID, r.SupersededBy = str(m.PlanID), str(m.PlanItemID), str(m.SupersededBy)
	return r
}

// assigneeArg reads the "me" / "none" / principal-id convention plan tools
// use for an assignee.
func assigneeArg(s string, principalID uuid.UUID) (set bool, id *uuid.UUID, err error) {
	switch s {
	case "":
		return false, nil, nil
	case "me":
		me := principalID
		return true, &me, nil
	case "none":
		return true, nil, nil
	}
	a, err := uuid.Parse(s)
	if err != nil {
		return false, nil, err
	}
	return true, &a, nil
}

func registerMemoryTools(server *sdkmcp.Server, deps Deps, principalID, clientID uuid.UUID) {
	resolve := projectResolver(deps, principalID)

	type createArgs struct {
		Project    string   `json:"project" jsonschema:"project path \"namespace/slug\""`
		Kind       string   `json:"kind,omitempty" jsonschema:"decision, finding, gotcha, note (default), status or other"`
		Title      string   `json:"title" jsonschema:"one line a colleague would search for: what was decided or found"`
		Body       string   `json:"body,omitempty" jsonschema:"the detail, in markdown: why, evidence, how to reproduce, what to do instead"`
		Branch     string   `json:"branch,omitempty" jsonschema:"the git branch the work happened on (git branch --show-current); fill it whenever you know it"`
		TicketKey  string   `json:"ticket_key,omitempty" jsonschema:"the ticket in the team's tracker, e.g. PROJ-123; fill it whenever the task names one"`
		TicketURL  string   `json:"ticket_url,omitempty" jsonschema:"absolute http(s) link to that ticket"`
		Tags       []string `json:"tags,omitempty" jsonschema:"up to 20 short labels, stored lowercased, e.g. billing, migration"`
		Assignee   string   `json:"assignee,omitempty" jsonschema:"who solved or owns this: \"me\", or a member's principal ID; omitted means nobody in particular"`
		PlanID     string   `json:"plan_id,omitempty" jsonschema:"the plan this came out of, if any"`
		PlanItemID string   `json:"plan_item_id,omitempty" jsonschema:"the plan step this came out of, if any"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "memory.create",
		Description: "Write down something the next agent or colleague on this project should know, so it is not lost " +
			"with your conversation. Write one when you make or learn a decision, find a non-obvious cause, hit a gotcha " +
			"(something that looks right and is not), or reach a status worth handing on — not for routine progress, " +
			"which belongs in plan.update_item. Fill branch and ticket_key/ticket_url whenever you know them: they are " +
			"how the team filters memories. Search first (memory.search) and supersede an outdated memory rather than " +
			"contradicting it with a second one.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args createArgs) (*sdkmcp.CallToolResult, memoryResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, memoryResult{}, err
		}
		_, assignee, err := assigneeArg(args.Assignee, principalID)
		if err != nil {
			return nil, memoryResult{}, err
		}
		planID, err := parseOptionalUUID(args.PlanID)
		if err != nil {
			return nil, memoryResult{}, err
		}
		itemID, err := parseOptionalUUID(args.PlanItemID)
		if err != nil {
			return nil, memoryResult{}, err
		}
		m, err := deps.Memories.Create(ctx, principalID, &clientID, projectID, memories.NewMemory{
			Kind: args.Kind, Title: args.Title, Body: args.Body, Branch: args.Branch,
			TicketKey: args.TicketKey, TicketURL: args.TicketURL, Tags: args.Tags,
			Assignee: assignee, PlanID: planID, PlanItemID: itemID,
		})
		if err != nil {
			return nil, memoryResult{}, err
		}
		return nil, toMemory(m, principalID, nil), nil
	})

	type getArgs struct {
		Project  string `json:"project" jsonschema:"project path \"namespace/slug\""`
		MemoryID string `json:"memory_id" jsonschema:"memory ID"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "memory.get",
		Description: "One memory with its whole body and metadata. Search and list return titles and snippets; this returns the rest.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args getArgs) (*sdkmcp.CallToolResult, memoryResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, memoryResult{}, err
		}
		id, err := uuid.Parse(args.MemoryID)
		if err != nil {
			return nil, memoryResult{}, err
		}
		m, err := deps.Memories.Get(ctx, principalID, projectID, id)
		if err != nil {
			return nil, memoryResult{}, err
		}
		people, err := deps.Memories.People(ctx, principalID, []memories.Memory{m})
		if err != nil {
			return nil, memoryResult{}, err
		}
		return nil, toMemory(m, principalID, people), nil
	})

	type updateArgs struct {
		Project         string    `json:"project" jsonschema:"project path \"namespace/slug\""`
		MemoryID        string    `json:"memory_id" jsonschema:"memory ID"`
		ExpectedVersion int       `json:"expected_version" jsonschema:"the version you read; a stale one is refused so you do not overwrite a colleague's edit"`
		Kind            *string   `json:"kind,omitempty" jsonschema:"decision, finding, gotcha, note, status or other"`
		Title           *string   `json:"title,omitempty" jsonschema:"replace the title"`
		Body            *string   `json:"body,omitempty" jsonschema:"replace the body"`
		Branch          *string   `json:"branch,omitempty" jsonschema:"replace the branch; empty string clears it"`
		TicketKey       *string   `json:"ticket_key,omitempty" jsonschema:"replace the ticket key; empty string clears it"`
		TicketURL       *string   `json:"ticket_url,omitempty" jsonschema:"replace the ticket link; empty string clears it"`
		Tags            *[]string `json:"tags,omitempty" jsonschema:"replace every tag"`
		Assignee        string    `json:"assignee,omitempty" jsonschema:"\"me\", \"none\", or a member's principal ID; omitted leaves it"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "memory.update",
		Description: "Correct or complete a memory: its text, kind, branch, ticket, tags or assignee. Needs the version you read. " +
			"When what a memory says is no longer true, prefer memory.supersede with a new memory, so the old reasoning stays findable.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args updateArgs) (*sdkmcp.CallToolResult, memoryResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, memoryResult{}, err
		}
		id, err := uuid.Parse(args.MemoryID)
		if err != nil {
			return nil, memoryResult{}, err
		}
		u := memories.Update{
			ExpectedVersion: args.ExpectedVersion, Kind: args.Kind, Title: args.Title, Body: args.Body,
			Branch: args.Branch, TicketKey: args.TicketKey, TicketURL: args.TicketURL, Tags: args.Tags,
		}
		if u.AssigneeSet, u.Assignee, err = assigneeArg(args.Assignee, principalID); err != nil {
			return nil, memoryResult{}, err
		}
		m, err := deps.Memories.Update(ctx, principalID, projectID, id, u)
		if err != nil {
			return nil, memoryResult{}, err
		}
		return nil, toMemory(m, principalID, nil), nil
	})

	type supersedeArgs struct {
		Project      string `json:"project" jsonschema:"project path \"namespace/slug\""`
		MemoryID     string `json:"memory_id" jsonschema:"the memory that is no longer current"`
		SupersededBy string `json:"superseded_by,omitempty" jsonschema:"the memory that replaces it; required unless status is given"`
		Status       string `json:"status,omitempty" jsonschema:"instead of superseding: archived (kept, no longer current) or active (bring it back)"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "memory.supersede",
		Description: "Retire a memory that is no longer true: name the newer memory that replaces it (create that first), " +
			"or set status archived when nothing replaces it. Retired memories stay readable but leave the default " +
			"list, search and context.bootstrap, so agents stop acting on them.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args supersedeArgs) (*sdkmcp.CallToolResult, memoryResult, error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, memoryResult{}, err
		}
		id, err := uuid.Parse(args.MemoryID)
		if err != nil {
			return nil, memoryResult{}, err
		}
		var m memories.Memory
		if args.SupersededBy != "" {
			by, perr := uuid.Parse(args.SupersededBy)
			if perr != nil {
				return nil, memoryResult{}, perr
			}
			m, err = deps.Memories.Supersede(ctx, principalID, projectID, id, by)
		} else {
			m, err = deps.Memories.SetStatus(ctx, principalID, projectID, id, args.Status)
		}
		if err != nil {
			return nil, memoryResult{}, err
		}
		return nil, toMemory(m, principalID, nil), nil
	})

	// filterArgs are the metadata filters list and search share.
	type filterArgs struct {
		Kind          string `json:"kind,omitempty" jsonschema:"only this kind"`
		Branch        string `json:"branch,omitempty" jsonschema:"only memories recorded on this git branch"`
		TicketKey     string `json:"ticket_key,omitempty" jsonschema:"only memories about this ticket"`
		Tag           string `json:"tag,omitempty" jsonschema:"only memories with this tag"`
		Assignee      string `json:"assignee,omitempty" jsonschema:"only memories assigned to this principal ID, or \"me\""`
		Author        string `json:"author,omitempty" jsonschema:"only memories written by this principal ID, or \"me\""`
		Status        string `json:"status,omitempty" jsonschema:"active (default), superseded, archived, or any"`
		CreatedAfter  string `json:"created_after,omitempty" jsonschema:"RFC 3339 time"`
		CreatedBefore string `json:"created_before,omitempty" jsonschema:"RFC 3339 time"`
	}
	toFilter := func(a filterArgs) (memories.Filter, error) {
		f := memories.Filter{Kind: a.Kind, Branch: a.Branch, TicketKey: a.TicketKey, Tag: a.Tag, Status: a.Status}
		who := func(s string) (*uuid.UUID, error) {
			if s == "me" {
				me := principalID
				return &me, nil
			}
			return parseOptionalUUID(s)
		}
		when := func(s string) (*time.Time, error) {
			if s == "" {
				return nil, nil
			}
			t, err := time.Parse(time.RFC3339, s)
			return &t, err
		}
		var err error
		if f.Assignee, err = who(a.Assignee); err != nil {
			return f, err
		}
		if f.Author, err = who(a.Author); err != nil {
			return f, err
		}
		if f.CreatedAfter, err = when(a.CreatedAfter); err != nil {
			return f, err
		}
		f.CreatedBefore, err = when(a.CreatedBefore)
		return f, err
	}

	type listArgs struct {
		Project string `json:"project" jsonschema:"project path \"namespace/slug\""`
		filterArgs
		Limit  int    `json:"limit,omitempty" jsonschema:"page size (default 100, max 500)"`
		Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page; newest first"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "memory.list",
		Description: "The project's memories, newest first, narrowed by metadata — everything on a branch, about a ticket, " +
			"of a kind, by an author. Bodies are included; use memory.search to find memories by their words.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args listArgs) (*sdkmcp.CallToolResult, pagedResult[memoryResult], error) {
		projectID, err := resolve(ctx, args.Project)
		if err != nil {
			return nil, pagedResult[memoryResult]{}, err
		}
		f, err := toFilter(args.filterArgs)
		if err != nil {
			return nil, pagedResult[memoryResult]{}, err
		}
		page, err := deps.Memories.List(ctx, principalID, projectID, f, pagingOptions(args.Limit, args.Cursor))
		if err != nil {
			return nil, pagedResult[memoryResult]{}, err
		}
		people, err := deps.Memories.People(ctx, principalID, page.Items)
		if err != nil {
			return nil, pagedResult[memoryResult]{}, err
		}
		out := make([]memoryResult, 0, len(page.Items))
		for _, m := range page.Items {
			out = append(out, toMemory(m, principalID, people))
		}
		return nil, paged(out, page.NextCursor), nil
	})

	type searchArgs struct {
		Project string `json:"project,omitempty" jsonschema:"project path \"namespace/slug\"; leave it out to search every project you can read"`
		Query   string `json:"query" jsonschema:"words to find in memory titles and bodies"`
		filterArgs
	}
	type searchHit struct {
		memoryResult
		Match   string `json:"match"`
		Snippet string `json:"snippet,omitempty"`
	}
	// An object, not a bare array, so a bounded answer can say it was
	// bounded (see plan.search).
	type searchResult struct {
		Items     []searchHit `json:"items"`
		Truncated bool        `json:"truncated"`
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "memory.search",
		Description: "Find what the team already knows before you start: search memories by words, optionally narrowed " +
			"by branch, ticket, kind, tag or author. Run it when you pick up a task — with the ticket key or the branch " +
			"if you have one — so you do not rediscover a known gotcha or reopen a settled decision. Title matches come " +
			"first, then body matches, newest first within each; at most 100 hits, each with a snippet (memory.get has the " +
			"whole body). Only active memories unless status says otherwise. Without a project it searches every project " +
			"you can read (at most 50). `truncated` true means a bound cut the answer: add a filter or narrow the query.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args searchArgs) (*sdkmcp.CallToolResult, searchResult, error) {
		f, err := toFilter(args.filterArgs)
		if err != nil {
			return nil, searchResult{}, err
		}
		var res memories.SearchResult
		if args.Project == "" {
			res, err = deps.Memories.SearchAll(ctx, principalID, args.Query, f)
		} else {
			projectID, rerr := resolve(ctx, args.Project)
			if rerr != nil {
				return nil, searchResult{}, rerr
			}
			res, err = deps.Memories.Search(ctx, principalID, projectID, args.Query, f)
		}
		if err != nil {
			return nil, searchResult{}, err
		}
		paths := map[uuid.UUID]string{}
		for _, p := range res.Projects {
			paths[p.ID] = p.Path
		}
		out := searchResult{Items: []searchHit{}, Truncated: res.Truncated}
		for _, h := range res.Hits {
			r := toMemory(h.Memory, principalID, res.People)
			r.Body, r.Project = "", paths[h.Memory.ProjectID]
			out.Items = append(out.Items, searchHit{memoryResult: r, Match: h.Match, Snippet: h.Snippet})
		}
		return nil, out, nil
	})
}
