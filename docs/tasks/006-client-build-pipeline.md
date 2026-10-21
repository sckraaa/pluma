# 006. Конвейер сборки клиентов

- Статус: todo
- Зависит от: 001 (новый RSA-ключ — до этого собирать на временном тестовом ключе)
- ADR: 0001, 0006, 0008

## Цель
Каждый клиент собирается одной командой/кнопкой, с кэшем, с проверками в начале; повтор после ошибки — минуты, а не час.

## Область
По `docs/audit/04-clients-and-builds.md`:
- Отдельный репозиторий (или каталог `clients/`) с патчами поверх тегов upstream: iOS `release-12.9.2`, Android 12.9.1 (`aac1efcb83`), tdesktop v7.0.x, Web K/A — найти коммиты на layer 228.
- Патч: адреса серверов (`pluma.chat`), публичный RSA-ключ, api_id/api_hash, ID приложений `chat.pluma.*`, push-ключи.
- Preflight за секунды: версии Xcode/NDK, патчи применились, layer = 228, отпечаток RSA-ключа, профили для всех iOS-целей и App Group, keystore.
- Self-hosted runners GitHub Actions: Mac (iOS, `Make.py --cacheDir --cacheHost`), Ryzen-ПК (Android в Docker с ccache и Gradle cache, tdesktop Linux, bazel-remote как кэш для iOS). Web — на GitHub-раннерах.
- Подпись отдельным шагом после компиляции.
- Порядок: Android → Web → iOS → Desktop.

## Критерии приёмки
- Каждый клиент подключается к локальному стенду и проходит вход.
- Повторная сборка без изменений с тёплым кэшем: iOS ≤10 мин, Android ≤10 мин.
- Намеренно сломанный профиль подписи → падение на preflight за минуту, а не в конце.

## Результат
—
