# internal/web

Embedded single-page dashboard served at `/`.

The HTML, JS, and CSS are inlined in `page.go` via `go:embed` (or a string constant). The UI connects to `/api/v1/stream` via `EventSource` and re-renders on each SSE message without page reload.

Displays:
- Cluster metadata and leadership.
- Cluster groups and management groups with desired/actual/health state.
- Per-node role state.
- Registry and session status of connected agents.

Not implemented: UI-driven commands, config editor, history/diff view.
