# internal/api

HTTP API и подключение Web UI.

Реализовано:
- `/api/v1/state` — pretty JSON state view;
- `/api/v1/commands` — создание command key;
- `/` — HTML UI через `internal/web`.

Частично реализовано:
- команды создаются, но command queue processor пока не реализован.