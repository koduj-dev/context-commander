# Context Commander — UI Color System

> UI palette derived from the Context Commander brand identity.  
> Brand colors remain **Context Pink** and **Relay Violet**; UI colors below add neutral surfaces,
> readable text colors and semantic states for a production interface.

## 1. Brand colors

| Token | HEX | Intended use |
|---|---|---|
| `--cc-context-pink` | `#FF2D8D` | Primary brand accent, left speaker, highlights, active context |
| `--cc-relay-violet` | `#7A35F5` | Secondary brand accent, right speaker, relay/shared-state visuals |
| `--cc-void` | `#0B0B12` | Primary dark background |
| `--cc-paper` | `#FFFFFF` | Primary light background |
| `--cc-mist` | `#F4F3F8` | Light neutral surface |
| `--cc-muted` | `#A7A6B0` | Neutral decorative/muted brand tone |

### Brand usage rules

- **Do not use gradients as a requirement.** Pink and violet should work as two independent flat colors.
- Pink and violet represent the two sides/participants. Keep that distinction consistent.
- Brand colors are accents, not default body-text colors.
- Avoid large areas filled with saturated pink or violet; use them to indicate identity, context, activity and focus.
- On dark surfaces, `Context Pink` works well as readable accent text; `Relay Violet` is better for icons, outlines and larger elements.
- For small text on light backgrounds, use the darker UI variants defined below instead of the raw brand colors.

---

# 2. Dark theme

Dark mode should be the visually strongest/default brand expression.

| Semantic token | HEX | Usage |
|---|---|---|
| `--cc-bg` | `#0B0B12` | Application background |
| `--cc-surface-1` | `#14141D` | Panels, cards, sidebar |
| `--cc-surface-2` | `#1B1B26` | Raised cards, popovers, inputs |
| `--cc-surface-3` | `#242431` | Hovered/selected neutral surface |
| `--cc-border` | `#2F2E3A` | Standard border/divider |
| `--cc-border-strong` | `#444150` | Strong divider / input active boundary |
| `--cc-text` | `#F8F7FB` | Primary text |
| `--cc-text-secondary` | `#C3C0CB` | Secondary text |
| `--cc-text-muted` | `#8D8997` | Metadata, placeholders, disabled text |
| `--cc-accent-primary` | `#FF2D8D` | Primary interactive/brand accent |
| `--cc-accent-primary-hover` | `#FF5AA8` | Pink hover/highlight |
| `--cc-accent-secondary` | `#7A35F5` | Secondary brand accent |
| `--cc-accent-secondary-hover` | `#9666FF` | Violet hover/highlight |
| `--cc-focus-ring` | `#B79AFF` | Keyboard focus outline |
| `--cc-success` | `#45D483` | Success / connected / healthy |
| `--cc-warning` | `#FFC857` | Warning / degraded |
| `--cc-danger` | `#FF5D6C` | Error / destructive action |
| `--cc-info` | `#52B7FF` | Informational state |

## Recommended dark-mode component use

```text
App background       #0B0B12
Sidebar / cards      #14141D
Input / raised       #1B1B26
Neutral hover        #242431

Primary text         #F8F7FB
Secondary text       #C3C0CB
Muted metadata       #8D8997

Primary CTA          pink background + Void text
Secondary CTA        violet background + white text
Danger               #FF5D6C
Success              #45D483
```

---

# 3. Light theme

The light theme should stay neutral and technical; saturated brand colors are accents.

| Semantic token | HEX | Usage |
|---|---|---|
| `--cc-bg` | `#FFFFFF` | Application background |
| `--cc-surface-1` | `#F7F6FA` | Panels, sidebar |
| `--cc-surface-2` | `#F0EEF4` | Inputs, raised secondary surfaces |
| `--cc-surface-3` | `#E9E6EF` | Hovered/selected neutral surface |
| `--cc-border` | `#DDD9E5` | Standard border/divider |
| `--cc-border-strong` | `#C3BECD` | Strong divider / active boundary |
| `--cc-text` | `#111118` | Primary text |
| `--cc-text-secondary` | `#4A4652` | Secondary text |
| `--cc-text-muted` | `#77727F` | Metadata, placeholders |
| `--cc-accent-primary` | `#D91473` | Accessible pink for text/actions on light background |
| `--cc-accent-primary-visual` | `#FF2D8D` | Raw brand pink for icons/large visuals |
| `--cc-accent-secondary` | `#5F27D6` | Accessible violet for text/actions |
| `--cc-accent-secondary-visual` | `#7A35F5` | Raw brand violet for icons/large visuals |
| `--cc-focus-ring` | `#7A35F5` | Keyboard focus outline |
| `--cc-success` | `#18794E` | Success |
| `--cc-warning` | `#9A6700` | Warning |
| `--cc-danger` | `#C93C49` | Error/destructive |
| `--cc-info` | `#126EAA` | Informational state |

---

# 4. Suggested semantic use

## Projects / scopes

Do **not** permanently assign arbitrary colors to individual projects as part of the brand language.
Use semantic identity instead:

| Meaning | Suggested color |
|---|---|
| Personal project | neutral / `text-secondary` |
| Shared project | `Relay Violet` |
| Active/current context | `Context Pink` |
| Organization-level scope | violet + neutral |
| Installation/global scope | neutral white/black/gray |

This keeps pink/violet meaningful instead of turning them into random category colors.

## Agent/client identity

Agent vendors should **not** inherit Context Commander brand colors.

Example:

```text
Claude Code       neutral label + provider icon
ChatGPT           neutral label + provider icon
Codex             neutral label + provider icon
Context state     Context Pink / Relay Violet
```

The brand colors describe **Context Commander state and relationships**, not the vendor.

---

# 5. Buttons

## Primary

Dark theme:

```css
background: #FF2D8D;
color: #0B0B12;
```

Use dark text on brand pink for better contrast than white text.

Light theme:

```css
background: #D91473;
color: #FFFFFF;
```

## Secondary

```css
background: #7A35F5;
color: #FFFFFF;
```

## Neutral

Use surface colors with standard text and border tokens.

## Destructive

Use semantic danger colors.  
Do **not** reuse pink for destructive operations: pink is a brand/action color, not an error color.

---

# 6. Status model

Recommended status mapping:

```text
healthy / connected / complete   green
warning / degraded               amber
failed / blocked / denied        red
information / pending info       blue
active context / current         pink
shared / relay / inherited       violet
disabled / unavailable           neutral gray
```

This is especially useful for:

- MCP connections
- project membership
- agent/client status
- artifact synchronization
- gates
- handoffs
- skill trust state
- event delivery

---

# 7. Skill and Gate UI

Suggested visual language:

```text
SKILL
icon/accent: Relay Violet

GATE
icon/accent: Context Pink

GATE DENY
semantic status: Danger red

GATE WARNING / GUIDANCE
semantic status: Warning amber
```

Do not make all gates red. A gate is a policy mechanism; red should mean an actual denied/error state.

---

# 8. Security UI

Security-related information should use normal semantic colors rather than brand colors:

```text
trusted / verified        success
unsigned / unknown        warning
blocked / revoked         danger
informational             info
```

For example:

```text
✓ Signed                   #18794E / #45D483
⚠ Executable skill         #9A6700 / #FFC857
✕ Revoked credential       #C93C49 / #FF5D6C
```

---

# 9. Accessibility notes

The raw brand colors are intentionally vivid and are not suitable for every text/background pairing.

Important combinations:

```text
#FF2D8D on #0B0B12   good accent contrast
#0B0B12 on #FF2D8D   preferred for primary dark-mode pink buttons

#FFFFFF on #7A35F5   suitable for violet buttons
#7A35F5 on #0B0B12   use mainly for large/iconographic elements

#D91473 on #FFFFFF   accessible pink text/action variant
#5F27D6 on #FFFFFF   accessible violet text/action variant
```

Rule of thumb:

> **Use the exact brand colors for identity and graphics; use semantic/darker variants when readability is the primary requirement.**

---

# 10. CSS token example

```css
:root {
  /* Brand */
  --cc-context-pink: #FF2D8D;
  --cc-relay-violet: #7A35F5;

  /* Light theme */
  --cc-bg: #FFFFFF;
  --cc-surface-1: #F7F6FA;
  --cc-surface-2: #F0EEF4;
  --cc-surface-3: #E9E6EF;
  --cc-border: #DDD9E5;
  --cc-border-strong: #C3BECD;

  --cc-text: #111118;
  --cc-text-secondary: #4A4652;
  --cc-text-muted: #77727F;

  --cc-accent-primary: #D91473;
  --cc-accent-secondary: #5F27D6;

  --cc-success: #18794E;
  --cc-warning: #9A6700;
  --cc-danger: #C93C49;
  --cc-info: #126EAA;
}

[data-theme="dark"] {
  --cc-bg: #0B0B12;
  --cc-surface-1: #14141D;
  --cc-surface-2: #1B1B26;
  --cc-surface-3: #242431;
  --cc-border: #2F2E3A;
  --cc-border-strong: #444150;

  --cc-text: #F8F7FB;
  --cc-text-secondary: #C3C0CB;
  --cc-text-muted: #8D8997;

  --cc-accent-primary: #FF2D8D;
  --cc-accent-secondary: #7A35F5;

  --cc-success: #45D483;
  --cc-warning: #FFC857;
  --cc-danger: #FF5D6C;
  --cc-info: #52B7FF;
}
```

---

# 11. Short design principle

> **The UI should be mostly neutral. Pink identifies active context; violet identifies relay/shared context. Semantic states keep their conventional green/amber/red/blue meanings.**

This keeps Context Commander recognizable without turning every screen into a pink-purple neon wall.
