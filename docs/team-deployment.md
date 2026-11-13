# Running Context Commander for a team

A recipe for a small team, three to ten developers, each with their own
coding agents working on the same application. By the end:

- everyone signs in with their Google Workspace account, and only addresses
  you allow get in;
- every agent reports what it is working on, on which branch and ticket, and
  the dashboard shows it live, with time, tokens and an estimated cost;
- agents write memories (decisions, findings, gotchas) and search them,
  by words and optionally by meaning;
- ideas dropped into the project are picked up by a planning agent, which
  proposes a solution for a person to accept.

It builds on [quickstart.md](quickstart.md). Read that once; this file only
adds what a team needs on top of it.

## 1. The server

On a host with Docker, take `deploy/quickstart/` and add a `.env` next to its
`docker-compose.yml`:

```dotenv
# openssl rand -base64 32 — keep a copy somewhere safe: without it,
# nothing in the database can be decrypted.
CC_ROOT_KEY=...

# Google sign-in (section 2).
CC_OIDC_ISSUER=https://accounts.google.com
CC_OIDC_CLIENT_SECRET=...

# The reverse proxy talks to the container over Docker's bridge, so the
# server sees the bridge gateway, not 127.0.0.1. `docker network inspect`
# shows the exact range; Docker's defaults fall inside this one.
CC_TRUSTED_PROXIES=172.16.0.0/12

# Optional: what tokens would cost on the API, as an estimate.
# CC_USAGE_PRICES=/data/usage-prices.json

# Optional: keep only this many days of the usage ledger (default: forever).
# CC_USAGE_LEDGER_RETENTION=400
```

The remaining flags go in a `compose.override.yml` beside it:

```yaml
services:
  app:
    command:
      - serve
      - --addr=0.0.0.0:8490
      - --insecure-allow-cleartext # TLS is the reverse proxy's job
      - --oidc-client-id=<id>.apps.googleusercontent.com
      - --oidc-redirect-url=https://cc.example.com/v1/auth/oidc/callback
      - --oidc-label=Google
```

Put a TLS-terminating reverse proxy in front of `127.0.0.1:8490`. See
[install.md](install.md) for the Server-Sent Events settings the dashboard
needs. Then run `docker compose up -d`.

**First administrator.** Open the site. On first start the server prints a
one-time setup code in its log (`docker compose logs app`). Enter it with
your email and a password. You are the installation administrator.

## 2. Google sign-in, limited to your company

In the Google Cloud console, under **APIs & Services → Credentials**, create an
**OAuth client ID** of type **Web application**. Add
`https://cc.example.com/v1/auth/oidc/callback` as an authorized redirect URI.
Put the client ID in the override file and the secret in `.env`.

Then, in Context Commander, as the administrator:

1. **Organizations → New**: create your company's organization.
2. **Administration → Sign-in → Add a rule**: enter your domain (`4net.tv`)
   and pick the organization. Add individual addresses the same way for
   anyone outside the domain, such as a contractor's `@gmail.com`.

Anyone with a Google Workspace account in a listed domain can now sign in.
Their account is created on the first sign-in, and they join the
organizations the rule names. A personal Google account registered under
your domain's address is refused, because the Workspace (`hd`) is checked.
Everyone else is refused. See [oidc.md](oidc.md) and
[ADR 0024](adr/0024-sign-in-rules-managed-by-an-administrator.md).

## 3. The project, and who is in it

Create the project for your application under the organization (**Projects →
New**). Then, in its **Members** tab, **invite your colleagues by email** with
a role, usually Contributor. Each invitation is accepted at that person's
first sign-in, so they arrive already in the project
([ADR 0030](adr/0030-project-invitations.md)).

## 4. Each developer's agents

Each developer does this once per machine.

1. **A token.** In **Access tokens**, create one per client and machine
   (`claude-code-laptop`, `codex-desktop`). Presence and usage are tracked
   per token, so one per client keeps the dashboard honest.
2. **Connect the client** over MCP, as in [quickstart.md](quickstart.md)
   section 4, with `https://cc.example.com/mcp`.
3. **Environment:**

   ```bash
   export CC_URL=https://cc.example.com
   export CC_TOKEN=cc_...           # the token from step 1
   export CC_PROJECT=4net/app       # the project, namespace/slug
   ```

4. **The usage hook**, which sends exact token counts and the current git
   branch after every turn. See [deploy/mcp-clients/README.md](../deploy/mcp-clients/README.md),
   "Token usage". You can ask the agent to install it.
5. **Tell the agent how to work.** Add this to the application repository's
   `CLAUDE.md` (or `AGENTS.md` for Codex), so every agent and every
   developer gets the same instructions:

   ```markdown
   ## Shared context: Context Commander

   This project's shared state lives in Context Commander, project `4net/app`
   (MCP server `context-commander`). Follow docs/AGENT-GUIDE.md from that
   repository. In short:

   - Start with `context.bootstrap`, then `plan.board`. Do not start work
     someone else is already doing.
   - Search memories (`memory.search`) before you decide anything. Someone
     may already have.
   - Record your plan (`plan.create`), mark steps as you go
     (`plan.update_item`), and call `plan.report` when you move to a step and
     every few minutes. Pass `branch` and `ticket_key` (e.g. `PROJ-123`).
     Call `plan.stop` when you finish.
   - Write a memory (`memory.create`) for every decision, finding or gotcha
     the next person would otherwise have to rediscover, with its branch,
     ticket and ticket URL.
   - Leave a handoff if you stop with work unfinished.
   ```

## 5. The planning agent

Ideas go into a project's **Ideas** tab, from the interface or from any agent
(`idea.add`). A planning agent takes them one at a time, reads the project's
context, and proposes a solution with a draft plan. A person then accepts or
rejects the proposal in the interface. Give the planning agent its own token,
so the team can see which agent proposed what, and run it by hand or on a
schedule:

```bash
claude -p "$(cat deploy/mcp-clients/planning-agent.md)"
```

See [deploy/mcp-clients/planning-agent.md](../deploy/mcp-clients/planning-agent.md)
and [ADR 0026](adr/0026-idea-stack.md).

## 6. Optional: search by meaning

Memories can be searched by meaning as well as by words, in Czech and
English. Uncomment the `ollama` service and the `CC_EMBEDDINGS_*` lines in
the quickstart compose file, then pull the model once
(`docker compose exec ollama ollama pull bge-m3`). Nothing leaves the server
unless you choose an external provider. See
[semantic-search.md](semantic-search.md).

## 7. What to watch

- **Dashboard:** who is working where right now, ideas waiting for a
  decision, and usage by day and by person.
- **A project's Usage tab:** time and tokens by day, person, branch, ticket
  or model, priced when `CC_USAGE_PRICES` is set. Kept forever unless
  `CC_USAGE_LEDGER_RETENTION` names a number of days.
- **Administration → Security log:** refused sign-ins and rate limits.

Back up the two Docker volumes and keep `CC_ROOT_KEY`; see "Backup and
restore" in [quickstart.md](quickstart.md). To upgrade, pull the new release
and run `docker compose up -d --build`. Migrations run before the server
starts.
