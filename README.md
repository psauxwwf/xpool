# xpool

`xpool` запускает Xray как локальный HTTP-прокси с пулом SOCKS5 upstream-прокси, проверяет их полным скачиванием тестового URL и переключает Xray balancer только на готовые прокси.

Проект нужен для стабильного доступа через большой список прокси, где часть строк может быть невалидной или часть прокси может не работать. Невалидные URL пропускаются при загрузке, а прокси, не прошедшие full-download health check через Xray, удаляются из дальнейших проверок на время текущего запуска.

## Возможности

- Генерация `config.json` для Xray из списка SOCKS5-прокси.
- Внешний HTTP inbound `0.0.0.0:8080` с auth из `HTTP_USERNAME` и `HTTP_PASSWORD`.
- Локальный HTTP inbound `127.0.0.1:8000` без auth.
- Xray API inbound для управления balancer override.
- Отдельный локальный check inbound на каждый outbound-прокси.
- Health pool с батчевой проверкой, ограничением concurrency, jitter и лимитом скачиваемого тела.
- Постоянное исключение proxy route после failed full-download check в рамках текущего процесса.
- Always-on status API: `/healthz` и `/status`.
- Конфигурация через YAML, без runtime-флагов в CLI.

## Требования

- Go 1.26.5 или новее.
- Установленный `xray` в `PATH` или путь к бинарю в `xray.binary_path`.
- Файл с upstream-прокси, по умолчанию `proxy.txt`.

## Как Это Работает

`xpool` не проксирует трафик сам. Он управляет Xray и держит вокруг него control loop:

1. Читает YAML-конфиг и файл `source.file` со списком upstream-прокси.
2. Парсит только валидные `socks5h://user:pass@host:port` строки, а невалидные пропускает и считает отдельно.
3. Генерирует `xray.generated_path` с несколькими inbound-ами и outbound-ами.
4. Проверяет сгенерированный config через `xray run -test -config`, чтобы не стартовать Xray с битой конфигурацией.
5. Запускает Xray и ждет готовности локального Xray API.
6. Запускает status API на `status.address`.
7. Запускает health pool: он батчами проверяет каждый outbound через отдельный локальный check inbound.
8. Когда появляется хотя бы один ready proxy, controller включает его через Xray balancer override.
9. Дальше controller делает плановую ротацию по `runtime.rotation_interval` и failover, если текущий proxy перестал быть ready.

Схема трафика:

```text
client -> xpool/Xray HTTP inbound -> Xray balancer -> selected SOCKS5 upstream -> target site
```

Схема health check:

```text
xpool health worker -> local Xray check inbound -> конкретный Xray outbound -> health.check_urls
```

Поэтому проверяется не просто TCP-доступность upstream-прокси, а реальный путь, которым потом пойдет пользовательский трафик через Xray.

## Как Использовать

Обычный сценарий:

1. Установите `xray` или укажите путь к бинарю в `xray.binary_path`.
2. Создайте YAML-конфиг через `--save-config` или отредактируйте `xpool.yaml`.
3. Создайте локальный `proxy.txt` со списком upstream-прокси.
4. Задайте `HTTP_USERNAME` и `HTTP_PASSWORD` для внешнего HTTP inbound.
5. Запустите `xpool run`.
6. Подключайте клиентов к `http://0.0.0.0:8080` с указанным username/password.
7. Для локального использования на этой же машине можно использовать `http://127.0.0.1:8000` без auth.

Минимальный `proxy.txt`:

```text
socks5h://username:password@example.com:1080
```

Проверить, что сервис готов:

```bash
curl -f http://127.0.0.1:18080/healthz
```

Посмотреть подробное состояние:

```bash
curl http://127.0.0.1:18080/status
```

## Быстрый Старт

Сгенерировать дефолтный конфиг:

```bash
go run ./cmd/xpool --save-config
```

Запустить:

```bash
go run ./cmd/xpool run
```

С другим YAML-файлом:

```bash
go run ./cmd/xpool --config xpool.override.yaml run
```

Собрать бинарь:

```bash
task build
```

Проверить тесты:

```bash
task test
```

## CLI

CLI намеренно минимальный. Все runtime-настройки находятся в YAML.

```text
xpool [command] [--flags]
```

Команды:

- `run` — сгенерировать Xray config, запустить Xray и контроллер пула.

Флаги:

- `--config` — путь к YAML-конфигу, по умолчанию `xpool.yaml`.
- `--save-config` — сохранить дефолтный YAML-конфиг и выйти.
- `--log-level` — override для `log.level`: `debug`, `info`, `warn`, `error`.
- `--log-path` — override для `log.path`, JSON-лог в файл.

Если `--config` не указан, приложение ищет `xpool.yaml`, затем `xpool.override.yaml`, затем использует `xpool.yaml` как путь по умолчанию.

## Конфигурация

Все runtime-параметры задаются через YAML. CLI оставлен минимальным специально: так проще хранить рабочий профиль, повторять запуск и в будущем подключить динамические источники прокси без разрастания флагов.

Пример `xpool.yaml`:

```yaml
log:
  level: info
  path: ""
source:
  file: proxy.txt
xray:
  binary_path: xray
  generated_path: config.json
  api_address: 127.0.0.1:10085
  generated_log_level: warning
  ping_timeout: 5s
  sampling: 3
status:
  address: 127.0.0.1:18080
runtime:
  rotation_interval: 1m0s
  startup_timeout: 30s
  failover_cooldown: 5s
health:
  check_urls:
    - https://web.telegram.org/js/app.js
  check_interval: 1m0s
  ready_ttl: 10m0s
  timeout: 3s
  concurrency: 32
  jitter: 5s
  max_bytes: 10485760
  ready_successes: 1
```

### Секции

- `log` — уровень логирования и опциональный JSON-лог в файл.
- `source.file` — путь к файлу со списком upstream-прокси.
- `xray.binary_path` — путь к бинарю Xray.
- `xray.generated_path` — куда записывать сгенерированный Xray config.
- `xray.api_address` — локальный Xray API address.
- `xray.generated_log_level` — loglevel внутри сгенерированного Xray config.
- `xray.ping_timeout` и `xray.sampling` — параметры Xray burst observatory.
- `status.address` — адрес always-on status API.
- `runtime.rotation_interval` — период плановой ротации ready-прокси.
- `runtime.startup_timeout` — общий timeout ожидания Xray API и первичного ready pool.
- `runtime.failover_cooldown` — минимальная пауза между failover-попытками.
- `health.check_urls` — URL-ы для full-download проверки через каждый прокси.
- `health.check_interval` — период батчевой перепроверки active routes.
- `health.ready_ttl` — максимальный возраст успешной проверки, при котором прокси считается ready.
- `health.timeout` — timeout HTTP-запроса health check.
- `health.concurrency` — максимум одновременных проверок в батче.
- `health.jitter` — deterministic jitter перед проверкой route.
- `health.max_bytes` — максимальный размер скачиваемого тела на check URL; `0` отключает лимит.
- `health.ready_successes` — сколько успешных проверок подряд нужно для статуса ready.

### Поля Конфига

| Поле | Зачем нужно | Что отражает |
| --- | --- | --- |
| `log.level` | Управляет подробностью логов `xpool`. | Чем ниже уровень, тем больше диагностической информации. |
| `log.path` | Включает дополнительный JSON-лог в файл. | Полезно для долгого запуска и последующего разбора ошибок. |
| `source.file` | Указывает файл со списком upstream-прокси. | Источник прокси для текущего запуска. |
| `xray.binary_path` | Указывает, какой бинарь Xray запускать. | Можно использовать системный `xray` или локальный бинарь. |
| `xray.generated_path` | Куда писать generated Xray config. | Файл содержит credentials и не должен коммититься. |
| `xray.api_address` | Локальный адрес Xray API. | Через него `xpool` читает balancer state и делает override. |
| `xray.generated_log_level` | Уровень логов внутри generated Xray config. | Не влияет на логи самого `xpool`. |
| `xray.ping_timeout` | Timeout для Xray burst observatory. | Используется Xray при оценке latency outbounds. |
| `xray.sampling` | Sampling для Xray observatory. | Сколько измерений Xray использует для оценки outbound. |
| `status.address` | Адрес status API. | На нем доступны `/healthz` и `/status`. |
| `runtime.rotation_interval` | Период плановой смены ready-прокси. | Чем меньше значение, тем чаще будет переключение между healthy upstream. |
| `runtime.startup_timeout` | Сколько ждать Xray API и первый ready pool при старте. | Защита от бесконечного запуска без рабочих прокси. |
| `runtime.failover_cooldown` | Минимальная пауза между failover-попытками. | Защита от частого дергания Xray override при нестабильной сети. |
| `health.check_urls` | URL-ы, которые скачиваются через каждый proxy route. | Чем ближе URL к реальному нужному сервису, тем полезнее проверка. |
| `health.check_interval` | Период между батчами проверок. | Регулирует частоту перепроверки active routes. |
| `health.ready_ttl` | Сколько времени успешный check считается свежим. | Если success старше TTL, proxy перестает быть ready. |
| `health.timeout` | Timeout одного HTTP health request. | Ограничивает зависание на медленных или мертвых прокси. |
| `health.concurrency` | Максимум одновременных проверок в батче. | Главный лимит нагрузки при большом количестве прокси. |
| `health.jitter` | Детерминированная задержка перед проверкой route. | Размазывает проверки по времени, чтобы не ударять по сети одним пиком. |
| `health.max_bytes` | Максимум байт, который можно скачать с check URL. | Защищает от слишком больших ответов; `0` отключает лимит. |
| `health.ready_successes` | Сколько успешных проверок подряд нужно для ready. | Повышает устойчивость против случайных единичных успехов. |

### Как Подбирать Значения

Для большого списка прокси обычно важнее всего эти поля:

- `health.concurrency` — начните с `32` или `64`; увеличивайте только если машина, сеть и Xray справляются.
- `health.check_interval` — чем больше список, тем больше должен быть interval, иначе проверки будут идти почти постоянно.
- `health.jitter` — держите включенным, чтобы проверки распределялись во времени.
- `health.max_bytes` — оставляйте лимит, чтобы full-download check не превращался в большой расход трафика.
- `runtime.failover_cooldown` — увеличивайте, если в логах видно частые failover-попытки.
- `health.ready_successes` — ставьте `2` или `3`, если прокси часто дают случайный успешный ответ, а потом сразу ломаются.

## Формат Прокси

Файл `source.file` содержит один proxy URL на строку:

```text
socks5h://username:password@example.com:1080
socks5h://username:password@192.0.2.10:1080
```

Поддерживается только схема `socks5h://user:pass@host:port`.

Пустые строки и строки с `#` в начале игнорируются. Невалидные строки не валят загрузку всего файла: они пропускаются, их количество и первые ошибки видны в `/status`.

## Переменные Окружения

Сгенерированный внешний HTTP inbound использует auth из переменных:

```bash
export HTTP_USERNAME='user'
export HTTP_PASSWORD='pass'
```

Если переменные не заданы, в Xray config попадут placeholder-значения `HTTP_USERNAME` и `HTTP_PASSWORD`.

## Runtime

При запуске `xpool run` выполняет шаги:

1. Загружает YAML-конфиг.
2. Загружает и парсит `source.file`, пропуская невалидные proxy URL.
3. Генерирует Xray config в `xray.generated_path`.
4. Валидирует config через `xray run -test -config`.
5. Запускает Xray.
6. Ждет готовности Xray API.
7. Запускает status API.
8. Запускает health pool и ждет первый ready proxy.
9. Переключает balancer на ready proxy и дальше выполняет rotation/failover.

## Status API И Метрики

Status API всегда включен и слушает `status.address`.

Проверка готовности:

```bash
curl -f http://127.0.0.1:18080/healthz
```

Полный статус:

```bash
curl http://127.0.0.1:18080/status
```

`/healthz` возвращает `200 ok`, только если есть ready pool и Xray сейчас serving. В остальных случаях возвращает `503`. Этот endpoint нужен для простых health checks в systemd, Docker, reverse proxy или внешнем watchdog.

`/status` возвращает JSON. Это не Prometheus endpoint, но его поля являются operational metrics: их можно читать через `curl`, `jq`, скрипты мониторинга или любой JSON collector.

Примеры:

```bash
curl -s http://127.0.0.1:18080/status | jq
curl -s http://127.0.0.1:18080/status | jq '.healthy, .serving, .pool.ready, .pool.retired'
curl -s http://127.0.0.1:18080/status | jq '.source.invalid_count'
curl -s http://127.0.0.1:18080/status | jq '.pool.states[] | select(.retired == true)'
```

### Верхний Уровень `/status`

| Поле | Зачем нужно | Что отражает |
| --- | --- | --- |
| `healthy` | Быстро понять, есть ли хотя бы один usable proxy. | `true`, если `pool.ready > 0`. |
| `serving` | Понять, обслуживает ли Xray трафик через ready proxy прямо сейчас. | `true`, если controller уже выбрал current outbound и он все еще ready. |
| `started_at` | Зафиксировать время старта процесса. | UTC timestamp запуска controller. |
| `uptime` | Смотреть, как долго процесс работает без рестарта. | Продолжительность с `started_at`. |
| `xray_api_ready` | Проверить связь `xpool` с Xray API. | `true` после успешного ожидания API на старте. |
| `xray_pid` | Найти конкретный процесс Xray. | PID дочернего Xray-процесса. |
| `current` | Узнать, какой outbound выбран сейчас. | Tag вида `socks-1`, `socks-2`. |
| `balancer_tag` | Проверить управляемый Xray balancer. | Обычно `proxy-balancer`. |
| `rotation_interval` | Видеть активный интервал ротации. | Значение из YAML после defaults/validation. |
| `failover_cooldown` | Видеть cooldown failover. | Значение из YAML после defaults/validation. |
| `next_rotation_at` | Понять, когда ожидается следующая плановая ротация. | `last_selected_at + rotation_interval`, если proxy уже выбран. |

### `source`

| Поле | Зачем нужно | Что отражает |
| --- | --- | --- |
| `source.name` | Понять, откуда загружен список. | Обычно путь к `source.file`. |
| `source.loaded_at` | Видеть время загрузки source. | UTC timestamp чтения файла. |
| `source.proxy_count` | Оценить размер рабочего пула на старте. | Количество валидных proxy URL после парсинга. |
| `source.invalid_count` | Найти проблемы с качеством списка. | Количество строк, которые были пропущены как невалидные. |
| `source.invalid_errors` | Быстро увидеть примеры ошибок. | Первые parse errors с номером строки и причиной. |

`invalid_count` особенно важен для будущих подписок: большой список может содержать много URL не того формата, но это не должно валить весь запуск.

### `pool`

| Поле | Зачем нужно | Что отражает |
| --- | --- | --- |
| `pool.total` | Видеть общий размер health pool. | Количество валидных routes, созданных из proxy source. |
| `pool.ready` | Главная метрика доступности. | Сколько routes сейчас могут принимать трафик. |
| `pool.retired` | Понять, сколько прокси окончательно исключено. | Количество routes, проваливших full-download check. |
| `pool.ready_ttl` | Проверить freshness policy. | Максимальный возраст успешной проверки для ready. |
| `pool.check_urls` | Убедиться, что проверяются правильные targets. | Список URL из YAML. |
| `pool.check_concurrency` | Контролировать нагрузку health checker. | Максимум одновременных проверок в батче. |
| `pool.check_jitter` | Проверить распределение проверок по времени. | Максимальная deterministic задержка перед route check. |
| `pool.check_max_bytes` | Контролировать лимит скачивания. | Максимальный размер тела ответа health URL. |
| `pool.ready_successes` | Видеть требование к стабильности route. | Сколько success подряд нужно для ready. |
| `pool.states` | Разобрать состояние каждого proxy route. | Детальные per-route метрики. |

Практическая интерпретация:

- `pool.ready == 0` — трафик безопасно обслуживать нечем; `/healthz` будет `503`.
- `pool.retired` быстро растет — source содержит много мертвых прокси или check URL недоступен через них.
- `pool.ready` сильно скачет — стоит увеличить `ready_successes`, `failover_cooldown` или проверить качество прокси.
- `pool.total - pool.retired` показывает, сколько routes еще участвует в будущих проверках.

### `pool.states[]`

| Поле | Зачем нужно | Что отражает |
| --- | --- | --- |
| `tag` | Связать status с Xray outbound. | Tag вида `socks-N`. |
| `alive` | Понять результат последней проверки. | `true`, если последний full-download check был успешным. |
| `ready` | Понять, может ли route быть выбран controller-ом. | `alive`, не retired, success свежий и success подряд достаточно. |
| `last_success` | Видеть свежесть успешной проверки. | UTC timestamp последнего success. |
| `last_error` | Диагностировать причину сбоя. | Последняя ошибка HTTP/download/status через этот route. |
| `duration` | Оценить скорость health check. | Время полного скачивания всех `check_urls` для route. |
| `consecutive_failures` | Видеть серию сбоев. | В текущей логике route retire-ится после первого full-download failure. |
| `consecutive_successes` | Видеть накопленную стабильность. | Сколько успешных проверок подряд было до текущего момента. |
| `retired` | Проверить, исключен ли route из будущих батчей. | `true`, если route больше не проверяется в этом запуске. |
| `retired_at` | Понять, когда route был исключен. | UTC timestamp retire. |
| `retired_reason` | Понять, почему route исключен. | Ошибка, из-за которой full-download check провалился. |

`retired` нужен, чтобы не тратить ресурсы на заведомо плохие прокси. Это особенно важно для больших подписок: один раз не прошедший проверку route удаляется из active pool и не занимает worker-ы в следующих батчах.

### `switches`

| Поле | Зачем нужно | Что отражает |
| --- | --- | --- |
| `switches.rotations` | Видеть плановые переключения. | Сколько раз controller сменил proxy по rotation interval. |
| `switches.failovers` | Видеть аварийные переключения. | Сколько раз controller переключился из-за неготовности current proxy. |
| `switches.failures` | Отлавливать проблемы управления Xray. | Количество неудачных попыток balancer override. |
| `switches.last_reason` | Понять причину последнего успешного выбора. | `startup`, `rotation` или `failover`. |
| `switches.last_selected_at` | Видеть время последнего выбора proxy. | UTC timestamp последнего successful override. |
| `switches.last_rotation_at` | Видеть последнюю плановую ротацию. | UTC timestamp последней rotation. |
| `switches.last_failover_at` | Видеть последний аварийный failover. | UTC timestamp последнего failover. |
| `switches.last_error` | Диагностировать последний failure. | Текст ошибки override или empty, если последняя операция успешна. |
| `switches.last_error_reason` | Понять контекст failure. | На каком этапе случилась ошибка: `startup`, `rotation`, `failover`. |

Практическая интерпретация:

- `failovers` растет часто — current proxy регулярно теряет ready state.
- `failures > 0` вместе с `last_error` — проблема не в прокси, а в управлении Xray/API или в отсутствии ready candidates.
- `last_reason=startup` и `serving=true` — сервис запустился и выбрал первый proxy, но плановой ротации еще не было.

### Что Считать Метриками

Для мониторинга обычно достаточно вытаскивать эти значения:

- availability: `healthy`, `serving`, `pool.ready`;
- pool quality: `pool.total`, `pool.retired`, `source.invalid_count`;
- stability: `switches.rotations`, `switches.failovers`, `switches.failures`;
- freshness: `pool.states[].last_success`, `next_rotation_at`, `uptime`;
- diagnostics: `pool.states[].last_error`, `pool.states[].retired_reason`, `switches.last_error`.

Минимальная shell-проверка для watchdog:

```bash
curl -fsS http://127.0.0.1:18080/healthz >/dev/null
```

Пример получения численных метрик через `jq`:

```bash
status_url=http://127.0.0.1:18080/status
curl -s "$status_url" | jq '{
  healthy,
  serving,
  ready: .pool.ready,
  retired: .pool.retired,
  invalid: .source.invalid_count,
  failovers: .switches.failovers,
  switch_failures: .switches.failures
}'
```

## Health Checks

Health check идет не напрямую в upstream, а через сгенерированные локальные Xray inbounds вида `127.0.0.1:18001`, `127.0.0.1:18002` и далее. Это проверяет реальный путь через Xray и конкретный outbound.

Проверка считается успешной, если все `health.check_urls` отдали HTTP `2xx` или `3xx`, тело скачалось полностью и не превысило `health.max_bytes`.

Если route получил ошибку health check, он помечается `retired`, удаляется из active pool и больше не проверяется в текущем запуске.

## Безопасность Файлов

Не коммитьте и не публикуйте:

- `proxy.txt` и любые файлы со списками прокси;
- сгенерированный `config.json`, потому что там могут быть credentials;
- логи, если они могут содержать инфраструктурные детали.

Эти файлы должны оставаться локальными.

## Разработка

Форматирование и тесты:

```bash
gofmt -w ./cmd ./internal ./pkg
go test ./...
```

Сборка без Task:

```bash
go build -o /tmp/opencode/xpool-check ./cmd/xpool
```

Через Task:

```bash
task build
task test
task run
```
