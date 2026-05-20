# Release Subscription — Fake-Door Experiment

Статус: **планируется**, ещё не реализовано.
Дата фиксации плана: 2026-05-20.

## Гипотеза

Webtor-юзер, попавший на страницу раздачи сериала в стадии показа (status = `Returning Series`), хотел бы получать уведомления о новых сериях **именно этой раздачи** — того же релизера, в том же качестве, с той же аудиодорожкой. Если гипотеза верна, это открывает release-level subscription как retention-механику, аналогичную тому что Sonarr делает для self-hosted power users.

## Контекст

Решение делать fake-door, а не сразу полноценную фичу, пришло после анализа данных (см. ниже). Прежде чем вкладываться в схему БД, matching engine, fallback-логику и парсер-фиксы — валидируем интент дешёвой пробой.

### Что показал анализ данных (на 2026-05-20)

- Аудитория airing-сериалов фактически малая: **261 уникальный юзер** смотрели хотя бы один эпизод `Returning Series` за последние 90 дней. Из них **213 в свежей когорте** (≤90 дней с регистрации).
- Watchlist как фича практически мёртв: **140 юзеров из 37k** (0.38%) имеют что-то в `movie_watchlist`/`series_watchlist`. У свежей платной когорты adoption ещё ниже.
- Концентрация релиз-групп внутри сезона — **высокая**: в 84.8% (28/33) сезонов топ-10 airing-сериалов одна release group покрывает ≥80% эпизодов. То есть подписка "на эту раздачу" технически выживает в течение сезона.
- Парсер `parse_torrent_name` извлекает group в ~70-100% случаев, с детерминированными edge-багами (trailing `)`, `[Indexer.tld]` суффикс, audio-channel префикс). Для exact-string fingerprint matching внутри одного сезона это не блокер.

### Что мы НЕ делаем на этой стадии

- Не парсим release group в боевой логике (только сохраняем raw в Umami event-property для post-hoc analysis).
- Не fingerprint'им раздачу.
- Не строим matching engine.
- Не пишем в БД (никаких миграций, моделей, handler'ов).
- Не правим парсер.
- Не делаем post-episode overlay в плеере (одна точка показа — страница раздачи).
- Не делаем rail "готово смотреть".
- Не отправляем email/push никому.

Всё это — после того, как fake-door покажет CTR ≥20%.

## UX

### Где показываем

Один блок на странице ресурса, под file-tree, перед метаданными. Не блокирующий, не модалка.

### Eligibility

Баннер рендерится только если:
- Ресурс распознан как сериальный (есть `episode` запись связанная с `series_metadata` через `series.series_metadata_id`).
- Сериал есть в `tmdb.info` со `metadata->>'status' = 'Returning Series'` (или `in_production = true`).
- Юзер не дисмиссил баннер для этой раздачи ранее (флаг в localStorage, ключом `release_sub_dismissed:<resource_id>`).

Eligibility-чек идёт **server-side** на render страницы — TMDB-статус читается из локальной БД (`tmdb.info`), это один JOIN к существующему flow.

### Аудитория показа

- **Все**: free, paid, **и анонимы**. Анонимы намеренно включены — fake-door одновременно проверяет интент И работает как driver для anon → registration funnel.
- Никакого recency-фильтра. Любой ресурс airing-серии — баннер виден сразу, даже если юзер ещё не стримил эпизод.

### Копирайт

Баннер сформулирован как **опрос**, не как обещание. Без БД мы не можем честно сказать "напишем когда заработает".

```
🟣 СКОРО
Подписка на новые серии этой раздачи

Когда раздача обновится новой серией от того же релизера —
покажем в твоей подборке.

[Полезно]    [Не нужно]    [✕]
```

### Поведение по клику

| Клик | Авторизованный | Анон |
|---|---|---|
| **Полезно** | Баннер схлопывается → "Спасибо за фидбек." Umami event `release-subscribe-banner-yes`. | То же + secondary CTA "Зарегистрируйся чтобы получать уведомления когда фича появится" (мягкий, не модалка) |
| **Не нужно** | Баннер схлопывается → "Спасибо за фидбек." Umami event `release-subscribe-banner-no`. localStorage dismiss. | То же |
| **✕** | Баннер закрывается. Umami event `release-subscribe-banner-dismissed`. localStorage dismiss. | То же |

После любого из действий баннер не показывается на этой раздаче снова (localStorage). На других airing-раздачах — показывается заново (нам важна разнообразие сигналов по разным шоу).

## Tracking

Всё через Umami. **Никакой БД-таблицы.**

### События

Имена в kebab-case — единый стиль с остальными webtor Umami-событиями (`grace-soft-cta-shown`, `no-peers-shown`, `discover-see-more-click`).

| Event name | Когда |
|---|---|
| `release-subscribe-banner-shown` | Баннер отрендерен и виден ≥50% в viewport (через IntersectionObserver, single-shot) |
| `release-subscribe-banner-yes` | Клик "Полезно" |
| `release-subscribe-banner-no` | Клик "Не нужно" |
| `release-subscribe-banner-dismissed` | Клик ✕ |
| `release-subscribe-banner-register-click` | (только для анонов) Клик secondary CTA "Зарегистрироваться" |

### Properties на каждом событии

```json
{
  "series_title":      "The Boys",
  "series_video_id":   "tt1190634",
  "season":            5,
  "resource_id":       "8d3b1e1a740...",
  "release_group_raw": "FLUX",
  "tier":              "anon" | "free" | "paid",
  "lang":              "en" | "ru" | "es" | ...,
  "user_id":           "<uuid>"  // null для анонов
}
```

`release_group_raw` извлекается через `parse_torrent_name` на render баннера. Если парсер не вернул group — пишем пустую строку, **не блокируем показ баннера** (хотим понять и эти кейсы тоже).

### Дедупликация в анализе

- Авторизованные: dedupe по `user_id`
- Анонимы: dedupe по Umami `session_id` (он уже трекается дефолтно)

Конверсия считается на **уникальных юзерах** (registered) и **уникальных сессиях** (anon), сегментировано.

## Метрики и decision gate

### Primary metric

`CTR = unique(release-subscribe-banner-yes) / unique(release-subscribe-banner-shown)`

Считается отдельно по сегментам:
- `tier = anon`
- `tier = free`
- `tier = paid`

### Decision gate

| CTR (по primary сегменту — paid+free объединённо) | Решение |
|---|---|
| **≥20%** | Сильный сигнал. Строим полноценную фичу (release-level подписка, matching, rail, парсер-фиксы — см. план в чате). |
| 10-20% | Маргинальный. До commit'а на инфру — пробуем 2-3 вариации copy/placement, ещё ~2 недели. |
| 5-10% | Слабый. Не строим release-level подписку. Возвращаемся к series-level (как изначально предлагалось) или к другим retention-механикам. |
| **<5%** | Гипотеза опровергнута. Поведенческая модель webtor-юзера — transactional, не subscription-driven. Перестраиваем retention-стратегию вокруг этого. |

### Secondary metrics (для понимания, не для gate)

- CTR по сериалам — где работает лучше: топ-5 (The Boys, FROM, INVINCIBLE, Daredevil, Euphoria) или long tail?
- CTR по языку аудио — RU аудитория с конкретным переводом vs EN.
- Anon → registration conversion после клика "Полезно" — даёт fake-door дополнительную ценность даже если основная гипотеза провалится.
- Free vs Paid CTR разница — гипотеза была "free кликают больше (страх пропустить), paid реже (всегда могут зайти)". Проверим.
- Доля "Не нужно" vs "✕" vs игнор. Если "Не нужно" >50% — баннер раздражает, не просто не цепляет.

### Stop-condition

Гибкая. Минимум — **2000 уникальных impressions** для значимости. Если набор идёт быстро (за 1-2 недели) — снимаем cohort, анализируем, решаем. Если медленно — продолжаем до 4 недель максимум.

## Объём работ

| Задача | Где | Оценка |
|---|---|---|
| Eligibility helper "is airing series episode page?" | server-side в resource-handler | 2 ч |
| Парсинг release_group для event property (server-side вызов `parse_torrent_name` на render) | в том же helper'е | 1 ч |
| Шаблон баннера | `templates/partials/resource/release_subscribe_banner.html` | 2 ч |
| JS: click handlers, IntersectionObserver impression, localStorage dismiss, Umami calls | `assets/src/js/...` | 2 ч |
| Anon secondary CTA → registration link | в шаблоне + JS | 30 мин |
| Этот документ | `docs/release_sub_fake_door.md` | — |

**Итого: ~1 рабочий день.**

## Что нужно проверить перед стартом разработки

- `user_id` в Umami events: пробрасывается ли он сейчас для зарегистрированных? Если нет — добавить в общий wrapper для всех событий, не только для этого эксперимента.
- IntersectionObserver-based impression tracking уже где-то используется в webtor? Если нет — переиспользуемый хелпер на будущее.
- Umami event data — ограничение на размер property: проверить лимиты, иначе обрезаем `series_title` и т.п.

## После эксперимента

### Если passed (≥20% CTR)

Переход к полноценному плану (зафиксирован в чате 2026-05-20):
- Sprint 1 (2 недели): миграции `56_release_subscription` + `57_release_subscription_dismissals`, модель, handler, matching engine как hook на enrichment torrent-store, rail "Готово смотреть".
- Sprint 1 параллельно (3 дня): парсер-фиксы (trailing punctuation, indexer suffix, audio-channel prefix) + investigate The Rookie S6 0%-extraction.
- Sprint 2 по результатам Sprint 1: email digest / web push / fallback "раздача замолкла".

Подписка scope'ом **release × season** (не series), fingerprint **medium** (tmdb_id + season + group + quality + audio_language).

### Если failed

Документируем в этом же файле раздел "## Результаты" — что увидели, как сегментировалось, какие выводы для следующих экспериментов. Не удаляем — нужен archival контекст для будущих product-решений.
