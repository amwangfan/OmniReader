# Admin Shell Navigation Fix Design

## Scope

Fix two web UI regressions without changing authentication, book management, or API behavior:

- Render the login brand on two lines as `Omni` and `Reader` so it remains fully visible at supported browser widths and zoom levels.
- Prevent stale page fragments from remaining visible when navigating among Home, Novel Management, Sync, and Settings without a full reload.

## Root Cause

The admin pages do not share a replacement boundary. Home renders its page header outside `<main>`, while the other modules put their headings and navigation inside `<main>`. The navigation script replaces only `<main>`, so the Home header survives when another module is inserted. This produces the duplicated layout shown in the reported screenshot.

The login brand is a single unbreakable word with a large responsive font size and tight letter spacing. At narrower effective widths, the heading exceeds the hero column and is clipped by the hero's `overflow: hidden` rule.

## Design

Every admin page will wrap all page-specific body content in one shared root element:

```html
<div id="admin-app">
  <!-- complete page content, including header and main -->
</div>
```

The navigation script will fetch the destination document, select its `#admin-app`, and replace the current `#admin-app` atomically. The transition classes will apply to this root instead of `<main>`. The delegated click and history listeners remain attached to `document` and `window`, so replacing the application root does not remove navigation behavior.

Navigation falls back to a normal page load when either document lacks `#admin-app` or the response cannot be loaded. Existing form submissions and download links keep their current full-page behavior.

The login heading will contain two explicit line elements, `Omni` and `Reader`. Its responsive font size will retain the current visual hierarchy while using a safe upper bound within the hero column.

## Testing

Server-rendering tests will assert that:

- the login page contains the two explicit brand lines;
- all four admin pages contain exactly one `#admin-app` root;
- the navigation script selects and replaces `#admin-app` and no longer replaces `<main>`;
- the admin pages still contain the navigation script.

After automated tests and a server build pass, the deployed demo will be checked by logging in, navigating Home to each module and back, and confirming that only one application root and one navigation bar remain after every transition. The login page will also be checked at narrow and wide viewport sizes.

## Non-goals

- Redesigning the visual style of the four admin modules.
- Converting forms or uploads to asynchronous submissions.
- Introducing a client-side framework or router.
