'use strict';
// The moderation panel (task 025, ADR 0040). No build step, no dependencies:
// the Content-Security-Policy allows only this file.

const main = document.getElementById('main');
let me = null;

const b64 = (buf) => btoa(String.fromCharCode(...new Uint8Array(buf))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
const unb64 = (s) => Uint8Array.from(atob(s.replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0));
const el = (tag, props = {}, ...kids) => {
  const e = Object.assign(document.createElement(tag), props);
  e.append(...kids);
  return e;
};
const dateOf = (t) => (t ? new Date(t * 1000).toLocaleString('ru-RU') : '—');

async function api(path, opts = {}) {
  const r = await fetch(path, { ...opts, headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) } });
  if (r.status === 401) { await enter(); throw new Error('Войдите с ключом доступа'); }
  if (!r.ok) throw new Error((await r.text()) || r.statusText);
  return r.status === 204 ? null : r.json();
}

const post = (path, body) => api(path, { method: 'POST', body: JSON.stringify(body) });

function creationOptions(o) {
  const c = structuredClone(o.publicKey);
  c.challenge = unb64(c.challenge);
  c.user.id = unb64(c.user.id);
  c.excludeCredentials = (c.excludeCredentials || []).map((d) => ({ ...d, id: unb64(d.id) }));
  return c;
}

function requestOptions(o) {
  const c = structuredClone(o.publicKey);
  const challengeBytes = hexToBytes(o.challengeHex);
  c.challenge = challengeBytes;
  c.allowCredentials = (c.allowCredentials || []).map((d) => ({ ...d, id: unb64(d.id) }));
  return c;
}

function hexToBytes(hex) {
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < hex.length; i += 2) out[i / 2] = parseInt(hex.slice(i, i + 2), 16);
  return out;
}

function assertionBody(cred, challengeHex) {
  return {
    credentialId: b64(cred.rawId),
    clientDataJSON: b64(cred.response.clientDataJSON),
    authenticatorData: b64(cred.response.authenticatorData),
    signature: b64(cred.response.signature),
    challenge: challengeHex,
  };
}

async function getAssertion(begin) {
  const options = await begin();
  const cred = await navigator.credentials.get({ publicKey: requestOptions(options) });
  return assertionBody(cred, options.challengeHex);
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

async function login() {
  const assertion = await getAssertion(() => post('/api/login/begin', {}));
  await post('/api/login/finish', assertion);
}

function show(...nodes) { main.replaceChildren(...nodes); }

async function enter() {
  document.getElementById('nav').hidden = true;
  try {
    me = await api('/api/me');
  } catch (err) {
    show(el('p', { className: 'error', textContent: 'Нет доступа: ' + err.message }));
    return;
  }
  document.getElementById('who').textContent = me.login;
  if (!me.has_passkey) {
    show(el('p', {}, 'Зарегистрируйте ключ доступа (Touch ID / Face ID).'),
      el('button', { textContent: 'Зарегистрировать', onclick: () => run(async () => { await register(); await enter(); }) }));
    return;
  }
  if (!me.logged_in) {
    show(el('button', { textContent: 'Войти с ключом', onclick: () => run(async () => { await login(); await enter(); }) }));
    return;
  }
  document.getElementById('nav').hidden = false;
  document.querySelectorAll('[data-owner]').forEach((a) => { a.hidden = me.role < 2; });
  await route();
}

async function run(fn) {
  try { await fn(); } catch (err) { show(el('p', { className: 'error', textContent: err.message }), el('button', { textContent: 'Назад', onclick: route })); }
}

const kinds = { 1: 'сообщение', 2: 'профиль/чат', 3: 'фото профиля', 4: 'спам', 5: 'спам в канале' };
const actions = { dismiss: 'Отклонить', delete: 'Удалить сообщение', restrict: 'Ограничить', ban: 'Заблокировать' };

async function route() {
  const hash = location.hash.slice(1) || 'queue';
  if (hash.startsWith('report/')) return run(() => reportPage(hash.split('/')[1]));
  if (hash === 'queue' || hash === 'decided') return run(() => queuePage(hash));
  if (hash === 'audit') return run(auditPage);
  if (hash === 'premium') return run(premiumPage);
  if (hash === 'admins') return run(adminsPage);
  show(el('p', { textContent: 'Нет такой страницы' }));
}

async function queuePage(kind) {
  const { reports } = await api('/api/reports' + (kind === 'decided' ? '?status=decided' : ''));
  const rows = (reports || []).map((r) => {
    const tr = el('tr', {}, el('td', { textContent: r.id }), el('td', { textContent: kinds[r.kind] || r.kind }),
      el('td', { textContent: r.reason }), el('td', { textContent: r.target_user_id || '—' }), el('td', { textContent: dateOf(r.created_at) }),
      el('td', { textContent: r.decision ? (actions[r.decision] || r.decision) : '—' }));
    tr.dataset.id = r.id;
    tr.onclick = () => { location.hash = 'report/' + r.id; };
    return tr;
  });
  show(el('table', {}, el('thead', {}, el('tr', {}, ...['#', 'Тип', 'Причина', 'На кого', 'Когда', 'Решение'].map((t) => el('th', { textContent: t })))), el('tbody', {}, ...rows)),
    rows.length ? '' : el('p', { className: 'muted', textContent: 'Пусто' }));
}

function untilPicker() {
  const select = el('select', {},
    el('option', { value: '86400', textContent: 'Сутки' }), el('option', { value: '604800', textContent: 'Неделя' }),
    el('option', { value: '2592000', textContent: 'Месяц' }), el('option', { value: '0', textContent: 'Навсегда' }));
  return { node: select, until: () => { const v = Number(select.value); return v ? Math.floor(Date.now() / 1000) + v : 0; } };
}

async function reportPage(id) {
  const { report, snapshot, restriction, history } = await api('/api/reports/' + id);
  const user = (uid) => snapshot.users?.find((u) => u.id === uid);
  const nameOf = (uid) => { const u = user(uid); return u ? `${u.first_name} ${u.last_name || ''} ${u.username ? '@' + u.username : ''} (${u.id})` : uid; };
  const msgs = (snapshot.messages || []).map((m) => {
    const node = el('div', { className: 'msg' }, el('div', { className: 'muted', textContent: `${nameOf(m.from_id)} · ${dateOf(m.date)}` }), m.text || '');
    if (m.media) {
      const url = `/api/reports/${id}/media/${m.id}`;
      if (m.media.mime && m.media.mime.startsWith('video/')) node.append(el('video', { src: url, controls: true }));
      else if (m.media.photo_id || (m.media.mime && m.media.mime.startsWith('image/'))) node.append(el('img', { src: url, alt: '' }));
      else node.append(el('div', { className: 'muted', textContent: m.media.kind }));
    }
    return node;
  });
  const reason = el('input', { placeholder: 'Причина (видна в журнале)' });
  const picker = untilPicker();
  const decide = async (action) => {
    const body = { action, reason: reason.value, until: (action === 'restrict' || action === 'ban') ? picker.until() : 0 };
    const path = `/api/reports/${id}/decide`;
    if (action === 'dismiss') await post(path, { payload: JSON.stringify(body) });
    else await signed(path, body);
    location.hash = 'queue';
  };
  const buttons = report.status === 0 ? el('div', { className: 'actions' },
    reason, picker.node,
    ...Object.entries(actions).map(([k, v]) => el('button', { textContent: v, className: k === 'dismiss' ? '' : 'danger', onclick: () => run(() => decide(k)) }))) : '';
  const blocks = [
    el('h2', { textContent: `Жалоба #${report.id}` }),
    el('p', {}, el('span', { className: 'muted', textContent: 'Причина: ' }), report.reason, ' · ', el('span', { className: 'muted', textContent: 'От: ' }), String(report.reporter_user_id), ' · ', dateOf(report.created_at)),
  ];
  if (report.comment) blocks.push(el('p', {}, el('span', { className: 'muted', textContent: 'Комментарий: ' }), report.comment));
  if (snapshot.peer_title) blocks.push(el('p', {}, el('span', { className: 'muted', textContent: 'Чат: ' }), snapshot.peer_title));
  if (report.target_user_id) blocks.push(el('p', {}, el('span', { className: 'muted', textContent: 'На кого: ' }), String(nameOf(report.target_user_id))));
  if (restriction) {
    blocks.push(el('p', { className: 'error', textContent: `Уже действует: ${restriction.kind === 2 ? 'блокировка' : 'ограничение'} до ${restriction.until_date ? dateOf(restriction.until_date) : 'бессрочно'}` }),
      el('button', { textContent: 'Снять', onclick: () => run(async () => { await signed(`/api/users/${report.target_user_id}/lift`, { reason: 'lifted from the panel' }); route(); }) }));
  }
  if (report.status !== 0) {
    blocks.push(el('p', {}, el('span', { className: 'muted', textContent: 'Решение: ' }), actions[report.decision] || report.decision, ' · ', report.decided_by, ' · ', dateOf(report.decided_at), report.decision_reason ? ` · ${report.decision_reason}` : ''));
    if (report.effects_stage < report.effects_total) {
      blocks.push(el('p', { className: 'error', textContent: `Шаги решения: ${report.effects_stage}/${report.effects_total}. ${report.effects_error || 'Выполняются.'}` }));
      if (report.effects_attempts >= 10) blocks.push(el('button', { textContent: 'Повторить', onclick: () => run(async () => { await post(`/api/reports/${id}/retry`, {}); route(); }) }));
    }
  }
  if (msgs.length) blocks.push(el('div', { className: 'card' }, ...msgs));
  if (snapshot.photo_id) blocks.push(el('p', { className: 'muted', textContent: `Фото профиля ${snapshot.photo_id}` }));
  blocks.push(buttons);
  const others = (history || []).filter((h) => h.id !== report.id);
  if (others.length) blocks.push(el('h3', { textContent: 'Другие жалобы на этого пользователя' }), el('p', {}, ...others.map((h) => el('a', { href: `#report/${h.id}`, textContent: `#${h.id} (${h.reason}) ` }))));
  show(...blocks);
}

async function auditPage() {
  const { entries } = await api('/api/audit');
  show(el('table', {}, el('tbody', {}, ...(entries || []).map((e) => el('tr', {}, el('td', { textContent: dateOf(e.at) }), el('td', { textContent: e.actor }), el('td', { textContent: e.action }), el('td', { textContent: e.target }), el('td', { textContent: e.detail }))))));
}

async function premiumPage() {
  const { accounts } = await api('/api/premium');
  const phone = el('input', { placeholder: '+7...', required: true });
  const months = el('input', { type: 'number', min: 0, placeholder: 'Месяцев (0 — бессрочно)' });
  const form = (grant) => () => run(async () => {
    await signed('/api/premium', { phone: phone.value, months: Number(months.value || 0), grant });
    route();
  });
  show(el('form', { className: 'row', onsubmit: (e) => e.preventDefault() }, phone, months,
    el('button', { type: 'button', textContent: 'Выдать', onclick: form(true) }), el('button', { type: 'button', className: 'danger', textContent: 'Снять', onclick: form(false) })),
    el('table', {}, el('tbody', {}, ...(accounts || []).map((a) => el('tr', {}, el('td', { textContent: a.phone }), el('td', { textContent: a.name }), el('td', { textContent: a.until ? 'до ' + dateOf(a.until) : 'бессрочно' }))))));
}

async function adminsPage() {
  const { admins } = await api('/api/admins');
  const login = el('input', { placeholder: 'login@tailnet', required: true });
  const role = el('select', {}, el('option', { value: '0', textContent: 'Наблюдатель' }), el('option', { value: '1', textContent: 'Модератор' }), el('option', { value: '2', textContent: 'Владелец' }));
  const save = (disabled) => run(async () => { await signed('/api/admins', { login: login.value, role: Number(role.value), disabled }); route(); });
  const roles = ['Наблюдатель', 'Модератор', 'Владелец'];
  show(el('form', { className: 'row', onsubmit: (e) => e.preventDefault() }, login, role,
    el('button', { type: 'button', textContent: 'Сохранить', onclick: () => save(false) }), el('button', { type: 'button', className: 'danger', textContent: 'Отключить', onclick: () => save(true) })),
    el('table', {}, el('tbody', {}, ...(admins || []).map((a) => el('tr', {}, el('td', { textContent: a.login }), el('td', { textContent: roles[a.role] }), el('td', { textContent: a.disabled ? 'отключён' : '' }))))));
}

window.addEventListener('hashchange', route);
enter();
