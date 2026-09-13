import os, html

NAV_GLOBAL = [
    ("dashboard.html", "Dashboard", "M3 9.5 9 4l6 5.5M4.5 8.5V15h9V8.5", None),
    ("projects.html", "Projects", "M2 5.5h5l1.2 1.5H16V14H2zM2 5.5V3.5h4l1 1.5", "7"),
    ("organizations.html", "Organizations", "M3 15V4.5h5V15M11 15V7.5h4V15M2 15h14M5 7h1M5 9.5h1M13 10h1", "1"),
    ("registry.html", "Skill Registry", "M4 3.5h8l2 2.5v9H4zM6 7h6M6 9.5h6M6 12h4", "14"),
    ("administration.html", "Administration", "M9 2.5 3.5 5v4.2c0 3 2.3 5.2 5.5 6.3 3.2-1.1 5.5-3.3 5.5-6.3V5z M6.8 9l1.6 1.6L11.6 7", None),
]

PROJECT_TABS = [
    ("project-overview.html", "Overview"),
    ("project-members.html", "Members"),
    ("project-connections.html", "Connections"),
    ("project-skills.html", "Skills"),
    ("project-policies.html", "Policies"),
    ("project-secrets.html", "Secrets"),
    ("project-activity.html", "Activity"),
    ("project-settings.html", "Settings"),
]

def icon(d):
    return ('<svg class="ico" viewBox="0 0 18 18" fill="none" stroke="currentColor" '
            'stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="%s"/></svg>' % d)

def nav(active):
    out = ['<div class="nav-label">Workspace</div>']
    for href, label, d, count in NAV_GLOBAL:
        if label == "Administration":
            out.append('<div class="nav-label">Installation</div>')
        cls = ' class="active"' if href == active else ''
        c = '<span class="count num">%s</span>' % count if count else ''
        out.append('<a href="%s"%s>%s<span>%s</span>%s</a>' % (href, cls, icon(d), label, c))
    return "\n        ".join(out)

def shell(fname, title, active, crumbs, body, tabs_active=None, page_class=""):
    tabs = ""
    if tabs_active:
        links = "".join(
            '<a href="%s"%s>%s</a>' % (h, ' class="active"' if h == tabs_active else '', l)
            for h, l in PROJECT_TABS)
        tabs = '<nav class="tabs">%s</nav>' % links
    doc = f"""<!doctype html>
<html lang="en" data-theme="dark">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{html.escape(title)} · Context Commander</title>
<link rel="icon" href="../brand/svg/cc-favicon.svg">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&display=swap">
<link rel="stylesheet" href="../brand/tokens/ui-colors.css">
<link rel="stylesheet" href="assets/mockup.css">
</head>
<body>
<div class="mockbar">
  <span class="tag">Mockup</span>
  <a href="index.html">All screens</a>
  <span class="dim">·</span>
  <span>{html.escape(title)}</span>
  <span style="flex:1"></span>
  <span class="seg">
    <button type="button" aria-pressed="true" onclick="setTheme('dark',this)">Dark</button>
    <button type="button" aria-pressed="false" onclick="setTheme('light',this)">Light</button>
  </span>
</div>
<div class="app">
  <aside class="sidebar">
    <div class="brand">
      <img class="logo-dark" src="../brand/svg/cc-logo-horizontal-on-dark.svg" alt="Context Commander">
      <img class="logo-light" src="../brand/svg/cc-logo-horizontal-on-light.svg" alt="Context Commander" hidden>
    </div>
    <nav class="nav">
        {nav(active)}
    </nav>
    <div class="sidebar-foot">
      <div class="avatar">FM</div>
      <div class="who">
        <b>Filip Majerík</b>
        <span>Installation admin</span>
      </div>
    </div>
  </aside>
  <div class="main">
    <div class="topbar">
      <div class="crumbs">{crumbs}</div>
      <div class="spacer"></div>
      <div class="search"><svg class="ico" viewBox="0 0 18 18" fill="none" stroke="currentColor" stroke-width="1.4"><circle cx="8" cy="8" r="5"/><path d="m12 12 3 3"/></svg><span>Search</span><kbd>⌘K</kbd></div>
    </div>
    <div class="page {page_class}">
      {tabs}
      {body}
    </div>
  </div>
</div>
<script>
function setTheme(t, el) {{
  document.documentElement.setAttribute('data-theme', t);
  el.parentNode.querySelectorAll('button').forEach(b => b.setAttribute('aria-pressed', String(b === el)));
  document.querySelector('.logo-dark').hidden = (t === 'light');
  document.querySelector('.logo-light').hidden = (t !== 'light');
}}
</script>
</body>
</html>
"""
    open(fname, "w").write(doc)
    return fname
