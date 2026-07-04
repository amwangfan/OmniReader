# Persistent Admin Shell Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep the OmniReader brand and module navigation stationary while only the selected admin module content animates and changes.

**Architecture:** Every admin response renders the same `#admin-app` shell containing `.admin-header`, `.admin-brand`, and `.admin-nav`, followed by `#admin-content`. The existing delegated JavaScript router fetches a destination page but replaces only `#admin-content` and updates the persistent navigation's active state.

**Tech Stack:** Go 1.24+, `net/http`, `html/template`, vanilla JavaScript, Go `testing`/`httptest`

---

### Task 1: Define persistent-shell rendering behavior with failing tests

**Files:**
- Modify: `server/internal/httpapi/server_test.go`
- Test: `server/internal/httpapi/server_test.go`

- [ ] **Step 1: Replace the shared-root regression with persistent-shell assertions**

Rename `TestAdminPagesUseSharedApplicationRoot` to `TestAdminPagesUsePersistentShell` and, for each admin route, assert:

```go
for _, marker := range []string{
	`id="admin-app"`,
	`class="admin-header"`,
	`class="admin-brand"`,
	`class="admin-nav"`,
	`id="admin-content"`,
	`querySelector("#admin-content")`,
	`replaceWith(nextContent)`,
	`updateActiveNavigation(url.pathname)`,
} {
	if strings.Count(body, marker) == 0 {
		t.Fatalf("%s missing %q: %s", route, marker, body)
	}
}
if strings.Count(body, `id="admin-app"`) != 1 ||
	strings.Count(body, `class="admin-header"`) != 1 ||
	strings.Count(body, `class="admin-nav"`) != 1 ||
	strings.Count(body, `id="admin-content"`) != 1 {
	t.Fatalf("%s must render one persistent shell: %s", route, body)
}
if !strings.Contains(body, `<a class="active" href="`+route+`">`) {
	t.Fatalf("%s missing active navigation item: %s", route, body)
}
for _, stale := range []string{
	`querySelector("#admin-app").replaceWith`,
	`querySelector("main").replaceWith`,
} {
	if strings.Contains(body, stale) {
		t.Fatalf("%s uses stale replacement boundary %q: %s", route, stale, body)
	}
}
```

- [ ] **Step 2: Run the focused test and verify RED**

```bash
go test ./internal/httpapi -run TestAdminPagesUsePersistentShell
```

Expected: FAIL because current pages do not share `.admin-header`, `.admin-nav`, or `#admin-content`, and the router still replaces `#admin-app`.

- [ ] **Step 3: Commit the failing test**

```bash
git add server/internal/httpapi/server_test.go
git commit -m "test: define persistent admin shell behavior"
```

### Task 2: Restrict navigation to the module content boundary

**Files:**
- Modify: `server/internal/httpapi/server.go:29-92`
- Test: `server/internal/httpapi/server_test.go`

- [ ] **Step 1: Target content in the transition CSS and router**

Replace application-root selectors with the content boundary:

```javascript
"#admin-content { will-change: transform, opacity; }"

const currentContent = document.querySelector("#admin-content");
currentContent.classList.add("omni-slide-out");

const nextContent = nextDoc.querySelector("#admin-content");
document.querySelector("#admin-content").replaceWith(nextContent);
const entered = document.querySelector("#admin-content");
```

Keep the existing full-page fallback whenever either content node is absent.

- [ ] **Step 2: Update the persistent active navigation item**

Add and call:

```javascript
function updateActiveNavigation(pathname) {
  document.querySelectorAll(".admin-nav a[href]").forEach(link => {
    link.classList.toggle("active", new URL(link.href, window.location.href).pathname === pathname);
  });
}

updateActiveNavigation(url.pathname);
```

Call it after replacing content and before starting the entrance transition.

- [ ] **Step 3: Leave history and normal-link behavior unchanged**

Retain `history.pushState`, `popstate`, fetch failure fallback, form submissions, downloads, and non-admin links exactly as they currently behave.

### Task 3: Standardize all four server-rendered admin shells

**Files:**
- Modify: `server/internal/httpapi/server.go:575-1105`
- Test: `server/internal/httpapi/server_test.go`

- [ ] **Step 1: Render the same fixed header on every route**

Inside each `#admin-app`, render this structure with the matching link marked active:

```html
<header class="admin-header">
  <p class="admin-eyebrow">Personal library sync</p>
  <h1 class="admin-brand">OmniReader</h1>
  <nav class="admin-nav" aria-label="Admin navigation">
    <a href="/admin/books">主页</a>
    <a href="/admin/novels">小说管理</a>
    <a href="/admin/sync">同步</a>
    <a href="/admin/settings">设置</a>
  </nav>
</header>
<main id="admin-content">
  <!-- route-specific content -->
</main>
```

Use `class="active"` on only the route's own link.

- [ ] **Step 2: Keep route-specific headings inside content**

Home starts with a compact module introduction containing `主页`, the existing upload description, and the book count. Novel Management, Sync, and Settings retain their current headings and explanatory text at the top of `#admin-content`.

- [ ] **Step 3: Use identical fixed-header CSS in every response**

Each page includes these same shell rules, with route-specific rules scoped beneath `#admin-content`:

```css
.admin-header { max-width: 1120px; margin: 0 auto; padding: 36px 24px 18px; }
.admin-eyebrow { margin: 0 0 8px; color: #1f6f5b; font: 700 12px/1.2 ui-sans-serif,system-ui,sans-serif; letter-spacing: .16em; text-transform: uppercase; }
.admin-brand { margin: 0; color: #252018; font: 700 clamp(36px,5vw,64px)/.95 ui-serif,Georgia,"Noto Serif SC",serif; letter-spacing: -.045em; }
.admin-nav { display: flex; gap: 10px; flex-wrap: wrap; margin-top: 18px; }
.admin-nav a { border: 1px solid rgba(81,62,38,.14); border-radius: 999px; padding: 8px 12px; color: #776b5d; background: rgba(255,255,255,.46); text-decoration: none; font: 700 13px ui-sans-serif,system-ui,sans-serif; }
.admin-nav a.active { color: #fff; background: #7a4f2a; border-color: transparent; }
```

- [ ] **Step 4: Run the focused test and verify GREEN**

```bash
go test ./internal/httpapi -run TestAdminPagesUsePersistentShell
```

Expected: PASS.

- [ ] **Step 5: Commit the implementation**

```bash
git add server/internal/httpapi/server.go
git commit -m "fix: keep admin header mounted during navigation"
```

### Task 4: Verify, deploy, and inspect the interaction

**Files:**
- Verify: `server/internal/httpapi/server.go`
- Verify: `server/internal/httpapi/server_test.go`

- [ ] **Step 1: Format, test, and build on N100**

```bash
gofmt -w internal/httpapi/server.go internal/httpapi/server_test.go
go test ./...
go build -o /tmp/omnireader-check/omnireader-server ./cmd/omnireader-server
```

Expected: all packages pass and the build exits 0.

- [ ] **Step 2: Inspect repository state**

```bash
git diff --check
git status --short
```

Expected: no whitespace errors and no unrelated changes.

- [ ] **Step 3: Deploy without replacing data**

Build `/tmp/omnireader-demo/omnireader-server.new`, preserve `config.env` and `/tmp/omnireader-demo/data`, stop only the existing OmniReader process, atomically replace the binary, and restart it with `setsid -f`.

- [ ] **Step 4: Verify with the local browser through a temporary SSH tunnel**

At 1280px viewport, log in and record the `.admin-header` bounding rectangle. Navigate Home → Novel Management → Sync → Settings → Home. After each route, verify:

```javascript
({
  apps: document.querySelectorAll("#admin-app").length,
  headers: document.querySelectorAll(".admin-header").length,
  navs: document.querySelectorAll(".admin-nav").length,
  contents: document.querySelectorAll("#admin-content").length,
  activeHref: document.querySelector(".admin-nav a.active")?.getAttribute("href"),
  headerRect: document.querySelector(".admin-header")?.getBoundingClientRect().toJSON()
})
```

Expected: every count is 1, `activeHref` matches the route, and the header rectangle is unchanged.

- [ ] **Step 5: Check browser errors and clean temporary files**

Expected: zero console errors. Reset the viewport, close the browser tab, stop the SSH tunnel, and remove `/tmp/omnireader-check` plus temporary archives.
