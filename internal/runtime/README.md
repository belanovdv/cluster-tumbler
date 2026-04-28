# internal/runtime

Lifecycle-сборка всех модулей агента.

Runtime запускает:
- API / Web;
- ETCD connectivity;
- initial snapshot;
- bootstrap;
- watch loop;
- session;
- mock role manager;
- leadership;
- controller после получения leadership.