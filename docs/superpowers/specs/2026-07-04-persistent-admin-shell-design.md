# Persistent Admin Shell Design

## Goal

Keep the OmniReader brand and module navigation visually stationary while moving between Home, Novel Management, Sync, and Settings. Only the selected module's content should animate and change.

## Current Problem

The current shared `#admin-app` boundary fixed stale DOM fragments by replacing the entire rendered application root. Because that root includes the brand, navigation, and module content, every navigation animates the whole page. The result is technically correct but visually disruptive.

## Structure

All four admin responses will render the same shell structure:

```html
<div id="admin-app">
  <header class="admin-header">
    <p class="admin-eyebrow">Personal library sync</p>
    <h1 class="admin-brand">OmniReader</h1>
    <nav class="admin-nav">...</nav>
  </header>
  <main id="admin-content">
    <!-- route-specific heading and controls -->
  </main>
</div>
```

The brand and navigation remain outside the replacement boundary. Each response marks its matching navigation link active for full-page loads and direct visits.

## Navigation Behavior

The delegated navigation handler will fetch the target admin page, parse its `#admin-content`, and replace only the current `#admin-content`. Transition classes will apply only to that content node.

After replacement, the handler will:

- update the document title;
- push or restore the route in browser history;
- set `active` only on the navigation link whose pathname matches the target URL;
- fall back to a normal page load if the source or target content boundary is missing or the request fails.

The existing admin shell and delegated listeners remain mounted. Forms, downloads, and destructive confirmations retain their current full-page behavior.

## Styling

Every admin response will include identical shell styles for `.admin-header`, `.admin-brand`, and `.admin-nav`. Route-specific styles will be scoped to `#admin-content` or route-specific classes so replacing destination `<head>` styles cannot resize or reposition the persistent header.

The content transition will retain the current short horizontal fade, but `will-change`, exit, and entrance classes will target `#admin-content` only. Reduced-motion preferences will continue to disable movement.

## Testing

Server-rendering tests will verify that all four routes contain exactly one `#admin-app`, one `.admin-header`, one `.admin-nav`, and one `#admin-content`. They will also verify that the navigation script replaces `#admin-content`, never `#admin-app` or a generic `<main>`, and updates the active navigation item by pathname.

Browser verification will navigate through all four routes and back to Home. After each transition, it will assert one shell, one header, one navigation element, one content root, the correct active link, and no browser console errors. The fixed header's bounding rectangle will be compared before and after navigation to confirm it did not move.

## Non-goals

- Converting form submissions or uploads to asynchronous operations.
- Introducing a client-side framework.
- Redesigning the content of the four modules.
