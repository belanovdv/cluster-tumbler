## internal/web

Embedded draft Web UI.

Реализовано:
- `/` HTML dashboard;
- `/assets/*` embedded SVG assets;
- отображение cluster groups;
- отображение management groups;
- отображение node/role state;
- отображение registry/session статуса агентов.

не реализовано:
- управление через UI;
- SSE live refresh;
- history/diff view;
- config editor.