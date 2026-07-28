# SEO: Stremio-страницы и разведение intent

Замер 2026-07-28 (DataForSEO + Google Search Console). Причина работ: подключение
Stremio — самая липкая механика продукта (×9 по ретеншену, см.
[onboarding_empty_states.md](./onboarding_empty_states.md)), но поисковый трафик
на неё не заводился.

## Данные, на которых основаны решения

Объёмы (en, worldwide):

| Запрос | Объём/мес |
|---|---|
| stremio addons | 201 000 |
| best stremio addons | 14 800 |
| stremio online | 12 100 |
| webtor | 12 100 |
| **stremio torrent addon** | **6 600** |
| stremio debrid | 2 900 |
| stremio web player | 2 400 |
| torrentio alternative | 2 400 |
| how to add addons to stremio | 1 300 |
| **stremio addons online** | **0** |

GSC за 90 дней: 476 stremio-запросов, 2016 показов, 77 кликов. Позиции 6–10 по
«how to watch torrents on stremio», «stremio torrent streaming», «stremio
addons» — при **нулевом CTR**.

## Что сделано

**1. `/stremio-addons-online` перетаргетирована.** Была оптимизирована под
«stremio addons online» — запрос с **нулевым** объёмом, и собрала 18 показов за
90 дней. Контент страницы на самом деле про «Stremio без установки приложения»,
поэтому title/benefit/description переписаны под `stremio online` (12 100) +
`stremio web player` (2 400). Тело страницы не трогали — оно уже про это.

URL намеренно **не менялся**: точное вхождение в URL — слабый фактор, а 404 на
проиндексированных адресах (включая `/fr/stremio-addons-online`) — реальный
минус.

**2. Новая страница `/webtor-stremio-addon`** под `stremio torrent addon`
(6 600). Транзакционный лендинг: как поставить личный аддон, что он делает, где
работает. Файлы:

- `handlers/common/tools.go` — запись в `Tools` (роут, sitemap и перелинковка
  подхватываются автоматически)
- `templates/partials/about/webtor_stremio_addon.html` — секции how-to,
  explained, benefits, devices, safety; CTA — `stremio_cta`
- ключи `tool.webtorStremioAddon.*` (28 штук) во всех 11 локалях

**Копирайт-ограничение:** тексты нигде не утверждают, что Webtor ищет или
индексирует торренты — аддон публикует **библиотеку самого пользователя**. Это
юридическое позиционирование платформы, не стилистика.

**3. Блог: разведение intent.** Три поста про Stremio конкурировали между собой:

| Пост | Показы/90д | Роль |
|---|---|---|
| `watch-your-torrents-on-tv-with-stremio` | 3141 (2467 — брендовый «webtor») | announcement, июнь 2025 |
| `webtor-torrentio-stremio` | 141 | announcement, сент. 2025 |
| `stream-torrents-smart-tv-stremio-webtor` | **30** | настоящий how-to, март 2026 |

То есть по how-to запросам ранжируется announcement, а написанный под это гайд
почти не виден. Сделано: во все шесть постов (EN+RU) добавлен `description` —
раньше его не было и Hugo лепил сниппет из первых строк; из announcement-постов
проставлены ссылки на how-to гайд и на новую tool-страницу.

## Чего сознательно НЕ делали

**Главную не трогали.** Первоначально планировалось переписать её
title/description, но GSC показал 957 764 показа и 165 986 кликов за 90 дней —
`torrent downloader online` (позиция 1.0, CTR 45.6%), `online torrent downloader`
(1.0, 49.9%). Stremio-запросы дают главной 482 показа — 0.05% трафика.
Оптимизировать её под них означало бы рисковать основным каналом ради
статистической погрешности.

**CTA на `/stremio-addons-online` откачен.** Была попытка поставить туда
Stremio-CTA: страница обещает «No Stremio installation — everything works
online», то есть предлагать там установку Stremio — противоречие обещанию
страницы и удар по её тематической цельности.

## Что мерить

- позиции и CTR `/webtor-stremio-addon` по `stremio torrent addon` (сейчас 0)
- показы `/stremio-addons-online` по `stremio online` (было 18 показов суммарно)
- перетекание how-to запросов с announcement-поста на гайд и на tool-страницу
- недельные подключения Stremio, база ~95–110/нед

Горизонт — 4–8 недель: новой странице нужно попасть в индекс и набрать историю.

## Инструменты

DataForSEO и GSC — обёртки на NAS, внутри контейнера claudeclaw (на хосте нет
bash):

```
ssh 192.168.0.57 'sudo docker exec claudeclaw bash /home/claude/workspace/bin/dfs-query.sh <endpoint> <payload>'
ssh 192.168.0.57 'sudo docker exec claudeclaw bash /home/claude/workspace/bin/gsc-query.sh sc-domain:webtor.io <start> <end> query,page'
```

У DataForSEO месячный кап $5, каждый вызов пишется в ledger. Этот анализ стоил
$0.184.
