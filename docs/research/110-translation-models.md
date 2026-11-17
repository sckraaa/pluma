# Модели перевода для Pluma: обзор кандидатов (2026-11-17)

Цель: сообщение ~320 символов p95 ≤ 6 с (в идеале ~2 с), пачка из 20 ≤ 25 с, на i5-13500T без GPU. Сейчас: HY-MT1.5-1.8B Q8_0, tg ≈ 17 ток/с (упор в память), 320 символов — 6,4–12,2 с.
Все источники прочитаны 2026-11-17. Всё, что не подтверждено прочитанным источником, помечено **UNVERIFIED**.

## Главный вывод

1. **Hy-MT2-1.8B (Tencent, май 2026) — прямая замена HY-MT1.5-1.8B.** Та же архитектура (`hunyuan_v1_dense`, hidden 2048, 32 слоя, 16 голов / 4 KV), тот же llama.cpp, те же языки (включая kk, uk, ru), качество выше, лицензия **Apache-2.0 без территориальных исключений** (у HY-MT1.5 — Tencent HY Community License, не действует в ЕС/UK/Корее). Скорость на токен при той же квантизации та же; ускорение даёт только меньшая квантизация (Q4_K_M 1,13 ГБ против Q8_0 1,91 ГБ) ценой качества, в первую очередь следования инструкциям (важно для тегов).
2. **Одна модель-LLM на этом CPU цель «~2 с» не даст.** При упоре в память скорость ≈ пропускная способность / размер весов; ни один кандидат с качеством ≥ HY-MT1.5 не меньше 1 ГБ. Q4_K_M даёт оценочно ×1,5–1,7 к tg (**UNVERIFIED**, измерить), то есть 320 символов ≈ 4–7 с — на границе цели.
3. **Быстрый путь — модели Firefox Translations (Bergamot/Marian, int8, ~17–43 МБ на направление).** Есть en↔ru, en↔uk и ещё ~55 языков в паре с en (ru↔uk — через en), **казахского нет**. Скорость — сотни–тысячи слов/с на CPU (то есть десятки мс на сообщение), есть встроенный перенос HTML-разметки. Качество ниже LLM (критерий выпуска Mozilla — «в пределах ~5% COMET от Google Translate»). Это вариант гибридной схемы: Bergamot для покрытых пар, Hy-MT2 для kk и остального. Цена — новый C++-движок (cgo или отдельный сервис).
4. Gemma 4 E2B/E4B, TranslateGemma, Qwen3, EuroLLM, MADLAD, M2M-100 и OPUS-MT лучшего баланса не дают (ниже). NLLB, Tilmash, Aya Expanse, Tower+ исключены по лицензии.

## Ранжированный список для нашей цели

| # | Кандидат | Параметры | kk / uk / ru | Лицензия | Качество (по источникам) | Рантайм на CPU | Скорость (оценка) | Теги |
|---|---|---|---|---|---|---|---|---|
| 1 | **Hy-MT2-1.8B** (Q6_K / Q4_K_M) | ~1,8B (HF: «2B params») | да / да / да; 33 языка во введении, в таблице 38 кодов | Apache-2.0 | лучше HY-MT1.5-1.8B почти везде (таблица ниже) | llama.cpp GGUF (официальные Q4_K_M/Q6_K/Q8_0) | как сейчас на Q8; Q4_K_M ≈ ×1,5–1,7 tg (**UNVERIFIED**) | официальные шаблоны «Delimiters» и «Structured Data» |
| 2 | **Firefox Translations (Bergamot)** как быстрый путь | tiny/base Marian, файл модели 17–43 МБ | нет / да / да (только в паре с en) | MPL-2.0 (репозиторий; см. оговорку) | порог выпуска ≈ 95% COMET Google | bergamot-translator (C++, intgemm), WASM | ~1000–6500 слов/с на 4 ядрах старых CPU (arXiv 2109.10194) | HTML-режим с переносом тегов по выравниванию |
| 3 | OPUS-MT tc-big en-zle / zle-en | 0,2B | (kk — отдельная модель en-trk, слабая) / да / да | CC-BY-4.0 (en-zle); Apache-2.0 (en-trk) | FLORES-101 en→ru BLEU 32,7, chrF 0,597; en→uk 32,1 / 0,601 | Marian, CTranslate2 int8 | CTranslate2 int8: 696 ток/с на 4 потоках Xeon 8275CL (base-модель, пакетная пропускная) | нет |
| 4 | Gemma 4 E4B-it | 4,5B эффективных, 8,0B с эмбеддингами | **kk не подтверждён** (140+ языков в предобучении, «35+» из коробки) | Apache-2.0 | ≈ Hy-MT2-1.8B на FLORES, хуже на WMT25 (отчёт Hy-MT2) | llama.cpp (**UNVERIFIED** поддержка PLE на CPU) | заметно медленнее 1.8B | общий instruction-следователь, IFMTBench 71,7 |
| 5 | Gemma 4 E2B-it | 2,3B эфф., 5,1B с эмбеддингами | как у E4B | Apache-2.0 | хуже Hy-MT2-1.8B и HY-MT1.5-1.8B | llama.cpp (**UNVERIFIED**) | ≈ или медленнее 1.8B | IFMTBench 57,6 |
| 6 | TranslateGemma 4B | ~4,3B | **kk нет в 55 основных языках** (WMT24++), uk/ru есть | Gemma Terms of Use (коммерция и хостинг разрешены, передача ограничений дальше, право Google ограничить) | WMT24++ MetricX 5,32 / COMET 81,6 | GGUF есть у сообщества; вход ≤ 2K токенов | медленнее 1.8B (≈ ×2 весов) | не заявлено |
| 7 | Qwen3-1.7B / 4B | 2,0B / 4,0B | да / да / да (119 языков) | Apache-2.0 | качество перевода против Hy-MT не опубликовано (**UNVERIFIED**) | llama.cpp | 1.7B ≈ как сейчас; 4B ≈ вдвое медленнее | общий LLM |
| 8 | MADLAD-400 3B-MT | 3B (T5, 32 слоя) | да / да / да (419 языков) | Apache-2.0 | «конкурентна с моделями значительно больше», чисел в карточке нет; «не оценена для продакшена» | GGUF q4k (Candle), CTranslate2 (**UNVERIFIED**) | **UNVERIFIED** | нет |
| 9 | M2M-100 418M / 1.2B | 0,4 / 1,2B | да / да / да | MIT | модель 2020 г., по общему мнению заметно ниже современных (**UNVERIFIED**) | CTranslate2, ONNX | быстро (encoder-decoder) | нет |

### Hy-MT2-1.8B против HY-MT1.5-1.8B (arXiv 2605.22064, таблица 2; XCOMET-XXL / CometKiwi / GEMBA)

| Набор | Hy-MT2-1.8B | HY-MT1.5-1.8B | Gemma4-E4B | Gemma4-E2B |
|---|---|---|---|---|
| FLORES-200 EN⇔XX | 90,00 / 85,46 / 84,05 | 89,42 / 84,11 / 81,35 | 89,54 / 85,61 / 86,95 | 87,20 / 84,00 / 83,63 |
| FLORES-200 XX⇔XX | 79,77 / 73,41 / 78,64 | 78,40 / 71,82 / 75,12 | 79,96 / 72,91 / 80,97 | 75,97 / 71,00 / 76,64 |
| WMT25 | 50,30 / 64,59 / 70,36 | 53,08 / 61,95 / 63,58 | 38,36 / 55,45 / 59,95 | 35,60 / 51,83 / 54,35 |
| WildMTBench (XCOMET / GEMBA) | 87,43 / 86,04 | 87,41 / 80,84 | 83,45 / 84,18 | 82,01 / 81,58 |
| IFMTBench total | 69,36 | — | 71,70 | 57,59 |

Оговорка: разбор столбцов сделан по HTML-версии статьи, выравнивание таблицы неоднозначно; сравнивать числа внутри одной таблицы.

Квантизация Hy-MT2-1.8B (таблица 5 того же отчёта): BF16 → Q4_K_M → 2-bit: FLORES EN⇔XX 87,02 → 85,87 → 84,74; XX⇔XX 79,21 → 77,19 → 76,31; WildMTBench 86,74 → 86,09 → 83,33; **IFMTBench 69,36 → 63,47 → 58,99**. То есть Q4_K_M теряет ~1–2 пункта по переводу и ~6 по следованию инструкциям (риск для сохранения `<sN>`-тегов). Q6_K в таблице нет — вероятный компромисс (**UNVERIFIED**, проверить на нашем наборе тегов).

1.25-bit-вариант (440 МБ, «×1,5 к 4-bit HY-MT1.5 на Apple A15») требует ядра STQ1_0 из llama.cpp PR #22836: PR **открыт, не влит, ядро только ARM NEON** — на x86 сервере неприменимо. Формат 2-bit GGUF не проверен (**UNVERIFIED**).

Рекомендуемые параметры Hy-MT2 1.8B/7B: temperature 0.7, top_p 0.6, top_k 20, repetition_penalty 1.05; системного промпта нет (карточка). Для детерминизма в нашем сервисе — решать отдельно.

## Подробно по кандидатам

### Hy-MT2 (Tencent), 2026-05-21
- Размеры: 1.8B, 7B, 30B-A3B (MoE, 3B активных). Официальные GGUF: 1.8B Q4_K_M 1,13 ГБ, Q6_K 1,47 ГБ, Q8_0 1,91 ГБ; 7B GGUF; FP8.
- Языки (карточка 1.8B): zh, en, fr, pt, es, ja, tr, ru, ar, ko, th, it, de, vi, ms, id, tl, hi, zh-Hant, pl, cs, nl, km, my, fa, gu, ur, te, mr, he, bn, ta, **uk**, bo, **kk**, mn, ug, yue. Нет, например, el, sv, fi, da, no, hu, ro, bg, sr, hr, sk, sl, lt, lv, et, be, uz, az — для «всех языков Telegram» нужен второй движок или запасной путь.
- Лицензия: LICENSE.txt — «Hy-MT2-1.8B is licensed under the Apache License, Version 2.0». Для сравнения, License.txt HY-MT1.5-1.8B: «THIS LICENSE AGREEMENT DOES NOT APPLY IN THE EUROPEAN UNION, UNITED KINGDOM AND SOUTH KOREA».
- Разметка: шаблон «Delimiters» требует сохранить то же число разделителей; шаблон «Structured Data» — сохранить структуру и не переводить теги/ключи/плейсхолдеры. Количественных результатов по тегам нет; в отчёте — примеры с «##» и SRT.
- 7B: FLORES XX⇔XX XCOMET 86,89 (против 80,98 у HY-MT1.5-7B) — качественно намного лучше, но 7B на этом CPU ≈ в 4 раза медленнее 1.8B; не проходит по цели. 30B-A3B: 3B активных, но ~30B весов в ОЗУ (Q4 ≈ 17–18 ГБ, **UNVERIFIED**) — общий сервер, не рассматривать.
- Источники: https://huggingface.co/tencent/Hy-MT2-1.8B, https://huggingface.co/tencent/Hy-MT2-1.8B-GGUF, https://huggingface.co/tencent/Hy-MT2-1.8B/raw/main/LICENSE.txt, https://huggingface.co/tencent/Hy-MT2-1.8B/raw/main/config.json, https://arxiv.org/html/2605.22064, https://github.com/ggml-org/llama.cpp/pull/22836, https://huggingface.co/tencent/HY-MT1.5-1.8B/raw/main/License.txt.

### Firefox Translations / Bergamot (Mozilla)
- Список моделей из Remote Settings (https://firefox.settings.services.mozilla.com/v1/buckets/main/collections/translations-models/records): 116 направлений, все через en: af, ar, az, be, bg, bn, bs, ca, cs, da, de, el, es, et, eu, fa, fi, fr, gl, gu, he, hi, hr, hu, id, is, it, ja, kn, ko, lt, lv, ml, mr, ms, nb, nl, pl, pt, ro, **ru**, sk, sl, sq, sr, sv, ta, te, th, tl, tr, ug, **uk**, ur, vi, zh-Hans, zh-Hant (+ mt, nn только →en). **kk нет.** Firefox пивотирует не более одного раза и только через en (https://firefox-source-docs.mozilla.org/toolkit/components/translations/resources/01_overview.html — по выдаче поиска).
- Размеры: ru→en v2.1 модель 31,6 МБ + lex 4,4 МБ + vocab 0,95 МБ; en→ru v2.0 — 43 МБ; en→uk v2.2 — 31,6 МБ; старые tiny — 17 МБ.
- Качество: критерий выпуска — «в пределах ~5% COMET от Google Translate» (https://mozilla.github.io/translations/firefox-models/). Сравнения с HY-MT нет; по сути ниже LLM на разговорном тексте (**UNVERIFIED**).
- Скорость: arXiv 2109.10194 — en→de 8-bit на всех ядрах: ~1066 слов/с (i5 2012, 2 ядра), 3146 (i7 2012, 4 ядра), 6548 (i7 2016). Release notes bergamot-translator v0.3.1: ~500–600 слов/с. На одно сообщение — десятки мс (**UNVERIFIED** на нашем CPU; intgemm работает на AVX2, у i5-13500T нет AVX-512).
- Разметка: bergamot-translator имеет HTML-режим: из входа извлекается текст, теги запоминаются и вставляются в перевод по выравниванию (https://browser.mt/docs/main/api/classmarian_1_1bergamot_1_1HTML.html). Наши `<sN>` можно отобразить в `<b>`/`<a>`. Есть открытые баги сопоставления разметки (bugzilla 1917708, 1844096).
- Лицензия: репозитории mozilla/translations, firefox-translations-models, bergamot-translator — MPL-2.0 (GitHub API). Отдельного текста лицензии на веса я не нашёл — лицензия весов = MPL-2.0 репозитория **UNVERIFIED**; обучающие данные — OPUS с разными лицензиями.
- Поддержка: mozilla/bergamot-translator архивирован (2023), firefox-translations-models архивирован (2025-12), живой — mozilla/translations (push 2026-11-16) и browsermt/bergamot-translator (2024-05). Встраивание в Go — cgo или отдельный сервис; это отдельная инженерная задача.

### OPUS-MT / Tatoeba-Challenge (Helsinki-NLP)
- en→{bel, rus, ukr} `opus-mt-tc-big-en-zle`: 0,2B, CC-BY-4.0, FLORES-101 devtest en-rus BLEU 32,7 / chrF 0,59654, en-ukr 32,1 / 0,60131. Обратная `opus-mt-tc-big-zle-en` есть.
- Казахский: отдельных моделей en-kk / ru-kk / kk-ru на HF не найдено (поиск по API). Есть только мультиязычные тюркские: `opus-mt-en-trk` (Apache-2.0) — Tatoeba eng-kaz BLEU 11,1, chrF 0,359 — непригодно; `opus-mt-trk-en`; `opus-mt-tc-bible-big-*-trk` (обучены в т. ч. на Библии).
- Рантайм: Marian, CTranslate2 (готовые конверсии `gaudi/*-ctranslate2`), ONNX. Скорость CTranslate2 int8 на OPUS-MT base: 696 ток/с, 4 потока Xeon 8275CL (README CTranslate2).
- Теги: встроенной поддержки нет.
- Вывод: альтернатива Bergamot для ru/uk↔en, без kk; Bergamot предпочтительнее (меньше, быстрее, есть HTML-режим, выпускается Mozilla).

### TranslateGemma (Google), 2026-01
- 4B / 12B / 27B на базе Gemma 3. WMT24++ (55 языков): MetricX 5,32 / 3,60 / 3,09; COMET 81,6 / 83,5 / 84,4.
- Языки: 55 пар WMT24++ включают en-ru_RU и en-uk_UA, **kk нет** (https://huggingface.co/datasets/google/wmt24pp); «около 500 дополнительных пар» обучены без оценки.
- Вход ≤ 2K токенов; строгий чат-шаблон с кодами языков; про разметку не сказано.
- Лицензия: Gemma Terms of Use — хостинг как сервис явно считается «Distribution» и разрешён при условиях: передать ограничения (Prohibited Use Policy) пользователям, Google вправе «restrict (remotely or otherwise)» использование; территориальных ограничений нет (https://gemma.ai/resources/terms/).
- Вывод: 4B медленнее нашей 1.8B, без kk; не подходит.

### Gemma 4 (Google), 2026-04
- E2B (2,3B эфф. / 5,1B с эмбеддингами), E4B (4,5B / 8,0B), 12B, 26B-A4B, 31B; **Apache-2.0**; «140+ языков в предобучении, 35+ из коробки», kk/uk/ru в карточке явно не названы (https://gemma.ai/models/gemma-4/model-card/).
- По отчёту Hy-MT2: E4B ≈ Hy-MT2-1.8B на FLORES, заметно хуже на WMT25 и в домене; E2B хуже обеих Hunyuan 1.8B.
- Вывод: качество не выше, вычислений больше; хороший запасной вариант лицензии/языков, но не по скорости.

### Qwen3 small (Alibaba), 2025-04
- 0.6B / 1.7B / 4B — Apache-2.0, 119 языков, включая kk, uk, ru (https://qwenlm.github.io/blog/qwen3/).
- Опубликованного сравнения перевода с Hy-MT нет (**UNVERIFIED**). Специализированные Hy-MT обучались именно на переводе; ожидать от Qwen3-1.7B лучшего — нет оснований.
- Qwen-MT (2025) — только API, веса не выпущены. Qwen-MT-Lumen (HF `ATH-MaaS/Qwen-MT-Lumen`, Apache-2.0, 2026-08-07) — 14,8B по агрегатору, слишком велика для CPU.

### Seed-X (ByteDance), 2025-07
- 7B (HF: 7,5B), лицензия OpenMDW (разрешительная), 28 языков, ru и uk есть, **kk нет**. Официальные кванты GPTQ/AWQ, GGUF не рекомендуются авторами. Слишком велика и без kk.

### EuroLLM-1.7B-Instruct
- Apache-2.0, 35 языков, ru и uk есть, **kk нет**. Метрики только против Gemma-EuroBlocks в собственной шкале. Не лучше Hy-MT2 по языкам.

### MADLAD-400 3B-MT
- Apache-2.0, 419 языков (оценка на 204), T5 3B, 32 слоя; карточка: «не оценена для продакшена», возможен токсичный вывод из веб-данных. GGUF q4k 1,65 ГБ (Candle). Вероятно медленнее 1.8B на длинных сообщениях (**UNVERIFIED**). Как запасной путь для языков вне Hy-MT2 — возможен, но качество не измерено.

### M2M-100 418M / 1.2B
- MIT, 100 языков, kk/uk/ru есть. Модель 2020 года; публикаций, где она близка к современным LLM-переводчикам, не нашёл (**UNVERIFIED**). Возможна как дешёвый запасной путь для редких языков.

### Исключены по лицензии
- **NLLB-200 (вкл. distilled-600M/1.3B)** — CC-BY-NC-4.0, «research model and is not released for production deployment» (https://huggingface.co/facebook/nllb-200-distilled-600M). Подтверждено.
- **Tilmash (ISSAI, kk↔ru/en/tr)** — дообучена из NLLB; статья KazParC заявляет CC BY 4.0, но базовая модель NC — цепочка лицензий сомнительна; в карточке HF лицензия не указана.
- **Aya Expanse 8B / Aya 23** — CC-BY-NC-4.0 + AUP Cohere; kk нет.
- **Tower+ 2B** — заголовок HF cc-by-nc-sa-4.0, описание CC-BY-NC-4.0; kk нет.
- SeamlessM4T — NC (из условий задачи, не перепроверялось).

## Что из этого следует (для решения владельца)

1. Переход HY-MT1.5-1.8B → **Hy-MT2-1.8B**: снимает территориальную оговорку лицензии, поднимает качество, ничего не меняет в рантайме. Скорость решается квантизацией: проверить Q6_K и Q4_K_M на нашем наборе (ru↔en, en↔ru, ru↔kk, uk↔ru, теги `<sN>`), измерить tg/pp на домашнем сервере.
2. Если Q4_K_M не даёт p95 ≤ 6 с или ломает теги — гибрид: Bergamot для пар с en (и ru↔uk через en), Hy-MT2 для kk и прочих. Даёт ~2 с и лучше для основной массы ru/en/uk, но это новый движок и более низкое качество на этих парах.
3. Модели крупнее 2B (Hy-MT2-7B, TranslateGemma 4B+, Gemma 4 E4B, Seed-X) на этом CPU цель по скорости не проходят.

Открытые вопросы для локальной проверки: качество Hy-MT2 на kk (по-языковых чисел нет), реальная tg Q4_K_M/Q6_K на i5-13500T, сохранение `<sN>` на Q4_K_M, качество Bergamot ru/uk на разговорном тексте.
