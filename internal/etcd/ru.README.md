# internal/etcd

Обертка над клиентом etcd v3.

Предоставляет:
- `GetPrefix` / `WatchPrefix` — массовое чтение и потоковый watch, возвращающий значения `store.Event`.
- `Put` / `PutWithLease` / `Delete` — базовые операции записи KV.
- `TryPutIfAbsent` — условная запись через транзакцию etcd (create-revision == 0).
- `TryAcquireLeaseKey` — атомарный шаг выборов лидера: запись ключа с lease только если ключ не существует.
- `GrantLease` / `KeepAlive` — управление жизненным циклом lease.
- `SetSessionLeaseID` / `SessionLeaseID` / `ClearSessionLeaseID` — разделяемое изменяемое состояние для ID сессионного lease, используемое воркерами ролей для привязки своих записей к активной сессии.
