# internal/logging

Structured logger construction and component tagging.

Builds a `zap.Logger` from config (level, plain/JSON format, console and/or file output). `WithComponent` attaches a `component` field to a logger instance so log lines from different packages are distinguishable without adding a call-site field everywhere.
