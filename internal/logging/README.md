# internal/logging

Настройка `zap.Logger`.

Модуль не заменяет zap, а задает:
- формат;
- уровень логирования;
- console/file output;
- единое поле `component`.

В остальных модулях zap используется напрямую для structured fields.