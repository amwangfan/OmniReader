# Admin Shell Navigation Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep the login brand fully visible and eliminate duplicated admin page fragments during animated module navigation.

**Architecture:** Wrap every complete admin page in the same `#admin-app` boundary and atomically replace that boundary after fetching another module. Keep delegated navigation listeners outside the replaceable root, and render the login brand as two explicit lines.

**Tech Stack:** Go 1.24+, `net/http`, `html/template`, vanilla JavaScript, Go `testing`/`httptest`

---

### Task 1: Add rendering regression tests

**Files:**
- Modify: `server/internal/httpapi/server_test.go`
- Test: `server/internal/httpapi/server_test.go`

- [ ] **Step 1: Write the failing login brand test**

Extend `TestLoginPageRendersStyledForm` with these required fragments:

```go
`<span class="brand-line">Omni</span>`,
`<span class="brand-line">Reader</span>`,
```

- [ ] **Step 2: Write the failing shared-root navigation test**

Add a test that requests all four authenticated admin routes and checks the shared root and replacement script:

```go
func TestAdminPagesUseSharedApplicationRoot(t *testing.T) {
	handler := testAuthHandler(t)
	cookie := webLoginForTest(t, handler)
	for _, route := range []string{"/admin/books", "/admin/novels", "/admin/sync", "/admin/settings"} {
		req := httptest.NewRequest(http.MethodGet, route, nil)
		req.AddCookie(cookie)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", route, res.Code, res.Body.String())
		}
		body := res.Body.String()
		if strings.Count(body, `id="admin-app"`) != 1 {
			t.Fatalf("%s must render exactly one admin root: %s", route, body)
		}
		for _, want := range []string{`querySelector("#admin-app")`, `replaceWith(nextRoot)`} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s navigation missing %q: %s", route, want, body)
			}
		}
		if strings.Contains(body, `querySelector("main").replaceWith`) {
			t.Fatalf("%s still replaces only main: %s", route, body)
		}
	}
}
```

- [ ] **Step 3: Run the focused tests and verify RED**

Run on the Linux build host:

```bash
go test ./internal/httpapi -run 'Test(LoginPageRendersStyledForm|AdminPagesUseSharedApplicationRoot)$'
```

Expected: FAIL because the login heading has no `.brand-line` spans and admin pages have no `#admin-app` root.

- [ ] **Step 4: Commit the failing tests**

```bash
git add server/internal/httpapi/server_test.go
git commit -m "test: cover shared admin shell navigation"
```

### Task 2: Implement the shared admin application root

**Files:**
- Modify: `server/internal/httpapi/server.go:29-91`
- Modify: `server/internal/httpapi/server.go:752-814`
- Modify: `server/internal/httpapi/server.go:889-938`
- Modify: `server/internal/httpapi/server.go:999-1022`
- Modify: `server/internal/httpapi/server.go:1055-1091`
- Test: `server/internal/httpapi/server_test.go`

- [ ] **Step 1: Wrap every admin page body**

Place all page-specific content inside one root immediately below `<body>` and close it immediately before the navigation script:

```html
<body>
  <div id="admin-app">
    <!-- existing header/main content -->
  </div>
  <!-- navigation script -->
</body>
```

- [ ] **Step 2: Replace the shared root in the navigation script**

Use the shared root for transition and replacement:

```javascript
const currentRoot = document.querySelector("#admin-app");
if (!currentRoot) {
  window.location.href = url.href;
  return;
}
ensureTransitionStyle();
currentRoot.classList.add("omni-slide-out");
// fetch and parse response
const nextRoot = nextDoc.querySelector("#admin-app");
if (!nextRoot) {
  window.location.href = url.href;
  return;
}
document.querySelector("#admin-app").replaceWith(nextRoot);
const entered = document.querySelector("#admin-app");
```

Update injected transition CSS from `main` to `#admin-app` so layout styles on nested `<main>` remain untouched.

- [ ] **Step 3: Run the focused admin-root test and verify GREEN**

```bash
go test ./internal/httpapi -run TestAdminPagesUseSharedApplicationRoot
```

Expected: PASS.

- [ ] **Step 4: Commit the implementation**

```bash
git add server/internal/httpapi/server.go
git commit -m "fix: replace complete admin application shell"
```

### Task 3: Split and constrain the login brand

**Files:**
- Modify: `server/internal/httpapi/server.go:323-329`
- Modify: `server/internal/httpapi/server.go:455`
- Test: `server/internal/httpapi/server_test.go`

- [ ] **Step 1: Render the two explicit brand lines**

Replace the single text node with:

```html
<h1 aria-label="OmniReader">
  <span class="brand-line">Omni</span>
  <span class="brand-line">Reader</span>
</h1>
```

- [ ] **Step 2: Add safe line and size styling**

```css
h1 {
  font-size: clamp(44px, 6.4vw, 78px);
  line-height: .82;
}
.brand-line { display: block; }
```

- [ ] **Step 3: Run the focused login test and verify GREEN**

```bash
go test ./internal/httpapi -run TestLoginPageRendersStyledForm
```

Expected: PASS.

- [ ] **Step 4: Commit the implementation**

```bash
git add server/internal/httpapi/server.go
git commit -m "fix: keep login brand visible at narrow widths"
```

### Task 4: Verify and deploy

**Files:**
- Verify: `server/internal/httpapi/server.go`
- Verify: `server/internal/httpapi/server_test.go`

- [ ] **Step 1: Run formatting, full tests, and build**

```bash
gofmt -w internal/httpapi/server.go internal/httpapi/server_test.go
go test ./...
go build ./cmd/omnireader-server
```

Expected: formatting produces no semantic diff, all packages pass, and build exits 0.

- [ ] **Step 2: Inspect the final diff**

```bash
git diff --check
git status --short
```

Expected: no whitespace errors and no unrelated files staged or modified.

- [ ] **Step 3: Deploy to the Tailscale demo**

Copy the server source to `/tmp/omnireader-demo/src`, build the binary, preserve `/tmp/omnireader-demo/config.env` and its data directory, stop the previous process, and launch the replacement with `setsid -f`.

- [ ] **Step 4: Verify the deployed interaction**

At `http://100.114.93.90:18080/`, verify login at narrow and wide widths. After authentication, navigate Home to Novel Management, Sync, and Settings and back; after each route, assert exactly one `#admin-app` and one admin navigation bar are present.

- [ ] **Step 5: Commit any formatting-only changes, then push the current branch**

```bash
git add server/internal/httpapi/server.go server/internal/httpapi/server_test.go
git commit -m "style: format admin navigation fix" # only if gofmt changed files after prior commits
git push
```
