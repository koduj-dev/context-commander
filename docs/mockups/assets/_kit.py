"""Context Commander mockup kit.

Pages are composed from these functions, not written by hand. That is the
whole point: a component is decided once here, and every screen inherits the
decision. A page that needs markup this module cannot express is a signal
that the kit is missing something — add it here, do not improvise there.
"""
import sys; sys.path.insert(0, '/tmp')
from gen import shell  # noqa: F401  (re-exported for pages)

def esc(s): return s

# ---------------------------------------------------------------- primitives

def badge(text, tone="", dot=True):
    """Status. Always a word plus a glyph — never colour alone."""
    cls = " " + tone if tone else ""
    d = '<span class="dot"></span>' if dot else ""
    return f'<span class="badge{cls}">{d}{text}</span>'

def scope(kind, label=None):
    """Where a thing lives: installation / organization / project."""
    return f'<span class="scope {kind}">{label or kind}</span>'

def mono(s, truncate=False, dim=False):
    cls = "mono"
    if truncate: cls += " truncate"
    if dim: cls += " dim"
    return f'<span class="{cls}">{s}</span>'

def cell(primary, secondary=None):
    """Table cell with a value and a quieter second line under it."""
    if secondary is None:
        return primary
    return f'{primary}<div class="sub-line">{secondary}</div>'

def kpi(value, label, unit=None, tone=None):
    style = f' style="color:var(--cc-{tone})"' if tone else ""
    u = f'<span class="unit"> {unit}</span>' if unit else ""
    return f'<div class="kpi"><span class="v"{style}>{value}{u}</span><span class="k">{label}</span></div>'

def meter(pct, tone=""):
    cls = "meter" + (" " + tone if tone else "")
    return f'<div class="{cls}"><i style="width:{pct}%"></i></div>'

def note(text, tone=""):
    cls = "note" + (" " + tone if tone else "")
    return f'<div class="{cls}">{text}</div>'

def code(text):
    return f'<div class="code mono">{text}</div>'

def defs(pairs):
    rows = "".join(f'<dt>{k}</dt><dd>{v}</dd>' for k, v in pairs)
    return f'<dl class="defs">{rows}</dl>'

def btn(label, kind="", small=False, disabled=False, href=None, title=None):
    cls = "btn"
    if kind: cls += " btn-" + kind
    if small: cls += " btn-sm"
    t = f' title="{title}"' if title else ""
    if href:
        return f'<a class="{cls}" href="{href}"{t}>{label}</a>'
    d = " disabled" if disabled else ""
    return f'<button class="{cls}"{d}{t}>{label}</button>'

def seg(options, active=0):
    out = []
    for i, o in enumerate(options):
        out.append(f'<button type="button" aria-pressed="{"true" if i == active else "false"}">{o}</button>')
    return '<span class="seg">%s</span>' % "".join(out)

def label_caps(text, value):
    return f'<div><div class="label-caps">{text}</div>{value}</div>'

# ---------------------------------------------------------------- structures

def page_head(title, lead=None, actions=None, title_mono=False):
    cls = ' class="mono"' if title_mono else ""
    lead_html = f'<p>{lead}</p>' if lead else ""
    acts = f'<div class="actions">{"".join(actions)}</div>' if actions else ""
    return f"""<div class="page-head">
  <div><h1{cls}>{title}</h1>{lead_html}</div>
  {acts}
</div>"""

def panel(title=None, body="", sub=None, right=None, footer=None, tight=False, badges=None):
    head = ""
    if title:
        bits = [f'<h2>{title}</h2>']
        if badges: bits += badges
        if sub: bits.append(f'<span class="sub">{sub}</span>')
        if right: bits.append(f'<div class="right">{right}</div>')
        head = "<header>%s</header>" % "".join(bits)
    cls = "body tight" if tight else "body"
    foot = f'<footer>{footer}</footer>' if footer else ""
    inner = body if tight else f'<div class="{cls}">{body}</div>'
    if tight:
        inner = f'<div class="{cls}">{body}</div>'
    return f'<section class="panel">{head}{inner}{foot}</section>'

def table(columns, rows, sticky=True):
    """columns: list of (label, align) or plain label. rows: list of lists."""
    ths = []
    aligns = []
    for c in columns:
        label, align = (c if isinstance(c, tuple) else (c, ""))
        aligns.append(align)
        ths.append(f'<th class="{align}">{label}</th>' if align else f'<th>{label}</th>')
    head = f'<thead><tr>{"".join(ths)}</tr></thead>' if any(
        (c[0] if isinstance(c, tuple) else c) for c in columns) else ""
    body = []
    for r in rows:
        tds = []
        for i, v in enumerate(r):
            a = aligns[i] if i < len(aligns) else ""
            tds.append(f'<td class="{a}">{v}</td>' if a else f'<td>{v}</td>')
        body.append(f'<tr>{"".join(tds)}</tr>')
    return f'<table class="dt">{head}<tbody>{"".join(body)}</tbody></table>'

def feed(items):
    """items: list of (when, what, trailing_html_or_None)."""
    lis = []
    for when, what, trail in items:
        t = trail or ""
        lis.append(f'<li><span class="when">{when}</span><span class="what">{what}</span>{t}</li>')
    return f'<ul class="feed">{"".join(lis)}</ul>'

def grid(children, cols="g2", extra=""):
    cls = f"grid {cols}" + (" " + extra if extra else "")
    return f'<div class="{cls}">{"".join(children)}</div>'

def stack(children, start=False):
    cls = "stack" + (" stack-start" if start else "")
    return f'<div class="{cls}">{"".join(children)}</div>'
