# internal/lease

Управляет двумя типами etcd lease, используемыми агентом.

`session.go` — `SessionManager` получает TTL lease (`cluster.session_ttl`), записывает регистрацию узла (постоянная) и ключ сессии (привязан к lease) в etcd, затем поддерживает KeepAlive на протяжении всего времени жизни контекста. ID сессионного lease сохраняется в `etcd.Client`, чтобы воркеры ролей могли привязывать свои записи actual/health к тому же lease.

`leadership.go` — `LeadershipManager` пытается получить ключ лидерства кластера через `TryAcquireLeaseKey` на каждом тике (`cluster.leader_renew_interval`). Ключ лидерства записывается с коротким TTL lease (`cluster.leader_ttl`). При успехе запускает KeepAlive и генерирует событие `"acquired"`; при потере — `"lost"`. Runtime подписывается на эти события для запуска/остановки горутины контроллера.
