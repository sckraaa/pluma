# Качество звонков уровня Discord без глобального флота серверов

Дата: 2026-10-21. Продолжает `docs/audit/04-clients-and-builds.md` §3 и ADR 0004.
Метки: **[проверено]** — по исходникам или официальной документации; **[вероятно]** — по нескольким косвенным источникам; **[оценка]** — инженерная оценка; **[не проверено]** — требует теста.

Исходники tgcalls смотрелись в двух версиях: коммит `2faee3b5` (2026-07-11, на нём закреплён tdesktop v7.0.9) и ветка `development` (октябрь 2026). Закреплённые версии tgcalls в iOS 12.9.2 и Android 12.9.1 построчно не сверялись. Но одни и те же лимиты есть и в июльской, и в октябрьской версии, так что, скорее всего, они совпадают.

---

## 0. Главное

1. **Качество Discord складывается из трёх вещей:**
   - **Бюджет битрейта.** Голос 64 кбит/с по умолчанию, до 384 с бустами; демонстрация экрана 720p30, с Nitro 1080p60.
   - **Сетевой путь.** Один SFU на звонок, хороший пиринг. Переезд на Cloudflare дал во Франкфурте −34% пинга и −42% потерь.
   - **Адаптация к каналу каждого зрителя.** Поток подстраивается под самого медленного зрителя; используются аппаратные кодеки.

   Секретного кодека нет [проверено: посты Discord 2018, 2024, 2026].
2. **Telegram хуже прежде всего из-за жёстких лимитов в tgcalls** [проверено по коду]:
   - 1:1: голос ≤32 кбит/с с кадрами 60 мс; видео ≤1–1.2 Мбит/с, и демонстрация экрана тоже.
   - Группа: голос фиксированно 32 кбит/с с кадрами **120 мс**; общий потолок отправки ≈**1.05 Мбит/с** и для камеры, и для экрана; слой 720p ≤0.9 Мбит/с.

   Это главный и самый дешёвый рычаг, но он требует патча клиента.
3. **«Близость к пользователю» без флота получается так:**
   - для 1:1 — P2P в первую очередь, а при неудаче anycast-TURN от Cloudflare (1 ТБ/мес бесплатно, дальше $0.05/ГБ);
   - для групп — один хорошо связанный SFU (Discord тоже держит ровно один SFU на звонок);
   - при необходимости — один дешёвый L4-форвардер рядом с удалённым кластером пользователей; его добавляет сервер, клиенты не меняются.
4. **Чего стоковые клиенты НЕ умеют** [проверено по коду]:
   - TURN через TCP/TLS для `phoneConnectionWebrtc`;
   - TURN в групповых звонках;
   - hostname вместо IP в ICE-серверах у ReferenceImpl старой версии;
   - AV1 в групповом движке.

   Поэтому пункт ADR 0004 «обязательно TURN over TLS на 443» без патча невыполним.
5. **Cloudflare Realtime SFU с tgcalls несовместим**: у него свой API сессий и треков с SDP offer/answer, а tgcalls ждёт свой join-JSON и Colibri. Cloudflare TURN — обычный TURN и работает [вероятно; UDP по коду, звонками не проверено].
6. **Деньги — не проблема.** При ≤100 пользователях трафик SFU 0.15–5.4 ТБ/мес укладывается в 20 ТБ, включённых в тариф VPS. Итого около $0–15/мес сверх основного VPS.

---

## 1. Что делает звонки Discord хорошими (публичные инженерные посты)

| Факт | Источник |
|---|---|
| Собственный SFU: в 2018 году на C++, в 2026 — на Rust/Tokio, 8 воркеров, у каждого свой UDP-сокет | [2018](https://discord.com/blog/how-discord-handles-two-and-half-million-concurrent-voice-users-using-webrtc), [2026](https://discord.com/blog/how-we-moved-discord-voice-to-the-edge) |
| **Один SFU на весь звонок**: все участники подключаются к одному хосту | 2026 |
| 2018: более 850 голосовых серверов, 13 регионов, 30+ ДЦ. До 2025: около 30 городов у гиперскейлеров. 2025–2026: перенос на edge Cloudflare (300+ городов), сейчас там более 80% трафика | 2018, 2026 |
| Франкфурт: пинг −34%, потери −42% после смены площадки. В Европе и Латинской Америке потери −20…60%. Выигрыш дал **пиринг**, а не кодек | 2026 |
| Исландия: «локальный» SFU улучшил местные звонки, но для иностранных участников пинг вырос в 2.7 раза. Вывод: **размещение по составу звонка, а не «поближе к кому-то одному»** | 2026 |
| Роттердам: перегруженный транзит через Telia дал задержки больше 1 с в часы пик. Вывод: проверять пиринг до запуска | 2026 |
| Потери 1.5–2% из-за очередей NIC; всплески пинга из-за softirq на одном vCPU — лечили CPU affinity и RPS | 2026 |
| В тишине пакеты не отправляются; нативный клиент с упрощённым сигналингом (~1 КБ вместо ~10 КБ SDP) | 2018 |
| Go Live: VP8 и H.264, на части платформ HEVC и AV1; аппаратное кодирование; разные оценщики полосы под разные сценарии; стример не шлёт больше, чем выдержит самый медленный зритель; метрики VMAF | [2024](https://discord.com/blog/how-it-all-goes-live-an-overview-of-discords-streaming-technology) |
| Голос 64 кбит/с по умолчанию, бусты: 128/256/384 кбит/с | [Audio Bitrate FAQ](https://support.discord.com/hc/en-us/articles/11635925354775-Audio-Bitrate-FAQ) [вероятно: страница отдала 403, данные из выдачи поиска] |
| Go Live: бесплатно 720p30, Nitro — 1080p60 (по некоторым источникам до 4K) | сторонние источники [не проверено] |

**Вывод для нас.** При смешанной географии топология Discord — один SFU на звонок и путь через публичный интернет до него — не лучше нашей «один SFU в хорошем ДЦ». Discord выигрывает в двух случаях: в локальных звонках в далёких регионах (там у него местный SFU) и за счёт пиринга. Остальное — битрейты и адаптация — мы можем повторить.

---

## 2. Почему звонки Telegram часто хуже: что видно в tgcalls [проверено]

| Где | Значение | Файл (коммит `2faee3b5`) |
|---|---|---|
| 1:1, голос | Opus ≤ **32 кбит/с**, кадр **60 мс**, in-band FEC | `v2/InstanceV2Impl.cpp` стр. 178–179, 2163 |
| 1:1, видео и экран | ≤ **1000 кбит/с** (V2Impl), ≤ **1200 кбит/с** (ReferenceImpl) | `InstanceV2Impl.cpp` 2166; `InstanceV2ReferenceImpl.cpp` 1454 |
| 1:1, транспорт | `enableTCP = false` (ICE-TCP выключен); TURN только UDP или TCP, **TLS нет**; ReferenceImpl пропускает `isTcp`-серверы и отбрасывает hostname | `InstanceV2Impl.cpp` 1060; `NativeNetworkingImpl.cpp` 626; `InstanceV2ReferenceImpl.cpp` 648, 653 |
| Группа, голос | Opus с **фиксированным** битрейтом (min = max = start = 32 кбит/с по умолчанию), кадр **120 мс**, FEC | `group/GroupInstanceCustomImpl.cpp` 2733–2744; `GroupInstanceImpl.h` `outgoingAudioBitrateKbit{32}` |
| Группа, камера | simulcast 180p ≤ 60, 360p ≤ 110, 720p ≤ 900 кбит/с | `GroupInstanceCustomImpl.cpp` 2618–2652 |
| Группа, экран | 3 слоя: ≤ 100 / 200 / 900 кбит/с, или один слой ≤ 1.8 Мбит/с | там же, 2578–2614 |
| Группа, общий потолок | `max_bitrate_bps = (1020+32)*1000` — **около 1.05 Мбит/с** и для камеры, и для экрана | там же, 3205–3207 |
| Группа, кодеки | только VP8, VP9, H.264 (AV1 нет) | там же, 219–225 |
| Группа, транспорт | **ни STUN, ни TURN**: ICE идёт только к кандидатам SFU из join response; поле `protocol` у кандидата разбирается, TCP не отключён | `group/GroupNetworkManager.cpp` 387–398; `GroupInstanceCustomImpl.cpp` 3874–3896 |

Сам upstream движется в ту же сторону: коммит `ce91d4ff` от 2026-09-15 «group: send 60 ms Opus frames from ReferenceImpl». В `tools/go_sfu/CLAUDE.md` прямо сказано, что ограничение `max_bitrate_bps` ≈ 1052 кбит/с может не давать дойти до 720p.

Что не проверено:
- география рефлекторов Telegram (документации нет);
- как настроен «боевой» SFU Telegram.

Отдельный фактор — блокировки звонков Telegram и WhatsApp в РФ с августа 2025 года ([Moscow Times](https://www.themoscowtimes.com/2026/08/06/roskomnadzor-says-its-restricting-whatsapp-telegram-calls)). На наш сервер они не распространяются, но общая DPI-среда влияет (см. риски).

---

## 3. Что стоковые клиенты умеют без патча

**1:1** (`phoneCall.connections`):
- `phoneConnectionWebrtc` превращается в STUN и TURN **только по UDP**. tdesktop не выставляет `isTcp` (`calls_call.cpp` стр. 134–166), а у TL-типа нет флага tcp.
- У закреплённой версии ReferenceImpl в `ip` должен быть **IP-литерал**: hostname отбрасывается. Пропуск hostname, по комментарию в коде нужный именно для Cloudflare TURN, появился только в ветке development в сентябре 2026.
- `phoneConnection` — протокол рефлектора Telegram (16-байтовый peer_tag), умеет **сырой TCP** (флаг `tcp`, `ReflectorPort.cpp`). Своего сервера рефлектора в открытом доступе нет, но клиентская сторона открыта. Написать его на Go реально [оценка: 1–2 недели]. Это единственный способ получить TCP-резерв для 1:1 без патча клиента.
- `p2p_allowed` решает сервер.
- `phoneCall.custom_parameters` (DataJSON) передаётся в tgcalls как `customParameters` — это **готовый канал удалённой настройки**. iOS и Android его передают (Android `VoIPService.java` стр. 3420), **tdesktop v7.0.9 — нет**. Сейчас там только сетевые флаги (`network_use_mtproto`, `network_standalone_reflectors`, `network_skip_initial_ping` и т. п.); битрейтов среди них нет.

**Групповые звонки** (`updateGroupCallConnection.params`, JSON формирует наш сервер или SFU):
- список кандидатов (IP, порт, протокол udp/tcp, приоритет);
- выбор payload types (VP8, VP9, H.264);
- RTCP-feedback: nack, pli, transport-cc;
- плюс сигналы Colibri по data-каналу (какой слой simulcast нужен получателю).

Битрейты отправителя и параметры Opus зашиты в клиенте.

---

## 4. Рычаги качества: сервер или патч клиента

### 4.1 Только сервер (клиенты стоковые)

| № | Рычаг | Эффект | Где |
|---|---|---|---|
| S1 | `p2p_allowed=true` для всех 1:1 + STUN (свой и Cloudflare) | Прямой путь, минимальная задержка, ноль серверного трафика | teamgram `phone.*` |
| S2 | TURN по UDP: Cloudflare anycast (IP-литералы A/AAAA от `turn.cloudflare.com`, порты 3478 и 443/udp, креды через `generate-ice-servers`, TTL ≈ 12 ч) + свой coturn | Работает за симметричным NAT; вход через ближайшую из 330+ точек Cloudflare | teamgram + небольшой сервис выдачи кредов |
| S3 | Свой рефлектор (протокол Telegram, UDP + TCP) | TCP-резерв для 1:1 без патча | новый Go-сервис [оценка] |
| S4 | SFU: один UDP-порт (UDPMux), IPv4+IPv6, правильный публичный IP, **passive ICE-TCP на 443** в кандидатах | Проходит через корпоративные и гостиничные сети | go_sfu (pion/ice v4: `NewUDPMuxDefault`, `NewTCPMuxDefault`) [на клиенте не проверено] |
| S5 | **Ответ на NACK из RTX-кэша SFU**. Сейчас `readRTCPLoop` обрабатывает только PLI, FIR и REMB | Главное средство против потерь на последней миле | go_sfu `participant.go` 419–464 |
| S6 | **Оценка канала до каждого получателя по transport-cc** (перенумерация TWCC под каждого получателя + GCC из `pion/interceptor/pkg/gcc`) и выбор слоя по ней | Сейчас выбор слоя опирается на REMB, а реальные клиенты его не шлют (по `CLAUDE.md` go_sfu). Без этого слабый получатель захлёбывается 720p | go_sfu `bandwidth.go` |
| S7 | Ограничение частоты PLI/FIR, ключевой кадр при смене слоя, плавная перенумерация seq/ts | Меньше «каши» и фризов | go_sfu |
| S8 | Пересылать только активных говорящих (расширение audio-level, id 1, или last-N) | Экономия downlink на мобильных сетях | go_sfu (сейчас «аудио всем безусловно», `sfu.go` 356) |
| S9 | Выбор кодека в join response (VP8 simulcast по умолчанию, H.264 для аппаратного кодирования), appConfig `confcallPrioritizeVP8`, `unmuted_video_limit` | Стабильность и нагрузка на CPU | teamgram |
| S10 | Площадка: ДЦ с хорошим пирингом; медиа отдельно от MTProto или с CPU pinning; sysctl UDP-буферов, RPS | Уроки Discord: потери 1.5–2% из-за очередей NIC, пинги из-за softirq | инфраструктура |
| S11 | **L4-форвардер** (DNAT+SNAT на дешёвом VPS рядом с кластером пользователей) как дополнительный кандидат в join response и coturn на нём же для 1:1. Сервер выбирает его по GeoIP | «Вход рядом с пользователем» без клиентского патча и без SFU в регионе | один VPS по требованию |
| S12 | Метрики: RTCP RR (потери, jitter, RTT) по участникам в Prometheus; критерии качества из ADR 0004 | Замеры вместо ощущений | go_sfu + Grafana |

### 4.2 Патч tgcalls: один C++-код для iOS, Android и tdesktop

Патч кладётся в три копии: iOS `submodules/TgVoipWebrtc/tgcalls`, Android `TMessagesProj/jni/voip/tgcalls`, tdesktop `Telegram/ThirdParty/tgcalls`.

| № | Изменение | Зачем |
|---|---|---|
| C1 | **Лимиты — в серверные параметры.** Для 1:1 через `customParameters` (например, `audio_max_kbps`, `video_max_kbps`, `opus_ptime_ms`, `opus_red`, `turn_tls`), для групп — через дополнительные поля join response. Значения по умолчанию = текущие | Один раз пересобрать клиенты, дальше настраивать на сервере |
| C2 | Opus: кадр 60/120 → **20 мс**; группа: фиксированные 32 → адаптивные 24–64 кбит/с; 1:1 до 48–64; FEC оставить; по желанию RED (`AudioEncoderCopyRed` в libwebrtc, согласовать `red` в ContentNegotiation и в групповом описании; SFU пересылает прозрачно) | Задержка и устойчивость к потерям. Кадр 120 мс = +100 мс и потеря 120 мс речи на один пакет [оценка] |
| C3 | Видео: 1:1 → 2.5–4 Мбит/с; группа: общий потолок → 3–4 Мбит/с для камеры, 4–6 Мбит/с для экрана; слои ≈150/450/1500–2000 кбит/с; экран — 1080p, 15–30 fps, приоритет разрешения над fps | Уровень Go Live. **Только после S5–S6** |
| C4 | TURN через TCP/TLS: добавить `PROTO_TLS`, разрешить `isTcp` в ReferenceImpl, разрешить hostname (для проверки сертификата `turn.cloudflare.com`). Тип транспорта выводить по соглашению о портах **внутри tgcalls** (443 и 5349 → turns, 80 → tcp), чтобы не трогать код маппинга в трёх клиентах | Сети, где UDP режут или DPI |
| C5 | Групповой TURN: передать TURN-серверы из join response в `GroupNetworkManager::SetConfiguration` | Групповой звонок там, где UDP до SFU не проходит |
| C6 | tdesktop: передавать `phoneCall.custom_parameters` в `Descriptor.config.customParameters` (одна строка в `calls_call.cpp`) | Иначе C1 не дойдёт до десктопа |
| C7 | (позже) AV1 или VP9 SVC для экрана | В групповом фильтре кодеков AV1 нет; нужен libaom/dav1d в трёх сборках WebRTC — дорого, отложить |

Веб-клиенты используют браузерный WebRTC: патч на них не действует, но с SFU они совместимы.

---

## 5. Управляемые relay-сети (цены 2025–2026)

| Сервис | Цена | Бесплатно | Подходит для tgcalls |
|---|---|---|---|
| **Cloudflare Realtime TURN** | **$0.05/ГБ** egress к TURN-клиенту | **1000 ГБ/мес** (общие с SFU) | Да, стандартный TURN. Anycast, 330+ городов; relay-адрес тоже anycast, «заблудившиеся» пакеты идут по магистрали Cloudflare. UDP 3478/443, TCP 3478/80, TLS 5349/443. Лимит на одну аллокацию — около 50–100 Мбит/с и 5–10 kpps. Китай исключён. Креды: `POST rtc.live.cloudflare.com/v1/turn/keys/{id}/credentials/generate-ice-servers {"ttl":…}`, есть отзыв. Для стоковых клиентов — только UDP и IP-литералы |
| Cloudflare Realtime SFU | $0.05/ГБ (общий 1 ТБ) | — | **Нет.** Сессия = PeerConnection с SDP offer/answer и API треков; у tgcalls свой join-JSON, Colibri и переписывание SSRC [вывод] |
| Twilio NTS | $0.40/ГБ (США, ЕС), $0.60–0.80 (Азия, ЮА) | только STUN | Да, но в 8–16 раз дороже |
| Metered.ca | $99 за 150 ГБ (+$0.40/ГБ), $199 за 500 ГБ, $499 за 2 ТБ; **считаются вход + выход** | 500 МБ | Да, есть 80/443 TCP/TLS |
| Xirsys | $39 за 50 ГБ, $99 за 150 ГБ, $199 за 500 ГБ, $499 за 2 ТБ | 500 МБ (жёсткий лимит) | Да, 12 регионов |

Вывод: Cloudflare TURN — практически бесплатная «периферийная точка входа» для 1:1 и для резервного пути групп.

---

## 6. Рекомендуемая архитектура

```
                    ┌──────────── Cloudflare anycast TURN (330+ PoP) ─────────┐
 клиент 1:1 ──P2P (ICE, STUN)──────────────────────────────── клиент 1:1     │
      └── при неудаче P2P: TURN UDP 3478/443 (стоково) │ TCP/TLS 443 (патч C4)│
                                                                              │
 клиенты группы ──UDP (UDPMux) / ICE-TCP 443──► SFU (go_sfu+) ──┐            │
      └── (патч C5) резерв через Cloudflare TURN ───────────────┘            │
                                                                              │
 Продакшен-VPS (один ДЦ с хорошим пирингом, напр. FRA/AMS/HEL):
   teamgram (phone.*, выдача TURN-кредов, join response, custom_parameters)
   go_sfu+ (NACK/RTX, TWCC-BWE, simulcast, ICE-TCP, метрики)
   coturn (запасной TURN UDP/TCP; TLS на отдельном IP/порту, если 443 занят)
 Опционально (по замерам): 1 L4-форвардер + coturn рядом с удалённым кластером
   пользователей; сервер добавляет его кандидат только этим пользователям (GeoIP)
```

---

## 7. Трафик и стоимость (≤100 пользователей, пик 20 участников)

Допущения на одного получателя в группе после патча:
- голос: несколько говорящих, ~0.1–0.2 Мбит/с;
- одно большое видео 720p: 1.5–2 Мбит/с, плюс 3 миниатюры по 0.15–0.4 Мбит/с;
- экран 1080p: 2–4 Мбит/с.

**Пик — 3–6 Мбит/с на получателя.** 16 участников в группах дают около 80–100 Мбит/с исходящего трафика SFU и около 40 Мбит/с входящего; гигабитного порта достаточно. Звонки 1:1 идут в основном P2P, и серверного трафика у них почти нет.

| Сценарий | Нагрузка | Исходящий трафик SFU в месяц |
|---|---|---|
| Лёгкий | 30 активных × 10 ч × 1 Мбит/с | ~0.14 ТБ |
| Средний | 100 × 20 ч × 1.5 Мбит/с | ~1.35 ТБ |
| Тяжёлый | 100 × 40 ч × 3 Мбит/с | ~5.4 ТБ |

| Статья | Стоимость в месяц |
|---|---|
| Трафик SFU на Hetzner EU (20 ТБ включено, сверх — около €1/ТБ) [вероятно] | **€0** во всех сценариях |
| Cloudflare TURN как резерв для 1:1 (обычно 10–25% звонков [оценка]; даже если все 1:1 пускать через relay — около 1.8 ГБ на час видеозвонка) | **$0**, в пределах 1 ТБ |
| Группы целиком через Cloudflare TURN (не рекомендуется; только резерв) | ~$17 (средний) / ~$220 (тяжёлый) |
| То же через Twilio | ~$540 (средний) |
| Отдельный медиа-VPS (по желанию) | Hetzner от €6.49 (shared) или от €43.49 (dedicated vCPU) |
| L4-форвардер в регионе (по требованию) | ~$3–7 [оценка] |
| **Итого обычно** | **≈ $0–15 сверх основного VPS** |

---

## 8. Порядок работ

1. **1:1 без патча:** реализовать `phone.*`, `p2p_allowed=true`, Cloudflare TURN (IP-литералы, UDP) + coturn; выдача кредов с TTL.
2. **SFU:** превратить go_sfu в сервис (UDPMux, ICE-TCP 443, публичные IP, аутентификация join через teamgram, лимиты); затем S5 (NACK/RTX) → S6 (TWCC-BWE) → S7–S8; метрики (S12).
3. **Патч tgcalls (C1, C6)** с текущими значениями по умолчанию: проверить, что сервер управляет параметрами. Затем поднять C2 и C3 с сервера поэтапно, на каждом шаге сверяясь с замерами.
4. **C4 и C5** (TCP/TLS-резерв), когда появятся пользователи в сетях с фильтрацией.
5. **Замеры по регионам** (RTT, потери, задержка рта-до-уха, разрешение и fps экрана). Если есть кластер с RTT больше 120–150 мс или с DPI — добавить один L4-форвардер (S11).
6. Поправить ADR 0004: убрать «coturn в регионах» и «TLS 443 обязательно без патча», зафиксировать связку «серверные параметры + один SFU + anycast TURN».

---

## 9. Риски

- **Битрейты нельзя поднимать раньше, чем SFU научится NACK и BWE**, иначе на плохих сетях станет хуже.
- **Сопровождение патча tgcalls:** upstream активно меняется (13.0.0, mtproto-in-DTLS, ReferenceImpl). У трёх клиентов закреплены разные коммиты, патч надо перебазировать при каждом обновлении.
- **go_sfu** — тестовый SFU; доводка около 4–8 недель [оценка].
- **ICE-TCP-кандидат SFU и Cloudflare TURN по IP-литералам** работают по коду, но звонками не проверены.
- **РФ:** с 2025 года на мобильных сетях соединения к зарубежным ДЦ (Hetzner, DigitalOcean) замораживаются после ~16 КБ, и для TCP, и для UDP. Cloudflare тоже деградирует ([zona.media](https://en.zona.media/article/2025/06/27/cloud_confirmed), [Cloudflare](https://blog.cloudflare.com/russian-internet-users-are-unable-to-access-the-open-internet), [net4people](https://github.com/net4people/bbs/issues/490)) [вероятно]. Если пользователи там, понадобится ретранслирующий узел в российском ДЦ — и для MTProto тоже.
- **Anycast-IP Cloudflare** у старых клиентов передаются литералами — резолвить при каждом звонке.
- **P2P раскрывает IP** участникам. Режим только через relay безопаснее, но даёт больше трафика (вероятно, всё равно в пределах бесплатного 1 ТБ).
- **Цены Hetzner** в 2026 году менялись; объём трафика — по сторонним источникам.

---

## Источники

- Discord: [How we moved Discord voice to the edge (2026-06-09)](https://discord.com/blog/how-we-moved-discord-voice-to-the-edge); [How Discord handles 2.5M concurrent voice users (2018)](https://discord.com/blog/how-discord-handles-two-and-half-million-concurrent-voice-users-using-webrtc); [How it all Goes Live (2024)](https://discord.com/blog/how-it-all-goes-live-an-overview-of-discords-streaming-technology); [Audio Bitrate FAQ](https://support.discord.com/hc/en-us/articles/11635925354775-Audio-Bitrate-FAQ); Go Live, сторонние: [eklipse](https://blog.eklipse.gg/streaming-tips/how-to-stream-on-discord.html), [onestream](https://onestream.live/blog/how-to-stream-on-discord-most-easy-steps/).
- tgcalls ([TelegramMessenger/tgcalls](https://github.com/TelegramMessenger/tgcalls), коммит `2faee3b5` и `development`): `tgcalls/v2/InstanceV2Impl.cpp`, `v2/InstanceV2ReferenceImpl.cpp`, `v2/NativeNetworkingImpl.cpp`, `v2/ReflectorPort.cpp`, `v2/ReflectorRelayPortFactory.cpp`, `v2/CustomParameters.h`, `group/GroupInstanceCustomImpl.cpp`, `group/GroupInstanceImpl.h`, `group/GroupNetworkManager.cpp`, `group/GroupJoinPayloadInternal.cpp`, `tools/go_sfu/{CLAUDE.md,sfu.go,participant.go,go.mod}`.
- Клиенты: [tdesktop v7.0.9](https://github.com/telegramdesktop/tdesktop) `calls/calls_call.cpp`, `calls/group/calls_group_call.cpp`; [DrKLO/Telegram @aac1efcb83](https://github.com/DrKLO/Telegram) `VoIPService.java`.
- Cloudflare: [Realtime TURN](https://developers.cloudflare.com/realtime/turn/), [Generate credentials](https://developers.cloudflare.com/realtime/turn/generate-credentials/), [Pricing](https://developers.cloudflare.com/realtime/sfu/pricing/), [TURN and anycast (2024-09-25)](https://blog.cloudflare.com/webrtc-turn-using-anycast), [Realtime SFU](https://developers.cloudflare.com/realtime/sfu/), [Sessions and tracks](https://developers.cloudflare.com/realtime/sfu/concepts/sessions-tracks/).
- TURN-провайдеры: [Twilio NTS pricing](https://www.twilio.com/en-us/stun-turn/pricing), [Metered](https://www.metered.ca/stun-turn), [Xirsys pricing](https://xirsys.com/pricing/).
- Pion: [pion/interceptor](https://github.com/pion/interceptor) (пакеты `gcc`, `nack`, `twcc`, `flexfec`, `pacing`; v0.1.49 от 2026-09-15), [pion/webrtc ICE mux](https://pkg.go.dev/github.com/pion/webrtc/v3), [pion/ice multiplexing](https://www.mintlify.com/pion/ice/guides/multiplexing).
- RED: [webrtcHacks: RED](https://webrtchacks.com/red-improving-audio-quality-with-redundancy/), [RED на SFU](https://webrtchacks.com/implementing-redundant-audio-on-an-sfu/), [audio_encoder_copy_red.cc](https://webrtc.googlesource.com/src/+/main/modules/audio_coding/codecs/red/audio_encoder_copy_red.cc).
- Хостинг: [Hetzner Cloud](https://www.hetzner.com/cloud/), [egresscost.com/hetzner](https://egresscost.com/hetzner), [findstack: Hetzner 2026](https://findstack.com/resources/hetzner-price-increase-2026).
- РФ: [Moscow Times 2026-08-06](https://www.themoscowtimes.com/2026/08/06/roskomnadzor-says-its-restricting-whatsapp-telegram-calls), [Meduza 2026-02-10](https://meduza.io/en/news/2026/02/10/russia-s-federal-censorship-agency-reportedly-begins-slowing-telegram-nationwide), [zona.media 16 KB](https://en.zona.media/article/2025/06/27/cloud_confirmed), [Cloudflare blog](https://blog.cloudflare.com/russian-internet-users-are-unable-to-access-the-open-internet), [net4people #490](https://github.com/net4people/bbs/issues/490), [paper 2025](https://corpus.lantern.io/papers/2025-hyperion-cs-censor-has-new/).

---

## Проверка фактов (независимый проверяющий)

| Утверждение | Статус | Поправка | Источник |
|---|---|---|---|
| 1. tgcalls 1:1 (InstanceV2Impl) caps outgoing audio at 32 kbps and video at 1000 kbps; Opus uses 60 ms frames with in-band FEC; ICE TCP is hard-disabled (enableTCP=false) | confirmed | Confirmed at 2faee3b5, which is exactly the tgcalls commit tdesktop v7.0.9 pins: kCodecParamUseInbandFec=1 and PTime=60 at lines 178-179, .enableTCP=false at line 1060, setMaxBitrate(32*1024) and (1000*1024) at lines 2163-2166. The development branch (last push 2026-10-13) has the same values at lines 162-163, 1044 and 2147-2150. One nuance: enableTCP=false sets PORTALLOCATOR_DISABLE_TCP, which in libwebrtc (tg_owt basic_port_allocator.cc CreateTCPPorts) only turns off host TCP candidates. It does not block TURN or reflector relays over TCP. | github.com/TelegramMessenger/tgcalls tgcalls/v2/InstanceV2Impl.cpp @2faee3b5 and @development; tdesktop v7.0.9 Telegram/ThirdParty/tgcalls submodule sha; desktop-app/tg_owt src/p2p/client/basic_port_allocator.cc |
| 2. InstanceV2ReferenceImpl (pinned) skips isTcp servers, drops non-IP-literal ICE hosts, audio 32 kbps, video 1200 kbps; hostname pass-through for Cloudflare TURN only added on development in Sept 2026 | confirmed | Pinned code: `if (server.isTcp) continue;` at line 648, `!address.IsComplete()` drops hostnames at line 653, 32*1024 at line 709, 1200*1024 at line 1454 (adjustBitratePreferences also caps at 1000*1024). The development branch adds network_reference_allow_hostname_ice_servers, with a comment naming Cloudflare TURN. Reflectors are still exempt. Commit 5e166010 on 2026-09-01 is consistent with the claimed timing. ReferenceImpl serves library versions 10.0.0 and 11.0.0. | tgcalls/v2/InstanceV2ReferenceImpl.cpp @2faee3b5 lines 647-675 and @development lines 746-790; GitHub commits API for this path |
| 3. TURN from phoneConnectionWebrtc is always UDP in stock clients (tdesktop maps without isTcp; NativeNetworkingImpl only PROTO_UDP/PROTO_TCP, no TLS) | confirmed | tdesktop v7.0.9 calls_call.cpp lines 134-166 builds the TURN RtcServer without isTcp, so it defaults to false. NativeNetworkingImpl.cpp line 626 has `server.isTcp ? PROTO_TCP : PROTO_UDP`, with no TLS branch. One caveat: the reflector-type phoneConnection does carry `.isTcp = data.is_tcp()` (line 129), so TCP exists in the protocol, but only for reflectors. | telegramdesktop/tdesktop v7.0.9 Telegram/SourceFiles/calls/calls_call.cpp; tgcalls/v2/NativeNetworkingImpl.cpp @2faee3b5 |
| 4. Group engine: Opus fixed 32 kbps (min=max=start), 120 ms ptime; total send cap (1020+32) kbps; simulcast 60/110/900 kbps camera; codecs VP8/VP9/H.264 only | confirmed | Lines 219-225 are the codec filter (VP8/VP9/H264). Lines 2733-2744 set min/max/start=_outgoingAudioBitrateKbit and ptime 120. Line 2195 has the field trial WebRTC-Audio-Allocation/min:32kbps,max:32kbps. Lines 3205-3207 set (1020+32)*1000. Camera layer max_bitrate is 60k/110k/900k, screencast is 100k/200k/900k. GroupInstanceImpl.h has outgoingAudioBitrateKbit{32}, a descriptor parameter that is uint8_t inside the engine, so values are ≤255. | tgcalls/group/GroupInstanceCustomImpl.cpp and GroupInstanceImpl.h @2faee3b5 |
| 5. Group engine configures no STUN/TURN; candidate protocol field parsed; TCP not disabled in port allocator | confirmed | GroupNetworkManager.cpp: the flags are only IPV6/IPV6_ON_WIFI, with no DISABLE_TCP, and SetConfiguration({}, {}, ...) at lines 387-398. GroupInstanceCustomImpl lines 3874-3896 pass candidate.protocol to cricket::Candidate. Nuance: GroupJoinPayloadInternal parses `tcptype`, but the field is NOT applied to cricket::Candidate, so it arrives empty. | tgcalls/group/GroupNetworkManager.cpp, GroupInstanceCustomImpl.cpp, GroupJoinPayloadInternal.cpp @2faee3b5 |
| 6. ICE-TCP passive candidate from SFU on port 443 will actually connect with stock tgcalls group clients | unverifiable | There is no test, but the code supports it. In tg_owt, TCPPort::CreateConnection rejects only tcptype=active or (empty tcptype AND port 0). An empty tcptype with port 443 therefore makes the client open an active TCP connection, and the group allocator does not disable TCP. Still unproven: compatibility with pion ICE-lite plus TCPMux (RFC 4571 framing), and the mobile builds of webrtc. This needs an end-to-end test. | tg_owt src/p2p/base/tcp_port.cc lines 131-140; tgcalls group code above (inference) |
| 7. go_sfu ignores NACKs, uses UDP host candidates only, forwards all audio unconditionally, layer selection REMB-based while real clients send no REMB | confirmed | readRTCPLoop (participant.go ~419-464) handles only PLI, FIR and REMB. The ICE agent is NetworkTypeUDP4 plus CandidateTypeHost with Lite=true, so there is no IPv6 and no TCP. The sfu.go comment says audio is forwarded unconditionally. CLAUDE.md says real clients send no REMB, and the fallback is OnMaxActiveLayerIncreased, which jumps straight to the highest layer. The SFU does send transport-cc feedback to senders (twcc.go), so sender-side BWE works. There is an RtxRingBuffer, but it is used for padding and probing, not for NACK. | github.com/TelegramMessenger/tgcalls tools/go_sfu/{participant.go,sfu.go,bandwidth.go,twcc.go,CLAUDE.md} @development |
| 8. Servers can push customParameters via phoneCall.custom_parameters; iOS and Android pass them, tdesktop v7.0.9 does not; current keys only network_* | confirmed | Android VoIPService.java @aac1efcb83 line 3420 passes privateCall.custom_parameters.data. iOS OngoingCallContext.swift accepts customParameters. tdesktop calls_call.cpp v7.0.9 has 0 occurrences of 'custom'. Every getCustomParameter* key in the V2, Reference, NativeNetworking and Group engines (pinned and dev) starts with network_*. None are bitrate knobs. | DrKLO/Telegram VoIPService.java @aac1efcb83; local Telegram-iOS TelegramVoip/OngoingCallContext.swift; tdesktop v7.0.9; tgcalls sources |
| 9. Reflector protocol (phoneConnection tcp flag) supports raw TCP client-side, so a self-written reflector would give 1:1 TCP fallback without client patches | confirmed | On the client side this is confirmed. ReflectorPort.cpp has a PROTO_TCP path through RawTcpSocket with a 16-byte peer tag, tdesktop passes is_tcp for phoneConnection, and PORTALLOCATOR_DISABLE_TCP does not block TCP relays. Limitation: InstanceV2ReferenceImpl (versions 10.0.0/11.0.0) skips every isTcp server, so this works only for NativeNetworkingImpl engines (InstanceV2Impl 7/8/9/12/13). The reflector server has to be written from scratch: tgcalls/tools contains only cli, go_sfu and parity. End-to-end it is untested. | tgcalls/v2/ReflectorPort.cpp @2faee3b5; tdesktop calls_call.cpp v7.0.9 lines 100-133; tg_owt basic_port_allocator.cc; tgcalls tools/ listing @development |
| 10. Cloudflare Realtime TURN: anycast turn.cloudflare.com, UDP 3478/443, TCP 3478/80, TLS 5349/443; $0.05/GB egress to client; 1,000 GB free shared with SFU; per-allocation ~50-100 Mbps, 5-10 kpps; China excluded | confirmed | Every figure matches. The docs say it is free when used together with Realtime SFU, and the first 1,000 GB per month are shared between SFU and TURN. | developers.cloudflare.com/realtime/turn/ ; developers.cloudflare.com/realtime/sfu/pricing/ |
| 11. Cloudflare TURN credentials minted via POST .../generate-ice-servers with {ttl}; revocation endpoint exists | confirmed | The example uses ttl=86400, and no maximum TTL is documented. The response lists URLs with the hostname turn.cloudflare.com, not IPs, which matters for clients that drop hostnames. Revocation is POST .../credentials/$USERNAME/revoke and returns 204. | developers.cloudflare.com/realtime/turn/generate-credentials/ |
| 12. Cloudflare TURN relay addresses are anycast; misrouted packets carried over Cloudflare backbone | confirmed | Blog post dated 2024-09-25, modified 2026-07-15. | blog.cloudflare.com/webrtc-turn-using-anycast/ |
| 13. Cloudflare Realtime SFU not usable by stock tgcalls group clients | unverifiable | The docs confirm the model: sessions/new and tracks/new over an HTTP API with an offer/answer recipe. Colibri and custom signalling are not mentioned. Incompatibility with tgcalls (join JSON, Colibri data channel, SSRC/simulcast semantics) is a plausible inference, but no test was done. | developers.cloudflare.com/realtime/sfu/sessions-tracks/ |
| 14. Twilio TURN $0.40-0.80/GB; Metered Growth $99/150 GB (+$0.40/GB, ingress+egress); Xirsys Basic $39/50 GB | confirmed | Twilio charges $0.40 for US and EU, $0.60 for Singapore/Mumbai/Tokyo, and $0.80 for Sydney/São Paulo. Xirsys Basic overage is $0.50/GB. The comparison of 1.35 TB × $0.40 = $540 holds. | twilio.com/en-us/stun-turn/pricing; metered.ca/stun-turn; xirsys.com/pricing |
| 15. Discord hosts each call on one SFU; moved voice to Cloudflare edge (Rust/Tokio); Frankfurt ping -34%, loss -42%; Iceland edge made mixed-region calls worse (ping x2.7) | confirmed | The figures are confirmed, with dates: the post is from 2026-06-09 and the Iceland test from February 2025. In mixed calls, non-Iceland users saw ping ×2.7 and losses +9%. However, the blog does NOT credit the Frankfurt gains to 'good peering'. They are stated relative to the previous vendor after moving onto Cloudflare's network, with no cause given. The blog treats peering as a risk and a launch gate (the Orange→Telia case in Rotterdam). Its main lesson is that call placement matters more than having many PoPs. | discord.com/blog/how-we-moved-discord-voice-to-the-edge |
| 16. Discord voice bitrate 64 kbps default; boost 128/256/384 kbps | unverifiable | support.discord.com returns 403. Only third-party sources agree on these numbers. | web search (online-tech-tips, voxbooster, syrinscape forum); support.discord.com Audio Bitrate FAQ inaccessible |
| 17. Russian mobile operators freeze TCP and UDP flows to foreign DC ASNs (Hetzner, DO) after ~16 KB / ~25 packets; Cloudflare also affected | confirmed | The core is confirmed, but the details need correcting. Cloudflare (blog of 2025-06-26) reports a 16 KB cutoff since 2025-06-09 across all protocols, including HTTP/3/QUIC. That is about 10-14 packets, not 25. The ISPs named are Rostelecom, MegaFon, VimpelCom, MTS and MGTS, so not only mobile. Hetzner, DigitalOcean and OVH are reported as affected too. For UDP, net4people#490 describes not a freeze after 16 KB but a full block of the server for about 10 minutes. Comments from Nov 2025 to Jan 2026 (forum-level) report a shift to SNI+CIDR whitelists on mobile networks. Under whitelists, any server outside them is blocked completely, possibly even a Russian VPS. | blog.cloudflare.com/russian-internet-users-are-unable-to-access-the-open-internet/ ; github.com/net4people/bbs/issues/490 and comments |
| 18. Hetzner Cloud EU includes 20 TB, ~EUR 1/TB overage; prices from EUR 5.99/6.49 shared, EUR 43.49 dedicated | confirmed | The traffic figures are now confirmed by a primary source: CX, CAX and CPX include 20 TB in the EU, CCX includes 20-60 TB, overage is €1 ($1.20) per TB, and only outgoing traffic is billed. US and SIN allowances are much smaller (0.5-8 TB). The €5.99 Cost-Optimized plan is marked 'Currently not available'. €6.49 (Regular Performance, shared) and €43.49 (dedicated) are listed. Prices exclude VAT. | hetzner.com/cloud; docs.hetzner.com/robot/general/traffic/; docs.hetzner.com/cloud/billing/faq/ |

**Итоговая рекомендация после проверки:**

Рекомендация в целом остаётся в силе: проверены все утверждения о коде tgcalls, tdesktop и go_sfu, а также цены. Нужны уточнения:

1. Один медиа-узел без «флота». go_sfu (доведённый до продакшна) и coturn ставим на продакшен-VPS или на соседний VPS в одном хорошо связанном дата-центре ЕС (Франкфурт, Амстердам или Хельсинки). Опыт Discord это подтверждает: один SFU на звонок, а для смешанной географии место хостинга звонка важнее числа точек присутствия (тест в Исландии: пинг у остальных вырос в 2.7 раза). Убираем формулировку, будто «главный выигрыш даёт пиринг». Цифры по Франкфурту (−34% пинг, −42% потери) Discord приводит в сравнении с прежним вендором после переезда на сеть Cloudflare, причину он не называет. Пиринг для Discord — условие запуска региона (случай Orange→Telia в Роттердаме), поэтому при выборе дата-центра проверяем связность с основными операторами пользователей.

2. Звонки 1:1. p2p_allowed=true. В phoneConnectionWebrtc отдаём STUN и TURN. Основной TURN — свой coturn, а Cloudflare TURN нужен для пользователей вне РФ: в РФ провайдеры с июня 2025 режут соединения и к Cloudflare (подтверждено Cloudflare). Креды Cloudflare получаем через generate-ice-servers. Cloudflare отдаёт имя хоста turn.cloudflare.com, а движок ReferenceImpl (версии 10/11) отбрасывает имена хостов. Поэтому сервер при каждом звонке сам резолвит имя в IP (адреса anycast) и передаёт IP-литералы. Только UDP: в phoneConnectionWebrtc нет флага TCP. Опционально, без патча клиента: запасной TCP-путь для 1:1 через свой рефлектор (phoneConnection с tcp=true). Ограничения: он работает только для движков InstanceV2Impl (7/8/9/12/13), ReferenceImpl 10/11 его пропускает, а серверную часть рефлектора придётся писать самим.

3. Групповые звонки. В go_sfu сейчас только UDP4-host. Добавляем IPv6, UDPMux на один порт и passive ICE-TCP на 443. По коду клиент такой кандидат примет: tcptype теряется, но для пустого tcptype с ненулевым портом libwebrtc устанавливает исходящее TCP-соединение. Это обязательно проверить тестом на iOS, Android и tdesktop до того, как на это рассчитывать.

4. Без изменений. Нужны: ответ на NACK из кэша RTX (сейчас NACK игнорируется), оценка канала до каждого получателя по transport-cc с выбором слоя (сейчас опора на REMB, которого клиенты не шлют, а запасной вариант сразу прыгает на верхний слой), ограничение частоты PLI, пересылка только активных говорящих (сейчас пересылается всё аудио), метрики в Prometheus.

5. Без изменений. Один патч tgcalls превращает лимиты в параметры от сервера. Сейчас это 32 кбит/с и 60/120 мс у Opus, 1000–1200 кбит/с видео в 1:1, общий потолок 1052 кбит/с и слои 60/110/900 у групп, а все существующие ключи — только network_*. Плюс в tdesktop добавляется передача custom_parameters. В патч входит TURN через TCP/TLS, сейчас поддерживается только PROTO_UDP или PROTO_TCP.

6. Пункт про РФ усиливаем. Если доля пользователей в РФ заметная, закладываем российский узел сразу, а не «по замерам». Это L4-форвардер плюс coturn у крупного российского провайдера. Проверять на МТС, МегаФоне, Билайне и Т2. Причины: TCP к зарубежным дата-центрам (Hetzner, DigitalOcean, OVH, Cloudflare) обрывается после ~16 КБ, UDP по сообщениям блокируется на ~10 минут, а с конца 2025 года (данные форумов) на мобильных сетях появляются белые списки SNI/CIDR, под которые может не попасть даже обычный российский VPS. Для остальных регионов форвардер ставим только по замерам (RTT больше 120–150 мс).

7. Порядок прежний: coturn/Cloudflare + P2P → SFU с NACK, transport-cc и ICE-TCP → патч tgcalls → замеры → форвардеры. AV1/SVC откладываем, групповой движок умеет только VP8, VP9 и H.264.

Стоимость: около $0–15 в месяц сверх основного VPS. В ЕС включено 20 ТБ исходящего трафика (входящий не тарифицируется), сверх этого €1 за ТБ. Медиа-VPS: от €6.49 (shared; тариф за €5.99 сейчас недоступен) или €43.49 (dedicated). Cloudflare TURN: 1 ТБ в месяц бесплатно, дальше $0.05/ГБ, то есть ~$17 при 1.35 ТБ и ~$220 при 5.4 ТБ. У Twilio те же 1.35 ТБ стоят ~$540. Российский форвардер — дополнительно несколько долларов в месяц (оценка, не проверено).
