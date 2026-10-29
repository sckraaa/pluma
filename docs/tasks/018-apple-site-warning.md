# 018. Снять пометку Apple с домена

- Статус: todo (после ребрендинга веб-клиента, задача 017)
- Зависит от: 017

## Проблема
Safari показывает «Предупреждение о мошенническом сайте» с пометкой «Apple сообщает, что этот сайт является мошенническим». Список ведёт сама Apple. Google Safe Browsing на 2026-10-29 угроз ни на `pluma.chat`, ни на `web.pluma.chat` не видит.

Вероятная причина: на новом домене страница называет себя Telegram, показывает его логотип и просит войти по номеру телефона. Так выглядят фишинговые копии Telegram.

## Шаги
1. Ребрендинг веб-клиента (задача 017): на `web.pluma.chat` не остаётся названия и логотипа Telegram.
2. Владелец открывает в Safari `https://pluma.chat` и `https://web.pluma.chat`. На каждом помеченном адресе — ссылка «сообщить об ошибке» в самом предупреждении. Она открывает форму Apple с уже подставленным адресом.
3. Текст обращения (по-английски):

   > pluma.chat and web.pluma.chat are the website and web client of Pluma, a private self-hosted messenger that I run for a closed group of up to 100 invited people. The web client is a build of the open-source Telegram Web K client (GPL-3.0). It connects only to our own server, never connects to Telegram, and never asks for Telegram credentials or payment data. The site serves only this client over HTTPS, with no downloads, ads or third-party scripts. Please review the site and remove the fraudulent website warning.
   >
   > Alexandr Skraglenko, <почта владельца>

   После ребрендинга фраза о брендинге исходного клиента не нужна.
4. Проверить через несколько дней: Safari на Mac и iPhone без предупреждения. Safari кэширует список, поэтому снятие пометки доходит не сразу.

## Результат
_—_
