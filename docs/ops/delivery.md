# Доставка: что делать по алертам

Как устроена доставка — ADR 0012. Обслуживание `mq_outbox` (`pluma-admin outbox list|replay|discard`), обновление и откат — `docs/ops/user-state.md`. Правила алертов — `teamgramd/deploy/prometheus/server/rules/delivery.yml`.

Главное: событие, записанное в `mq_outbox`, не теряется ни при падении сервиса, ни при недоступности Kafka. Пока что-то сломано, сообщения задерживаются; после починки они доставляются сами, по порядку и один раз. Устройство, пропустившее push, догоняет через `updates.getDifference`.

| Алерт | Что значит | Что делать |
|---|---|---|
| `DeliveryServiceDown` | msg, sync или session не отвечает на scrape | `docker compose ps`, `docker compose logs --tail=200 <сервис>`. Docker перезапускает упавший сервис сам; если он в цикле перезапусков — причина в логе (часто MySQL, etcd или ошибка конфига) |
| `DeliveryRelayDown` | нет лидера relay: msg не запущен или не получил блокировку | проверить msg; если msg жив — `SELECT IS_USED_LOCK(...)` в MySQL показывает, кто держит блокировку relay |
| `DeliveryKafkaDown` | relay не достучался до брокера | `docker compose ps kafka`, логи брокера, место на диске. События копятся в `mq_outbox` и уйдут после восстановления |
| `DeliveryTopicNotReady` | топика нет или у него не 16 партиций | `teamgramd/deploy/kafka/topics.sh` (в compose его выполняет `kafka-init`) |
| `DeliveryOutboxBacklog` | события старше 30 с не применены | смотреть, какой из алертов выше тоже горит; если ни один — `pluma-admin outbox list -state pending` и метрики консьюмера |
| `DeliveryDeadEvents` | события в состоянии dead или parked | `pluma-admin outbox list -state dead`; ошибка — в логе msg по `id`. Исправить причину, затем `outbox replay -user <id>`. Relay и сам повторяет такие полосы: через 15 мин и дальше до 4 ч |
| `DeliveryConsumerStuck` | консьюмер больше 2 мин повторяет одно сообщение (временная ошибка) | лог msg или sync: обычно недоступна MySQL или дедлок; после устранения продолжит сам |
| `DeliveryPartitionsUnassigned` | группа владеет не всеми 16 партициями | перезапуск msg или sync; если не помогает — `kafka-consumer-groups.sh --describe --group <группа>` |
| `DeliveryLag`, `DeliveryConsumerSlow` | консьюмер не успевает | нагрузка или медленная MySQL; смотреть `mq_consumer_event_age_seconds`, `userbox_*` |
| `DeliveryRedelivered` | relay опубликовал события повторно (консьюмер пропустил копию или её нет в логе Kafka) | само по себе безопасно (применение один раз по строке); частые повторы — смотреть retention и offset-ы групп |
| `DeliveryPublishErrors` | брокер отверг сообщения не временной ошибкой | лог msg (relay): размер сообщения, конфиг топика |
| `DeliveryForeignEvents` | консьюмер получил событие, не совпавшее со строкой outbox | чужие сообщения в топике или пересозданный `mq_outbox` (запрещено); лог msg |
| `PushDropped` | push не дошёл до устройства | устройство получило `updatesTooLong` и догнало через `getDifference`; часто — устройство офлайн или перезапускалась session |

**Новое inbox-событие** (метод, которого старый msg не знает, например `inbox.seqUpdatesToInbox`, ADR 0021): msg выкатывается раньше писателей в biz или вместе с ними. Старый msg делает такое событие dead сразу, и полоса получателя стоит до автоповтора нового msg.

**Остановка и перезапуск.** Каждый сервис — отдельный сервис compose: `docker compose restart <сервис>` безопасен в любой момент, транзакция ящика либо закоммичена вместе со своими событиями, либо откатится. `stop_grace_period` в `docker-compose.yaml` даёт сервису закончить начатое; при перезагрузке хоста systemd ждёт остановки Docker не дольше `TimeoutStopSec` (по умолчанию 90 с) — данные целы в любом случае.
