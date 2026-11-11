'use strict';
// The owner's console of Pluma (task 072, ADR 0040). No build step, no
// dependencies: the Content-Security-Policy allows only this file.

const $ = (id) => document.getElementById(id);
const main = $('main');
const titleEl = $('title');

const b64 = (buf) => btoa(String.fromCharCode(...new Uint8Array(buf))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
const unb64 = (s) => Uint8Array.from(atob(s.replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0));
const el = (tag, { dataset, ...props } = {}, ...kids) => {
  const e = Object.assign(document.createElement(tag), props);
  if (dataset) Object.assign(e.dataset, dataset);
  e.append(...kids.filter((k) => k !== null && k !== undefined));
  return e;
};
const dateOf = (t) => (t ? new Date(t * 1000).toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' }) : '—');
const plural = (n, one, few, many) => {
  const m = Math.abs(n) % 100; const d = m % 10;
  if (m > 10 && m < 20) return many;
  if (d > 1 && d < 5) return few;
  return d === 1 ? one : many;
};
const fmtBytes = (n) => {
  if (!n) return '—';
  const u = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ']; let i = 0; let v = n;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return `${v >= 10 || i === 0 ? v.toFixed(0) : v.toFixed(1)} ${u[i]}`;
};
const phoneShown = (p) => (p ? '+' + p : '—');
const nameOf = (u) => [u.first_name, u.last_name].filter(Boolean).join(' ') || 'Без имени';
const initial = (s) => (s || '?').trim().slice(0, 1).toUpperCase();

// Words of the server's refusals in the panel's language; an unknown one is
// shown as it came.
const refusals = {
  'block the account first': 'Сначала заблокируйте аккаунт.',
  'block it first': 'Сначала заблокируйте.',
  'the confirmation does not match': 'Подтверждение не совпало.',
  'confirm with the passkey': 'Подтвердите ключом доступа.',
  'passkey refused': 'Ключ доступа не принят.',
  'no pending request': 'Нет ожидающего запроса кода.',
  'the number is not allowed to log in': 'Этому номеру вход не разрешён.',
  'login codes are not configured': 'Коды входа не настроены.',
  'the service account cannot be changed': 'Служебный аккаунт менять нельзя.',
  'the service refused the change': 'Сервис не принял изменение.',
  'the service refused the deletion': 'Сервис не принял удаление.',
  'the account could not be deleted': 'Не удалось удалить аккаунт: повторите.',
  'the account is deleted': 'Аккаунт удалён.',
  'the message could not be sent': 'Сообщение не отправлено.',
  'the sessions could not be ended': 'Сеансы не завершены.',
  'the last passkey cannot be removed': 'Нельзя удалить последний ключ.',
  'the owner cannot lock himself out': 'Нельзя отключить самого себя.',
  'no recipients': 'Получателей нет.',
  'the broadcast is over': 'Рассылка уже закончена.',
  'bad phone number': 'Неверный номер.',
  'no such user': 'Пользователь не найден.',
  'no such peer': 'Не найдено.',
  'no such report': 'Жалоба не найдена.',
  'already decided': 'Жалоба уже решена.',
  'the date has passed': 'Эта дата уже прошла.',
  'internal error': 'Внутренняя ошибка сервера.',
};

class ApiError extends Error {
  constructor(status, message, data) { super(message); this.status = status; this.data = data; }
}

async function api(path, opts = {}) {
  let r;
  try {
    r = await fetch(path, { ...opts, headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) } });
  } catch (e) {
    throw new ApiError(0, 'Сервер не отвечает. Проверьте соединение.');
  }
  if (r.status === 401) { await enter(); throw new ApiError(401, 'Войдите с ключом доступа.'); }
  if (!r.ok) {
    const text = (await r.text()).trim();
    let data = null;
    try { data = JSON.parse(text); } catch (e) { /* a plain message */ }
    throw new ApiError(r.status, refusals[text] || text || r.statusText, data);
  }
  return r.status === 204 ? null : r.json();
}
const post = (path, body) => api(path, { method: 'POST', body: JSON.stringify(body ?? {}) });

// Passkeys.
function hexToBytes(hex) {
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < hex.length; i += 2) out[i / 2] = parseInt(hex.slice(i, i + 2), 16);
  return out;
}
function creationOptions(o) {
  const c = structuredClone(o.publicKey);
  c.challenge = unb64(c.challenge);
  c.user.id = unb64(c.user.id);
  c.excludeCredentials = (c.excludeCredentials || []).map((d) => ({ ...d, id: unb64(d.id) }));
  return c;
}
function requestOptions(o) {
  const c = structuredClone(o.publicKey);
  c.challenge = hexToBytes(o.challengeHex);
  c.allowCredentials = (c.allowCredentials || []).map((d) => ({ ...d, id: unb64(d.id) }));
  return c;
}
async function getAssertion(begin) {
  const options = await begin();
  let cred;
  try {
    cred = await navigator.credentials.get({ publicKey: requestOptions(options) });
  } catch (e) {
    throw new Error('Подтверждение ключом доступа отменено.');
  }
  return {
    credentialId: b64(cred.rawId),
    clientDataJSON: b64(cred.response.clientDataJSON),
    authenticatorData: b64(cred.response.authenticatorData),
    signature: b64(cred.response.signature),
    challenge: options.challengeHex,
  };
}
// signed sends a dangerous action together with a passkey assertion made
// for exactly this action.
async function signed(path, payload) {
  const text = JSON.stringify(payload);
  const assertion = await getAssertion(() => post('/api/stepup/begin', { path, payload: text }));
  return post(path, { payload: text, assertion });
}
async function register() {
  const options = await post('/api/passkey/register/begin', {});
  const cred = await navigator.credentials.create({ publicKey: creationOptions(options) });
  await post('/api/passkey/register/finish', {
    challenge: options.challengeHex,
    clientDataJSON: b64(cred.response.clientDataJSON),
    attestationObject: b64(cred.response.attestationObject),
  });
}
const login = async () => {
  const assertion = await getAssertion(() => post('/api/login/begin', {}));
  await post('/api/login/finish', assertion);
};

// Rendering states.
function toast(text, err) {
  const t = el('div', { className: 'toast' + (err ? ' err' : ''), textContent: text });
  $('toasts').append(t);
  setTimeout(() => t.remove(), 4200);
}
const skeleton = (n = 4) => el('div', { className: 'skeleton', ariaLabel: 'Загрузка' }, ...Array.from({ length: n }, () => el('span')));
const empty = (text) => el('div', { className: 'state' }, el('p', { textContent: text }));
function errorState(message, retry) {
  return el('div', { className: 'state error' },
    el('p', { textContent: message || 'Не удалось загрузить.' }),
    retry ? el('button', { className: 'btn', textContent: 'Повторить', onclick: retry }) : null);
}
function show(...nodes) { main.replaceChildren(...nodes); }

// action runs a user's action on its button: busy while running, a toast on
// refusal; ok (a message or a function) after it.
function action(button, fn, ok) {
  return async () => {
    if (button) button.disabled = true;
    try {
      const out = await fn();
      if (typeof ok === 'function') ok(out); else if (ok) toast(ok);
    } catch (e) {
      toast(e.message || 'Ошибка', true);
    } finally {
      if (button) button.disabled = false;
    }
  };
}
function btn(text, cls, fn) {
  const b = el('button', { className: 'btn' + (cls ? ' ' + cls : ''), type: 'button', textContent: text });
  if (fn) b.onclick = () => fn(b);
  return b;
}

// page loads a section: a skeleton while loading, an error with a retry.
async function page(title, load, n = 4) {
  titleEl.textContent = title;
  document.title = `${title} · Pluma`;
  show(skeleton(n));
  const route = location.hash;
  const retry = () => { if (location.hash === route) rerender(); };
  try {
    const nodes = await load();
    if (location.hash === route) show(...(Array.isArray(nodes) ? nodes : [nodes]));
  } catch (e) {
    if (location.hash === route) show(errorState(e.message, retry));
  }
}

// Dialogs.
function dialog(build) {
  return new Promise((resolve) => {
    const close = (v) => { node.remove(); resolve(v); };
    const body = build(close);
    const node = el('div', { className: 'modal', onclick: (e) => { if (e.target === node) close(null); } }, el('div', { className: 'sheet', role: 'dialog', ariaModal: 'true' }, ...body));
    document.body.append(node);
    const first = node.querySelector('input, textarea, select');
    if (first) first.focus();
  });
}
function confirmDialog({ title, text, confirmLabel, danger, typed, placeholder }) {
  return dialog((close) => {
    const inp = typed ? el('input', { placeholder: placeholder || typed, autocomplete: 'off', inputMode: 'text' }) : null;
    const go = btn(confirmLabel || 'Подтвердить', danger ? 'solid-danger' : 'primary', () => close(inp ? inp.value : true));
    if (inp) {
      go.disabled = true;
      inp.oninput = () => { go.disabled = inp.value.trim() !== typed; };
    }
    return [el('h2', { textContent: title }), el('p', { className: 'muted', textContent: text }), inp,
      el('div', { className: 'actions' }, go, btn('Отмена', '', () => close(null)))];
  });
}

// Durations.
const durations = [
  { label: 'Сутки', secs: 86400 }, { label: 'Неделя', secs: 604800 },
  { label: 'Месяц', secs: 2592000 }, { label: 'Навсегда', secs: 0 },
];
function durationPicker() {
  let cur = durations[3];
  const buttons = durations.map((d) => {
    const b = el('button', { type: 'button', textContent: d.label, ariaPressed: String(d === cur) });
    b.onclick = () => { cur = d; buttons.forEach((x, i) => { x.ariaPressed = String(durations[i] === cur); }); };
    return b;
  });
  return {
    node: el('div', { className: 'segmented' }, ...buttons),
    until: () => (cur.secs ? Math.floor(Date.now() / 1000) + cur.secs : 0),
  };
}

// Sections.
const sections = [
  { hash: 'overview', label: 'Обзор' },
  { hash: 'users', label: 'Пользователи' },
  { hash: 'peers', label: 'Каналы и группы' },
  { hash: 'reports', label: 'Жалобы' },
  { hash: 'registration', label: 'Регистрация' },
  { hash: 'broadcast', label: 'Рассылка' },
  { hash: 'releases', label: 'Выпуски' },
  { hash: 'system', label: 'Система' },
  { hash: 'audit', label: 'Журнал' },
  { hash: 'access', label: 'Доступ' },
];
function buildNav() {
  $('nav').replaceChildren(...sections.map((s) => el('a', { href: '#' + s.hash, textContent: s.label, dataset: { hash: s.hash } })));
}
function markNav(hash) {
  const root = hash.split('/')[0];
  const active = root === 'report' ? 'reports' : root;
  document.querySelectorAll('#nav a').forEach((a) => { if (a.dataset.hash === active) a.ariaCurrent = 'page'; else a.removeAttribute('aria-current'); });
  closeMenu();
}

async function route() {
  const hash = location.hash.slice(1) || 'overview';
  markNav(hash);
  const [a, b, c] = hash.split('/');
  if (a === 'overview') return overviewPage();
  if (a === 'users') return b ? userPage(b) : usersPage();
  if (a === 'peers') return b ? peerPage(b, c) : peersPage();
  if (a === 'reports') return reportsPage(b === 'decided');
  if (a === 'report') return reportPage(b);
  if (a === 'registration') return registrationPage();
  if (a === 'broadcast') return broadcastPage();
  if (a === 'releases') return releasesPage();
  if (a === 'system') return systemPage();
  if (a === 'audit') return auditPage();
  if (a === 'access') return accessPage();
  titleEl.textContent = 'Нет такой страницы';
  return show(errorState('Нет такой страницы', () => { location.hash = 'overview'; }));
}
const rerender = () => route();

// Overview.
function stat(n, label, problem, href) {
  const body = [el('span', { className: 'n', textContent: String(n) }), el('span', { className: 'l', textContent: label })];
  return href ? el('a', { className: 'stat' + (problem ? ' problem' : ''), href }, ...body) : el('div', { className: 'stat' + (problem ? ' problem' : '') }, ...body);
}
async function overviewPage() {
  await page('Обзор', async () => {
    const o = await api('/api/overview');
    const c = o.counters;
    const stats = [
      stat(c.users, 'пользователей', false, '#users'),
      stat(c.online, 'сейчас в сети'),
      stat(c.messages, 'сообщений за сутки'),
      stat(c.registrations, 'регистраций за сутки'),
      stat(c.open_reports, 'новых жалоб', c.open_reports > 0, '#reports'),
    ];
    if (o.disk) {
      const pct = Math.round((1 - o.disk.available / o.disk.size) * 100);
      stats.push(stat(`${pct}%`, `диск занят (свободно ${fmtBytes(o.disk.available)})`, pct >= 85, '#system'));
    }
    if (o.services_down !== undefined) stats.push(stat(`${o.services_down} из ${o.services_total}`, 'сервисов не работает', o.services_down > 0, '#system'));
    if (o.dead_events) stats.push(stat(o.dead_events, 'зависших событий', true, '#system'));
    const alerts = (o.alerts || []).filter((a) => a.state === 'firing');
    const alertsCard = el('div', { className: 'card' }, el('h2', { textContent: 'Проблемы' }),
      alerts.length
        ? el('div', { className: 'list' }, ...alerts.map(alertRow))
        : o.alerts_error ? el('p', { className: 'muted', textContent: 'Prometheus не отвечает: состояние неизвестно.' })
          : el('p', { className: 'muted', textContent: 'Сейчас всё спокойно.' }));
    return [el('div', { className: 'grid' }, ...stats), alertsCard];
  }, 3);
}
function alertRow(a) {
  return el('div', { className: 'item' },
    el('div', { className: 'grow' }, el('div', { className: 'title', textContent: a.summary || a.name }),
      el('div', { className: 'sub', textContent: [a.name, a.severity === 'critical' ? 'критично' : a.severity, a.instance].filter(Boolean).join(' · ') })),
    el('span', { className: 'badge ' + (a.severity === 'critical' ? 'danger' : 'warn'), textContent: a.state === 'firing' ? 'горит' : 'ожидание' }));
}

// Users.
const restrictionBadge = (kind) => (kind === 2 ? el('span', { className: 'badge danger', textContent: 'заблокирован' }) : kind === 1 ? el('span', { className: 'badge warn', textContent: 'ограничен' }) : null);
const restrictedNow = (u) => u.restrict_kind && (!u.restrict_until || u.restrict_until > Date.now() / 1000);
function userRow(u) {
  return el('a', { className: 'item', href: `#users/${u.id}` },
    el('span', { className: 'avatar', textContent: initial(u.first_name) }),
    el('div', { className: 'grow' },
      el('div', { className: 'title', textContent: nameOf(u) }),
      el('div', { className: 'sub', textContent: [phoneShown(u.phone), u.username ? '@' + u.username : null, `id ${u.id}`].filter(Boolean).join(' · ') })),
    restrictedNow(u) ? restrictionBadge(u.restrict_kind) : null,
    u.deleted ? el('span', { className: 'badge', textContent: 'удалён' }) : null);
}
function searchBox(placeholder, onSearch) {
  const inp = el('input', { type: 'search', placeholder, autocomplete: 'off' });
  let t = null;
  inp.oninput = () => { clearTimeout(t); t = setTimeout(() => onSearch(inp.value), 300); };
  return inp;
}
function usersPage() {
  titleEl.textContent = 'Пользователи';
  document.title = 'Пользователи · Pluma';
  const list = el('div', { className: 'list' });
  const load = async (q) => {
    list.replaceChildren(skeleton(5));
    try {
      const { users } = await api('/api/users?q=' + encodeURIComponent(q || ''));
      list.replaceChildren(...(users?.length ? users.map(userRow) : [empty(q ? 'Никого не нашлось.' : 'Пользователей пока нет.')]));
    } catch (e) {
      list.replaceChildren(errorState(e.message, () => load(q)));
    }
  };
  show(el('div', { className: 'card' }, searchBox('Номер, имя, @username или id', load), list));
  load('');
}

function reportRow(r) {
  return el('a', { className: 'item', href: `#report/${r.id}` },
    el('div', { className: 'grow' },
      el('div', { className: 'title', textContent: `${reportKinds[r.kind] || 'жалоба'}: ${reasons[r.reason] || r.reason}` }),
      el('div', { className: 'sub', textContent: `#${r.id} · ${dateOf(r.created_at)}` })),
    r.status === 0 ? el('span', { className: 'badge warn', textContent: 'новая' }) : el('span', { className: 'badge', textContent: actionLabels[r.decision] || r.decision }));
}

async function userPage(id) {
  await page('Пользователь', async () => {
    const { user: u, restriction, devices, devices_error, reports, protected: prot } = await api('/api/users/' + id);
    titleEl.textContent = nameOf(u);
    const banned = restriction && restriction.kind === 2;
    const props = el('dl', { className: 'props' },
      el('dt', { textContent: 'Телефон' }), el('dd', { className: 'mono', textContent: phoneShown(u.phone) }),
      el('dt', { textContent: 'Id' }), el('dd', { className: 'mono', textContent: String(u.id) }),
      el('dt', { textContent: 'Имя пользователя' }), el('dd', { textContent: u.username ? '@' + u.username : '—' }),
      u.about ? el('dt', { textContent: 'О себе' }) : null, u.about ? el('dd', { textContent: u.about }) : null,
      el('dt', { textContent: 'Регистрация' }), el('dd', { textContent: dateOf(u.created_at) }),
      el('dt', { textContent: 'Был в сети' }), el('dd', { textContent: u.last_seen ? dateOf(u.last_seen) : 'нет данных' }),
      u.is_bot ? el('dt', { textContent: 'Тип' }) : null, u.is_bot ? el('dd', { textContent: 'бот' }) : null);
    const header = el('div', { className: 'row between' },
      el('div', { className: 'row' }, el('span', { className: 'avatar', textContent: initial(u.first_name) }), el('h2', { className: 'break', textContent: nameOf(u) })),
      el('div', { className: 'row' }, u.deleted ? el('span', { className: 'badge', textContent: 'удалён' }) : null, restriction ? restrictionBadge(restriction.kind) : null));
    const cards = [el('div', { className: 'card' }, header, props)];
    if (restriction) {
      cards.push(el('div', { className: 'card' }, el('h2', { textContent: restriction.kind === 2 ? 'Заблокирован' : 'Ограничен' }),
        el('p', { textContent: `${restriction.until_date ? 'До ' + dateOf(restriction.until_date) : 'Бессрочно'}${restriction.reason ? ' · ' + restriction.reason : ''}${restriction.created_by ? ' · ' + restriction.created_by : ''}` }),
        el('div', { className: 'actions' }, btn('Снять', '', (b) => {
          action(b, () => signed(`/api/users/${u.id}/lift`, { reason: 'снято в панели' }), () => { toast('Снято'); rerender(); })();
        }))));
    }
    if (!prot && !u.deleted) {
      const acts = [
        btn('Завершить сеансы', '', (b) => actTerminate(u, b)),
        btn('Написать от Pluma', '', () => actMessage(u)),
        btn('Показать код входа', '', (b) => actCode(u, b)),
      ];
      if (!banned) {
        if (!restriction) acts.push(btn('Ограничить', 'danger', () => actRestrict(u, 'restrict')));
        acts.push(btn('Заблокировать', 'danger', () => actRestrict(u, 'ban')));
      } else {
        acts.push(btn('Удалить аккаунт…', 'danger', (b) => actDeleteUser(u, b)));
      }
      cards.push(el('div', { className: 'card' }, el('h2', { textContent: 'Действия' }), el('div', { className: 'actions' }, ...acts),
        el('p', { className: 'muted small', textContent: 'Опасные действия подтверждаются ключом доступа и пишутся в журнал. Удалить можно только заблокированный аккаунт.' })));
    }
    cards.push(el('div', { className: 'card' }, el('h2', { textContent: `Устройства` }),
      devices_error ? el('p', { className: 'muted', textContent: 'Не удалось получить устройства.' })
        : devices?.length ? el('div', { className: 'list' }, ...devices.map(deviceRow))
          : el('p', { className: 'muted', textContent: 'Нет активных сеансов.' })));
    cards.push(el('div', { className: 'card' }, el('h2', { textContent: 'Жалобы на пользователя' }),
      reports?.length ? el('div', { className: 'list' }, ...reports.map(reportRow)) : el('p', { className: 'muted', textContent: 'Жалоб нет.' })));
    return cards;
  });
}
function deviceRow(d) {
  return el('div', { className: 'item' }, el('div', { className: 'grow' },
    el('div', { className: 'title', textContent: [d.device_model, d.platform && d.system_version ? `${d.platform} ${d.system_version}` : d.platform].filter(Boolean).join(' · ') || 'Неизвестное устройство' }),
    el('div', { className: 'sub', textContent: [d.app_name && d.app_version ? `${d.app_name} ${d.app_version}` : d.app_name, d.country, d.ip, d.date_active ? 'активно ' + dateOf(d.date_active) : null].filter(Boolean).join(' · ') })));
}
function actTerminate(u, b) {
  action(b, async () => {
    const ok = await confirmDialog({ title: 'Завершить все сеансы?', text: `${nameOf(u)} выйдет на всех устройствах.`, confirmLabel: 'Завершить', danger: true });
    if (!ok) return null;
    await signed(`/api/users/${u.id}/terminate`, { reason: '' });
    toast('Сеансы завершены');
    rerender();
  })();
}
async function actMessage(u) {
  const text = await dialog((close) => {
    const area = el('textarea', { placeholder: 'Текст сообщения', maxLength: 4096, rows: 5 });
    const go = btn('Отправить', 'primary', () => close(area.value.trim()));
    return [el('h2', { textContent: 'Сообщение от Pluma' }), el('p', { className: 'muted', textContent: `${nameOf(u)} получит его в чате Pluma.` }), area,
      el('div', { className: 'actions' }, go, btn('Отмена', '', () => close(null)))];
  });
  if (!text) return;
  action(null, () => signed(`/api/users/${u.id}/message`, { text }), 'Отправлено')();
}
function actRestrict(u, kind) {
  dialog((close) => {
    const reason = el('input', { placeholder: 'Причина (видна в журнале)', maxLength: 512 });
    const picker = durationPicker();
    const label = kind === 'ban' ? 'Заблокировать' : 'Ограничить';
    const go = btn(label, kind === 'ban' ? 'solid-danger' : 'primary', () => close({ reason: reason.value, until: picker.until() }));
    return [el('h2', { textContent: `${label}: ${nameOf(u)}` }),
      el('p', { className: 'muted', textContent: kind === 'ban' ? 'Все сеансы будут завершены, вход закрыт.' : 'Писать можно только контактам; в группах и каналах — нельзя.' }),
      el('label', { className: 'field' }, el('span', { textContent: 'Причина' }), reason),
      el('div', { className: 'field' }, el('span', { className: 'muted small', textContent: 'Срок' }), picker.node),
      el('div', { className: 'actions' }, go, btn('Отмена', '', () => close(null)))];
  }).then((v) => {
    if (!v) return;
    action(null, () => signed(`/api/users/${u.id}/restrict`, { action: kind, ...v }), () => { toast('Готово'); rerender(); })();
  });
}
function actCode(u, b) {
  action(b, async () => {
    const out = await requestCode((reqId) => signed(`/api/users/${u.id}/code`, { request_id: reqId || '' }));
    if (out) showCode(out, phoneShown(u.phone));
  })();
}
// requestCode shows a code; when several requests are pending it asks which.
async function requestCode(call, reqId = '') {
  try {
    return await call(reqId);
  } catch (e) {
    if (e.status === 409 && e.data?.pending) {
      const chosen = await pickRequest(e.data.pending);
      return chosen ? requestCode(call, chosen) : null;
    }
    throw e;
  }
}
function pickRequest(pending) {
  return dialog((close) => [
    el('h2', { textContent: 'Запросов несколько' }),
    el('p', { className: 'muted', textContent: 'Запрос может сделать кто угодно, кто знает номер. Уточните у владельца, откуда и когда он просил код.' }),
    el('div', { className: 'list' }, ...pending.map((p) => el('button', {
      className: 'item link', type: 'button', onclick: () => close(p.id),
    }, el('div', { className: 'grow' }, el('div', { className: 'title', textContent: `Запрос ${dateOf(p.requested_at)}` }),
      el('div', { className: 'sub', textContent: `${p.client_ip || 'адрес неизвестен'} · проверок использовано: ${p.attempts}` }))))),
    btn('Отмена', '', () => close(null)),
  ]);
}
function showCode(out, label) {
  dialog((close) => [
    el('h2', { textContent: 'Код входа' }),
    el('div', { className: 'code' }, el('b', { textContent: out.code }),
      el('span', { className: 'small', textContent: `Для ${label}, действует до ${dateOf(out.expires_at)}` })),
    el('p', { className: 'muted small', textContent: 'Передайте только владельцу номера и по надёжному каналу. Код больше не показывается.' }),
    btn('Закрыть', 'primary', () => close(true)),
  ]);
}
function actDeleteUser(u, b) {
  action(b, async () => {
    const id = String(u.id);
    const v = await confirmDialog({ title: `Удалить аккаунт ${nameOf(u)}?`, text: `Это необратимо. Введите id ${id} для подтверждения.`, confirmLabel: 'Удалить навсегда', danger: true, typed: id });
    if (!v) return;
    await signed(`/api/users/${u.id}/delete`, { confirm: v.trim() });
    toast('Аккаунт удалён');
    location.hash = 'users';
  })();
}

// Channels and groups.
function peerKindLabel(p) {
  return p.kind === 'chat' ? 'Группа' : p.broadcast ? 'Канал' : 'Супергруппа';
}
function peerRow(p) {
  return el('a', { className: 'item', href: `#peers/${p.kind}/${p.id}` },
    el('span', { className: 'avatar', textContent: initial(p.title) }),
    el('div', { className: 'grow' },
      el('div', { className: 'title', textContent: p.title || 'Без названия' }),
      el('div', { className: 'sub', textContent: [peerKindLabel(p), p.username ? '@' + p.username : null, `${p.participants} ${plural(p.participants, 'участник', 'участника', 'участников')}`, `id ${p.id}`].filter(Boolean).join(' · ') })),
    p.deleted ? el('span', { className: 'badge', textContent: 'удалён' }) : p.blocked ? el('span', { className: 'badge danger', textContent: 'заблокирован' }) : null);
}
function peersPage() {
  titleEl.textContent = 'Каналы и группы';
  document.title = 'Каналы и группы · Pluma';
  const list = el('div', { className: 'list' });
  const load = async (q) => {
    list.replaceChildren(skeleton(5));
    try {
      const { peers } = await api('/api/peers?q=' + encodeURIComponent(q || ''));
      list.replaceChildren(...(peers?.length ? peers.map(peerRow) : [empty(q ? 'Ничего не нашлось.' : 'Каналов и групп пока нет.')]));
    } catch (e) {
      list.replaceChildren(errorState(e.message, () => load(q)));
    }
  };
  show(el('div', { className: 'card' }, searchBox('Название, @username или id', load), list));
  load('');
}
async function peerPage(kind, id) {
  await page('Канал', async () => {
    const { peer: p, owner, reports, history } = await api(`/api/peers/${kind}/${id}`);
    titleEl.textContent = p.title || peerKindLabel(p);
    const base = `/api/peers/${kind}/${id}`;
    const block = btn('Заблокировать', 'danger', () => {
      dialog((close) => {
        const reason = el('input', { placeholder: 'Причина (видна в журнале)', maxLength: 512 });
        return [el('h2', { textContent: `Заблокировать: ${p.title}` }),
          el('p', { className: 'muted', textContent: 'Никто не сможет открыть и писать сюда. Участники останутся, блокировку можно снять.' }),
          el('label', { className: 'field' }, el('span', { textContent: 'Причина' }), reason),
          el('div', { className: 'actions' }, btn('Заблокировать', 'solid-danger', () => close(reason.value || '')), btn('Отмена', '', () => close(null)))];
      }).then((r) => {
        if (r === null) return;
        action(null, () => signed(base + '/block', { reason: r }), () => { toast('Заблокировано'); rerender(); })();
      });
    });
    const unblock = btn('Снять блокировку', '', (b) => action(b, async () => {
      if (!await confirmDialog({ title: 'Снять блокировку?', text: `${p.title} снова станет доступен участникам.`, confirmLabel: 'Снять' })) return;
      await signed(base + '/unblock', { reason: '' });
      toast('Снято'); rerender();
    })());
    const del = btn('Удалить…', 'danger', (b) => action(b, async () => {
      const typed = String(p.id);
      const v = await confirmDialog({ title: `Удалить «${p.title}»?`, text: `Удаляется у всех участников, необратимо. Введите id ${typed}.`, confirmLabel: 'Удалить навсегда', danger: true, typed });
      if (!v) return;
      await signed(base + '/delete', { confirm: v.trim() });
      toast('Удалено'); location.hash = 'peers';
    })());
    const actions = p.deleted ? null : el('div', { className: 'actions' }, ...(p.blocked ? [unblock, del] : [block]));
    return [
      el('div', { className: 'card' },
        el('div', { className: 'row between' }, el('h2', { className: 'break', textContent: p.title }),
          p.deleted ? el('span', { className: 'badge', textContent: 'удалён' }) : p.blocked ? el('span', { className: 'badge danger', textContent: 'заблокирован' }) : el('span', { className: 'badge ok', textContent: peerKindLabel(p) })),
        el('dl', { className: 'props' },
          el('dt', { textContent: 'Тип' }), el('dd', { textContent: peerKindLabel(p) }),
          el('dt', { textContent: 'Id' }), el('dd', { className: 'mono', textContent: String(p.id) }),
          el('dt', { textContent: 'Участников' }), el('dd', { textContent: String(p.participants) }),
          el('dt', { textContent: 'Создан' }), el('dd', { textContent: dateOf(p.date) }),
          p.username ? el('dt', { textContent: 'Адрес' }) : null, p.username ? el('dd', { textContent: '@' + p.username }) : null,
          el('dt', { textContent: 'Владелец' }), el('dd', {}, owner ? el('a', { href: `#users/${owner.id}`, textContent: nameOf(owner) }) : '—')),
        actions,
        el('p', { className: 'muted small', textContent: 'Удалить можно только заблокированное: блокировка обратима, удаление нет.' })),
      el('div', { className: 'card' }, el('h2', { textContent: 'Жалобы' }), reports?.length ? el('div', { className: 'list' }, ...reports.map(reportRow)) : el('p', { className: 'muted', textContent: 'Жалоб нет.' })),
      history?.length ? el('div', { className: 'card' }, el('h2', { textContent: 'В журнале' }), el('div', { className: 'list' }, ...history.map(auditRow))) : null,
    ];
  });
}

// Reports.
const reportKinds = { 1: 'Сообщение', 2: 'Профиль или чат', 3: 'Фото профиля', 4: 'Спам', 5: 'Спам в канале', 6: 'Ложное срабатывание антиспама' };
const reasons = { spam: 'спам', violence: 'насилие', pornography: 'порнография', child_abuse: 'насилие над детьми', copyright: 'авторские права', illegal_drugs: 'наркотики', personal_details: 'личные данные', fake: 'фейк', geo_irrelevant: 'не по геопозиции', not_spam: 'не спам', other: 'другое' };
const actionLabels = { dismiss: 'Отклонено', delete: 'Удалено', restrict: 'Ограничен', ban: 'Заблокирован' };
function reportsPage(decided) {
  const seg = el('div', { className: 'segmented' },
    el('a', { className: 'btn small' + (decided ? '' : ' primary'), href: '#reports', textContent: 'Новые' }),
    el('a', { className: 'btn small' + (decided ? ' primary' : ''), href: '#reports/decided', textContent: 'Решённые' }));
  return page('Жалобы', async () => {
    const { reports } = await api('/api/reports' + (decided ? '?status=decided' : ''));
    return [seg, el('div', { className: 'card' }, reports?.length ? el('div', { className: 'list' }, ...reports.map(reportRow)) : empty(decided ? 'Решённых жалоб пока нет.' : 'Новых жалоб нет.'))];
  });
}
async function reportPage(id) {
  await page(`Жалоба #${id}`, async () => {
    const { report: r, snapshot, restriction, history } = await api('/api/reports/' + id);
    const user = (uid) => snapshot.users?.find((u) => u.id === uid);
    const who = (uid) => { const u = user(uid); return u ? `${nameOf(u)}${u.username ? ' @' + u.username : ''}` : `id ${uid}`; };
    const msgs = (snapshot.messages || []).map((m) => {
      const node = el('div', { className: 'msg' }, el('div', { className: 'muted small', textContent: `${who(m.from_id)} · ${dateOf(m.date)}` }), m.text || '');
      if (m.media) {
        const url = `/api/reports/${id}/media/${m.id}`;
        if (m.media.mime && m.media.mime.startsWith('video/')) node.append(el('video', { src: url, controls: true }));
        else if (m.media.photo_id || (m.media.mime && m.media.mime.startsWith('image/'))) node.append(el('img', { src: url, alt: '' }));
        else node.append(el('div', { className: 'muted small', textContent: m.media.kind }));
      }
      return node;
    });
    const info = el('div', { className: 'card' }, el('div', { className: 'row between' }, el('h2', { textContent: `${reportKinds[r.kind] || 'Жалоба'}: ${reasons[r.reason] || r.reason}` }),
      r.status === 0 ? el('span', { className: 'badge warn', textContent: 'новая' }) : el('span', { className: 'badge', textContent: actionLabels[r.decision] || r.decision })),
    el('dl', { className: 'props' },
      el('dt', { textContent: 'От кого' }), el('dd', {}, el('a', { href: `#users/${r.reporter_user_id}`, textContent: who(r.reporter_user_id) })),
      el('dt', { textContent: 'Когда' }), el('dd', { textContent: dateOf(r.created_at) }),
      r.target_user_id ? el('dt', { textContent: 'На кого' }) : null,
      r.target_user_id ? el('dd', {}, el('a', { href: `#users/${r.target_user_id}`, textContent: who(r.target_user_id) })) : null,
      snapshot.peer_title ? el('dt', { textContent: 'Чат' }) : null, snapshot.peer_title ? el('dd', { textContent: snapshot.peer_title }) : null,
      r.comment ? el('dt', { textContent: 'Комментарий' }) : null, r.comment ? el('dd', { textContent: r.comment }) : null));
    const blocks = [info];
    if (restriction) {
      blocks.push(el('div', { className: 'card' }, el('p', { textContent: `Уже действует: ${restriction.kind === 2 ? 'блокировка' : 'ограничение'}, ${restriction.until_date ? 'до ' + dateOf(restriction.until_date) : 'бессрочно'}` }),
        el('div', { className: 'actions' }, btn('Снять', '', (b) => action(b, () => signed(`/api/users/${r.target_user_id}/lift`, { reason: 'снято в панели' }), () => { toast('Снято'); rerender(); })()))));
    }
    if (r.status !== 0) {
      const decided = el('div', { className: 'card' }, el('p', { textContent: `Решение: ${actionLabels[r.decision] || r.decision} · ${r.decided_by} · ${dateOf(r.decided_at)}${r.decision_reason ? ' · ' + r.decision_reason : ''}` }));
      if (r.effects_stage < r.effects_total) {
        decided.append(el('p', { className: 'muted', textContent: `Шаги решения: ${r.effects_stage} из ${r.effects_total}. ${r.effects_error ? 'Ошибка: ' + r.effects_error : 'Выполняются.'}` }));
        if (r.effects_attempts >= 10) decided.append(el('div', { className: 'actions' }, btn('Повторить шаги', '', (b) => action(b, () => post(`/api/reports/${id}/retry`, {}), () => { toast('Запущено'); rerender(); })())));
      }
      blocks.push(decided);
    }
    if (msgs.length) blocks.push(el('div', { className: 'card' }, el('h2', { textContent: 'Снимок сообщений' }), ...msgs));
    if (r.status === 0) {
      const reason = el('input', { placeholder: 'Причина (видна в журнале)', maxLength: 512 });
      const picker = durationPicker();
      const decide = (act) => action(null, async () => {
        const body = { action: act, reason: reason.value, until: (act === 'restrict' || act === 'ban') ? picker.until() : 0 };
        const path = `/api/reports/${id}/decide`;
        if (act === 'dismiss') await post(path, { payload: JSON.stringify(body) });
        else await signed(path, body);
        location.hash = 'reports';
      }, 'Решено')();
      blocks.push(el('div', { className: 'card' }, el('h2', { textContent: 'Решение' }),
        el('label', { className: 'field' }, el('span', { textContent: 'Причина' }), reason),
        el('div', { className: 'field' }, el('span', { className: 'muted small', textContent: 'Срок (для ограничения и блокировки)' }), picker.node),
        el('div', { className: 'actions' },
          btn('Отклонить', '', () => decide('dismiss')),
          btn('Удалить сообщение', 'danger', () => decide('delete')),
          btn('Ограничить', 'danger', () => decide('restrict')),
          btn('Заблокировать', 'danger', () => decide('ban')))));
    }
    const others = (history || []).filter((h) => h.id !== r.id);
    if (others.length) blocks.push(el('div', { className: 'card' }, el('h2', { textContent: 'Другие жалобы на этого пользователя' }), el('div', { className: 'list' }, ...others.map(reportRow))));
    return blocks;
  });
}

// Registration.
async function registrationPage() {
  await page('Регистрация', async () => {
    const { open, phones } = await api('/api/registration');
    const mode = el('div', { className: 'card' }, el('h2', { textContent: 'Режим' }),
      el('div', { className: 'segmented' },
        modeButton('По списку', false, open), modeButton('Открыта для всех', true, open)),
      el('p', { className: 'muted', textContent: open
        ? 'Войти и зарегистрироваться может любой номер. Коды показываются владельцу здесь и на карточке пользователя, пока не подключена доставка.'
        : 'Войти и зарегистрироваться могут только номера из списка ниже.' }));
    const phone = el('input', { placeholder: '+79991234567', inputMode: 'tel', autocomplete: 'off' });
    const note = el('input', { placeholder: 'Заметка (по желанию)', maxLength: 128 });
    const add = btn('Добавить', 'primary', (b) => action(b, async () => {
      const r = await post('/api/registration/phones', { payload: JSON.stringify({ phone: phone.value, note: note.value }) });
      toast(r.added ? 'Добавлено' : 'Уже в списке');
      rerender();
    })());
    const list = el('div', { className: 'card' }, el('h2', { textContent: `Список номеров · ${phones?.length || 0}` }),
      el('div', { className: 'row' }, el('div', { className: 'grow' }, phone), el('div', { className: 'grow' }, note), add),
      phones?.length ? el('div', { className: 'list' }, ...phones.map((p) => el('div', { className: 'item' },
        el('div', { className: 'grow' }, el('div', { className: 'title mono', textContent: phoneShown(p.Phone) }), el('div', { className: 'sub', textContent: [p.Note, dateOf(p.CreatedAt)].filter(Boolean).join(' · ') })),
        el('div', { className: 'row' },
          btn('Код', 'small', (b) => action(b, async () => {
            const out = await requestCode((reqId) => signed('/api/registration/code', { phone: p.Phone, request_id: reqId || '' }));
            if (out) showCode(out, phoneShown(p.Phone));
          })()),
          btn('Убрать', 'small danger', (b) => action(b, async () => {
            if (!await confirmDialog({ title: 'Убрать номер?', text: `${phoneShown(p.Phone)}: сеансы останутся, новый вход будет закрыт.`, confirmLabel: 'Убрать', danger: true })) return;
            await signed('/api/registration/phones/remove', { phone: p.Phone });
            toast('Убрано'); rerender();
          })()))))) : el('p', { className: 'muted', textContent: 'Список пуст.' }));
    return [mode, list];
  });
}
function modeButton(label, value, current) {
  const b = el('button', { type: 'button', textContent: label, ariaPressed: String(value === current) });
  b.onclick = () => {
    if (value === current) return;
    action(b, async () => {
      if (!await confirmDialog({ title: value ? 'Открыть регистрацию для всех?' : 'Вернуть регистрацию по списку?', text: value ? 'Кто угодно с номером телефона сможет зарегистрироваться.' : 'Войти смогут только номера из списка.', confirmLabel: 'Переключить' })) return;
      await signed('/api/registration/mode', { open: value });
      toast('Переключено'); rerender();
    })();
  };
  return b;
}

// Broadcast.
async function broadcastPage() {
  await page('Рассылка', async () => {
    const { broadcasts } = await api('/api/broadcasts');
    const text = el('textarea', { placeholder: 'Текст сообщения от Pluma', maxLength: 4096, rows: 5 });
    const audience = el('select', {},
      el('option', { value: 'all', textContent: 'Все пользователи' }),
      el('option', { value: 'ids', textContent: 'Список id' }),
      el('option', { value: 'channel', textContent: 'Участники канала' }));
    const idsField = el('label', { className: 'field', hidden: true }, el('span', { textContent: 'Id через запятую или пробел' }), el('input', { placeholder: '1000001, 1000002', autocomplete: 'off', inputMode: 'numeric' }));
    const channelField = el('label', { className: 'field', hidden: true }, el('span', { textContent: 'Id канала' }), el('input', { type: 'number', min: 1, autocomplete: 'off' }));
    audience.onchange = () => { idsField.hidden = audience.value !== 'ids'; channelField.hidden = audience.value !== 'channel'; };
    const payload = () => ({
      text: text.value, audience: audience.value,
      audience_id: Number(channelField.querySelector('input').value || 0), ids: idsField.querySelector('input').value,
    });
    const start = btn('Запустить рассылку', 'primary', (b) => action(b, async () => {
      if (!text.value.trim()) throw new Error('Введите текст');
      const { count } = await post('/api/broadcasts/preview', { payload: JSON.stringify(payload()) });
      if (!await confirmDialog({ title: `Отправить ${count} ${plural(count, 'пользователю', 'пользователям', 'пользователям')}?`, text: 'Сообщения придут от служебного аккаунта Pluma.', confirmLabel: 'Отправить', danger: true })) return;
      await signed('/api/broadcasts', payload());
      toast('Рассылка запущена'); rerender();
    })());
    const form = el('div', { className: 'card' }, el('h2', { textContent: 'Новая рассылка' }),
      el('label', { className: 'field' }, el('span', { textContent: 'Кому' }), audience), idsField, channelField,
      el('label', { className: 'field' }, el('span', { textContent: 'Текст' }), text), el('div', { className: 'actions' }, start));
    return [form, el('div', { className: 'card' }, el('h2', { textContent: 'История' }), broadcasts?.length ? el('div', { className: 'list' }, ...broadcasts.map(broadcastRow)) : empty('Рассылок пока не было.'))];
  }, 2);
}
const broadcastAudience = { all: 'всем', ids: 'по списку id', channel: 'участникам канала' };
function broadcastRow(b) {
  const pct = b.total ? Math.min(100, Math.round((b.sent + b.failed) / b.total * 100)) : 100;
  const badge = b.state === 0 ? el('span', { className: 'badge ok', textContent: 'идёт' }) : b.state === 2 ? el('span', { className: 'badge', textContent: 'остановлена' }) : el('span', { className: 'badge', textContent: 'готово' });
  const row = el('div', { className: 'item' }, el('div', { className: 'grow stack' },
    el('div', { className: 'title break', textContent: b.text }),
    el('div', { className: 'sub', textContent: `${broadcastAudience[b.audience] || b.audience} · ${dateOf(b.created_at)} · отправлено ${b.sent} из ${b.total}${b.failed ? ', ошибок ' + b.failed : ''}` }),
    el('div', { className: 'bar' }, widthBar(pct)),
    b.last_error ? el('div', { className: 'sub', textContent: 'Последняя ошибка: ' + b.last_error }) : null), badge,
  b.state === 0 ? btn('Остановить', 'small danger', (x) => action(x, () => signed(`/api/broadcasts/${b.id}/cancel`, {}), () => { toast('Остановлено'); rerender(); })()) : null);
  return row;
}
function widthBar(pct) {
  const s = el('span');
  s.style.width = pct + '%';
  return s;
}

// Releases.
const platformLabels = { android: 'Android', ios: 'iOS', macos: 'macOS', windows: 'Windows', linux: 'Linux' };
async function releasesPage() {
  await page('Выпуски', async () => {
    const out = await api('/api/releases');
    if (!out.configured) return errorState('Сервис выпусков не настроен: нет RELEASES_ADMIN_TOKEN (docs/ops/releases.md).');
    return [el('p', { className: 'muted small', textContent: 'Новые сборки приходят из CI. Здесь — откат выпуска и его возврат: текущим становится последняя неотозванная сборка.' }),
      ...out.platforms.map((p) => {
        const current = p.builds.find((b) => b.build === p.current);
        return el('div', { className: 'card' },
          el('h2', {}, el('span', { textContent: platformLabels[p.platform] || p.platform }),
            el('span', { className: 'badge' + (current ? ' ok' : ''), textContent: current ? `текущая ${current.version} (${current.build})` : 'нет выпуска' })),
          p.builds.length ? el('div', { className: 'list' }, ...p.builds.map((b) => releaseRow(p, b))) : empty('Сборок нет.'));
      })];
  });
}
function releaseRow(p, b) {
  const label = `${b.version} (${b.build})`;
  const rollback = btn('Откатить', 'small danger', (x) => action(x, async () => {
    if (!await confirmDialog({ title: `Откатить ${platformLabels[p.platform]} ${label}?`, text: 'Сайт и приложения вернутся к предыдущей сборке. Уже поставленное обновление не откатывается.', confirmLabel: 'Откатить', danger: true })) return;
    await signed(`/api/releases/${p.platform}/${b.build}/withdraw`, {});
    toast('Откатили'); rerender();
  })());
  const restore = btn('Вернуть', 'small', (x) => action(x, async () => {
    await signed(`/api/releases/${p.platform}/${b.build}/restore`, {});
    toast('Выпуск возвращён'); rerender();
  })());
  const notes = b.notes_ru || b.notes_en;
  return el('div', { className: 'item' },
    el('div', { className: 'grow' }, el('div', { className: 'title', textContent: label }),
      el('div', { className: 'sub', textContent: [dateOf(b.created_at), notes].filter(Boolean).join(' · ') })),
    b.withdrawn ? el('span', { className: 'badge warn', textContent: 'отозвана' }) : b.build === p.current ? el('span', { className: 'badge ok', textContent: 'текущая' }) : null,
    b.withdrawn ? restore : b.build === p.current ? rollback : null);
}

// System.
async function systemPage() {
  await page('Система', async () => {
    const s = await api('/api/system');
    const services = s.services || [];
    const down = services.filter((x) => !x.up);
    const groups = el('div', { className: 'card' }, el('h2', {}, el('span', { textContent: 'Сервисы' }),
      s.services_error ? null : el('span', { className: 'badge ' + (down.length ? 'danger' : 'ok'), textContent: down.length ? `не работает ${down.length}` : 'все работают' })),
    s.services_error ? el('p', { className: 'muted', textContent: 'Prometheus не отвечает.' })
      : services.length ? el('div', { className: 'list' }, ...services.map((x) => el('div', { className: 'item' },
        el('div', { className: 'grow' }, el('div', { className: 'title', textContent: x.job }), el('div', { className: 'sub mono', textContent: x.instance })),
        el('span', { className: 'badge ' + (x.up ? 'ok' : 'danger'), textContent: x.up ? 'работает' : 'не работает' })))) : empty('Нет данных.'));
    const alerts = el('div', { className: 'card' }, el('h2', { textContent: 'Алерты' }),
      s.alerts_error ? el('p', { className: 'muted', textContent: 'Prometheus не отвечает.' })
        : s.alerts?.length ? el('div', { className: 'list' }, ...s.alerts.map(alertRow)) : el('p', { className: 'muted', textContent: 'Активных алертов нет.' }));
    const events = el('div', { className: 'card' }, el('h2', { textContent: 'Зависшие события' }),
      s.events_error ? el('p', { className: 'muted', textContent: 'Не удалось прочитать события.' })
        : s.events?.length ? el('div', { className: 'list' }, ...s.events.map(eventRow)) : el('p', { className: 'muted', textContent: 'Зависших событий нет.' }));
    const links = s.grafana ? el('div', { className: 'card' }, el('h2', { textContent: 'Графики' }),
      el('a', { href: s.grafana, textContent: 'Открыть Grafana' }), el('span', { className: 'muted small', textContent: 'Логи: Grafana → Explore → Loki, {service="admin"}.' })) : null;
    return [groups, alerts, events, links];
  });
}
const eventStates = { 2: 'зависло', 3: 'отложено' };
function eventRow(e) {
  return el('div', { className: 'item' },
    el('div', { className: 'grow' },
      el('div', { className: 'title', textContent: e.method || `событие #${e.id}` }),
      el('div', { className: 'sub', textContent: [`#${e.id}`, e.topic, `получатель ${e.recipient}`, `попыток ${e.attempts}`, dateOf(e.created_at)].join(' · ') }),
      e.error ? el('div', { className: 'sub break', textContent: e.error }) : null),
    el('div', { className: 'stack' }, el('span', { className: 'badge warn', textContent: eventStates[e.state] || `состояние ${e.state}` }),
      el('div', { className: 'row' },
        btn('Повторить', 'small', (b) => action(b, async () => {
          if (!await confirmDialog({ title: 'Повторить события получателя?', text: `Все зависшие события получателя ${e.recipient} снова пойдут в обработку.`, confirmLabel: 'Повторить' })) return;
          const r = await signed('/api/system/outbox/replay', { recipient: e.recipient });
          toast(`Повторено: ${r.replayed}`); rerender();
        })()),
        btn('Отбросить', 'small danger', (b) => action(b, async () => {
          if (!await confirmDialog({ title: 'Отбросить событие?', text: 'Оно никогда не будет доставлено.', confirmLabel: 'Отбросить', danger: true })) return;
          await signed('/api/system/outbox/discard', { id: e.id });
          toast('Отброшено'); rerender();
        })()))));
}

// Journal.
const auditLabels = {
  login: 'вход', passkey_register: 'ключ добавлен', passkey_reset: 'сброс ключей', passkey_remove: 'ключ удалён',
  admin_set: 'доступ изменён', view_report: 'просмотр жалобы', view_media: 'просмотр медиа', view_user: 'просмотр пользователя', view_peer: 'просмотр канала',
  decide: 'решение по жалобе', retry_effects: 'повтор шагов', lift: 'снято ограничение', restrict_direct: 'ограничен', ban_direct: 'заблокирован',
  terminate_sessions: 'завершены сеансы', message_user: 'сообщение пользователю', login_code: 'показан код входа', delete_user: 'аккаунт удалён',
  block_peer: 'заблокировано', unblock_peer: 'разблокировано', delete_peer: 'удалено', registration_mode: 'режим регистрации',
  allowlist_add: 'номер добавлен', allowlist_remove: 'номер убран', broadcast_start: 'рассылка', broadcast_cancel: 'рассылка остановлена',
  release_withdraw: 'выпуск откачен', release_restore: 'выпуск возвращён', outbox_replay: 'повтор событий', outbox_discard: 'событие отброшено',
};
function auditRow(e) {
  return el('div', { className: 'item' }, el('div', { className: 'grow' },
    el('div', { className: 'title', textContent: auditLabels[e.action] || e.action }),
    el('div', { className: 'sub', textContent: [dateOf(e.at), e.actor, e.target].filter(Boolean).join(' · ') }),
    e.detail ? el('div', { className: 'sub break', textContent: e.detail }) : null));
}
function auditPage() {
  titleEl.textContent = 'Журнал';
  document.title = 'Журнал · Pluma';
  const action2 = el('select', {}, el('option', { value: '', textContent: 'Все действия' }), ...Object.entries(auditLabels).map(([k, v]) => el('option', { value: k, textContent: v })));
  const target = el('input', { type: 'search', placeholder: 'Объект, например user:123', autocomplete: 'off' });
  const list = el('div', { className: 'list' });
  const more = btn('Показать ещё', '', () => load(false));
  more.hidden = true;
  let before = 0;
  const load = async (reset) => {
    if (reset) { before = 0; list.replaceChildren(skeleton(4)); more.hidden = true; }
    try {
      const q = new URLSearchParams({ before: String(before), action: action2.value, target: target.value.trim() });
      const { entries } = await api('/api/audit?' + q);
      if (reset) list.replaceChildren();
      if (entries?.length) {
        list.append(...entries.map(auditRow));
        before = entries[entries.length - 1].id;
        more.hidden = entries.length < 100;
      } else if (reset) {
        list.replaceChildren(empty('Записей нет.'));
      }
    } catch (e) {
      if (reset) list.replaceChildren(errorState(e.message, () => load(true))); else toast(e.message, true);
    }
  };
  action2.onchange = () => load(true);
  let t = null;
  target.oninput = () => { clearTimeout(t); t = setTimeout(() => load(true), 300); };
  show(el('div', { className: 'card' }, el('div', { className: 'row' }, el('div', { className: 'grow' }, action2), el('div', { className: 'grow' }, target)), list, more));
  load(true);
}

// Access.
async function accessPage() {
  await page('Доступ', async () => {
    const [{ admins }, { passkeys }] = await Promise.all([api('/api/admins'), api('/api/passkeys')]);
    const keys = el('div', { className: 'card' }, el('h2', {}, el('span', { textContent: `Ключи доступа · ${passkeys?.length || 0}` }),
      btn('Добавить ключ', 'small', (b) => action(b, async () => { await register(); toast('Ключ добавлен'); rerender(); })())),
    el('div', { className: 'list' }, ...(passkeys || []).map((p) => el('div', { className: 'item' },
      el('div', { className: 'grow' }, el('div', { className: 'title', textContent: 'Ключ' }), el('div', { className: 'sub', textContent: `добавлен ${dateOf(p.created_at)} · ${p.id.slice(0, 8)}…` })),
      passkeys.length > 1 ? btn('Удалить', 'small danger', (b) => action(b, async () => {
        if (!await confirmDialog({ title: 'Удалить ключ доступа?', text: 'С этого устройства войти будет нельзя.', confirmLabel: 'Удалить', danger: true })) return;
        await signed('/api/passkeys/remove', { id: p.id });
        toast('Удалён'); rerender();
      })()) : null))));
    const logins = el('div', { className: 'card' }, el('h2', { textContent: 'Вход в панель' }),
      el('p', { className: 'muted small', textContent: 'Панель открывается только для учётных записей tailnet из этого списка. Разделения прав нет: кто вошёл, тот и владелец.' }),
      el('div', { className: 'list' }, ...(admins || []).map((a) => el('div', { className: 'item' },
        el('div', { className: 'grow break title', textContent: a.login }),
        a.disabled ? el('span', { className: 'badge', textContent: 'отключён' }) : el('span', { className: 'badge ok', textContent: 'активен' }),
        a.login !== me.login ? btn(a.disabled ? 'Включить' : 'Отключить', 'small' + (a.disabled ? '' : ' danger'), (b) => action(b, () => signed('/api/admins', { login: a.login, disabled: !a.disabled }), () => rerender())()) : null))),
      adminAddForm());
    return [keys, logins];
  }, 2);
}
function adminAddForm() {
  const login2 = el('input', { placeholder: 'login@tailnet', autocomplete: 'off' });
  return el('div', { className: 'row' }, el('div', { className: 'grow' }, login2),
    btn('Добавить', 'primary', (b) => action(b, async () => {
      if (!login2.value.trim()) return;
      await signed('/api/admins', { login: login2.value.trim(), disabled: false });
      toast('Добавлено'); rerender();
    })()));
}

// The session.
let me = null;
async function enter() {
  $('nav').hidden = true;
  $('foot').hidden = true;
  titleEl.textContent = 'Панель';
  try {
    me = await api('/api/me');
  } catch (err) {
    show(errorState(err.status === 403 ? 'Этой учётной записи нет в панели.' : 'Нет доступа: ' + err.message, enter));
    return;
  }
  $('who').textContent = me.login;
  if (!me.has_passkey) {
    show(el('div', { className: 'welcome' }, el('h2', { textContent: 'Первый вход' }),
      el('p', { className: 'muted', textContent: 'Зарегистрируйте ключ доступа: Touch ID, Face ID или аппаратный ключ.' }),
      btn('Зарегистрировать ключ', 'primary', (b) => action(b, async () => { await register(); await enter(); })())));
    return;
  }
  if (!me.logged_in) {
    show(el('div', { className: 'welcome' }, el('h2', { textContent: 'Вход в панель Pluma' }),
      el('p', { className: 'muted', textContent: me.login }),
      btn('Войти с ключом доступа', 'primary', (b) => action(b, async () => { await login(); await enter(); })())));
    return;
  }
  buildNav();
  $('nav').hidden = false;
  $('foot').hidden = false;
  await route();
}

// The menu on a phone.
function openMenu() { $('rail').classList.add('open'); $('backdrop').hidden = false; $('menu').ariaExpanded = 'true'; }
function closeMenu() { $('rail').classList.remove('open'); $('backdrop').hidden = true; $('menu').ariaExpanded = 'false'; }
$('menu').onclick = () => ($('rail').classList.contains('open') ? closeMenu() : openMenu());
$('backdrop').onclick = closeMenu;
$('logout').onclick = action($('logout'), async () => { await post('/api/logout', {}); location.hash = ''; await enter(); });
window.addEventListener('hashchange', () => { if (me && me.logged_in) route(); });
enter();
