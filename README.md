# dummi-updater

Последовательно обновляет Git-репозитории и выполняет
команду из обязательного поля `run` в каждом target.

## Требования

- Linux или macOS (Intel / Apple Silicon).
- Git и доступ к репозиториям через SSH-ключи или credential helper.
- Запущенный Docker и команда `docker compose`.
- Compose-файл в корне каждого целевого репозитория.
- Go 1.22+ только для сборки и pre-commit; для готовых бинарников не нужен.

## Настройка

1. Заполните `update-items.json`: `repo` — URL для клонирования,
   `target` — директория проекта, `run` — непустая строка команды запуска.
   Все три поля обязательны. Добавляйте нужное количество элементов.
   Для существующего проекта `repo` должен точно совпадать с
   `git remote get-url origin`.
2. Установите Git, Docker и Compose plugin на Linux или macOS.
3. Запустите `bash update.sh` или нужный бинарник:
   `./dist/updater-linux-amd64 -config ./update-items.json`.

Пример `update-items.json` для двух проектов:

```json
{
  "items": [
    {
      "repo": "git@github.com:YOUR_USER/backend.git",
      "target": "/srv/backend",
      "run": "docker compose up -d --build --remove-orphans"
    },
    {
      "repo": "git@github.com:YOUR_USER/frontend.git",
      "target": "/srv/frontend",
      "run": "docker compose up -d --build --remove-orphans"
    }
  ]
}
```

Замените URL и пути своими значениями. Ключ называется `repo`, без двоеточия.
На macOS используйте доступные вам пути, например `/Users/your-user/projects/backend`.

## Запуск

```sh
# Автоматический выбор бинарника и JSON из директории скрипта
bash update.sh

# Другая конфигурация
bash update.sh -config /absolute/path/update-items.json

# Прямой запуск, например на Apple Silicon
./dist/updater-darwin-arm64 -config ./update-items.json
```

`update.sh` автоматически выбирает Linux/macOS и amd64/arm64.
Для macOS доступны `dist/updater-darwin-amd64` (Intel) и
`dist/updater-darwin-arm64` (Apple Silicon).

Без `-config` JSON ищется рядом с бинарником. Относительные target
разрешаются относительно JSON. Для standalone-установки скопируйте
бинарник и JSON в одну директорию.

## Поведение обновления

Отсутствующий target клонируется. Существующий обновляется через
`git pull --ff-only` (текущая ветка и её upstream).
Локальные изменения, включая untracked-файлы, приводят к пропуску проекта.
Ошибки не останавливают остальные проекты; итоговый код выхода — 1.
Команда `run` выполняется через `sh -c` в каталоге `target`, даже без новых
коммитов, чтобы повторить неудачный запуск. Поддерживаются кавычки, переменные
окружения и shell-операторы (`&&`, `|` и другие). Команды `docker compose ps`
после запуска и `docker compose logs --tail=30` при ошибке остаются фиксированными.
Для `docker compose up` это не безусловный restart: неизменившиеся контейнеры
Compose может оставить работающими.

Ошибки обновления проектов записываются в `error.log` рядом с JSON
и одновременно выводятся в stderr с путём проекта и причиной ошибки.
Информационные сообщения, предупреждения и вывод команд остаются в консоли.
Блокировка не допускает параллельного запуска
с одним и тем же JSON и освобождается ОС при завершении. Не запускайте разные
конфигурации с пересекающимися target одновременно.
Используйте только доверенные репозитории и JSON: `run` выполняет произвольные
shell-команды с правами пользователя updater, а Compose запускает код репозитория.
Не помещайте токены в URL репозитория; используйте SSH-ключи или Git credential helper.

## Telegram

Telegram опционален: переменные окружения `TELEGRAM_ENABLED=true`,
`TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`. Уведомления отправляются после
успешного обновления/клонирования, без HTML-разметки.

## Сборка и pre-commit

Нужен Go 1.22+. В каждом новом клоне включите hook:

```sh
git config core.hooksPath .githooks
```

При коммите hook проверяет тесты и собирает **staged-версию** исходников:
`dist/updater-{linux,darwin}-{amd64,arm64}`, затем добавляет все четыре
бинарника в коммит. Перед первым коммитом добавьте исходники в index.
Docker тестами и hook не запускается.

## Проверки

```sh
go test ./...
go vet ./...
```

Текущие тесты проверяют чтение и валидацию JSON, выполнение `run` в target
(включая повторный запуск и ошибки) и запись только ошибок в лог; Docker не запускают.
Работу Compose проверяйте отдельно в своём окружении.

## Ручная сборка

```sh
mkdir -p dist
for os in linux darwin; do
  for arch in amd64 arm64; do
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags="-s -w" -o "dist/updater-$os-$arch" .
  done
done
```
