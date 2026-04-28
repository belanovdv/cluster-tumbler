# internal/controller

Leader-only controller.

Реализовано:
- discovery management groups;
- aggregation actual/health;
- semantic writes only;
- priority-based failover в `automatic` режиме.

Не реализовано:
- auto failback policy;
- anti-flapping windows;
- quorum-aware policy;
- command queue processing;
- real actor execution.