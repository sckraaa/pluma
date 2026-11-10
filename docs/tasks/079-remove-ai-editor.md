# 079. ИИ-редактор убрать из клиентов

- Статус: todo
- Решение владельца 2026-11-11: ИИ-редактор сообщений убрать из всех четырёх клиентов (модели у Pluma нет).

## Что сделать
Патчами клиентов убрать вход в редактор и его экраны: Android `AIEditorAlert`, iOS вызовы `Translate.swift` для `composeMessageWithAI`, Desktop `create_ai_box` и `api_compose_with_ai`, Web соответствующие кнопки. Точные места — `docs/project/feature-registry.md`, пространство `aicompose` и `messages.compose*WithAI`.

## Результат
_Заполняется по завершении._
