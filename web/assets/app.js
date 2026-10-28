'use strict';

/* mbtorrent UI — ванильный JS, без сборщиков и зависимостей. */

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

const ICONS = {
  pause: '<svg viewBox="0 0 24 24"><path d="M9 6v12M15 6v12"/></svg>',
  play: '<svg viewBox="0 0 24 24" fill="currentColor" stroke="none"><path d="M8 5.5v13l11-6.5z"/></svg>',
  refresh: '<svg viewBox="0 0 24 24"><path d="M20 11a8 8 0 1 0-.7 4.3"/><path d="M20 5v6h-6"/></svg>',
  folder: '<svg viewBox="0 0 24 24"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/></svg>',
  trash: '<svg viewBox="0 0 24 24"><path d="M4 7h16M9 7V5h6v2M7 7l1 12h8l1-12"/></svg>',
  down: '<svg viewBox="0 0 24 24"><path d="M12 5v11m0 0 4-4m-4 4-4-4"/></svg>',
  up: '<svg viewBox="0 0 24 24"><path d="M12 19V8m0 0 4 4m-4-4-4 4"/></svg>',
  x: '<svg viewBox="0 0 24 24"><path d="m6 6 12 12M18 6 6 18"/></svg>',
  check: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="8"/><path d="m8.5 12 2.5 2.5 4.5-5"/></svg>',
  alert: '<svg viewBox="0 0 24 24"><path d="M12 8v5m0 3h.01"/><circle cx="12" cy="12" r="8"/></svg>',
  magnet: '<svg viewBox="0 0 24 24"><path d="M7 5v7a5 5 0 0 0 10 0V5"/><path d="M5.2 5h3.6M15.2 5h3.6" stroke-width="3"/></svg>',
  plus: '<svg viewBox="0 0 24 24"><path d="M12 5v14M5 12h14"/></svg>',
};

const STATES = {
  metadata: { label: 'Метаданные' },
  downloading: { label: 'Загрузка' },
  stalled: { label: 'Ожидание' },
  seeding: { label: 'Раздача' },
  completed: { label: 'Готово' },
  paused: { label: 'Пауза' },
  checking: { label: 'Проверка' },
  error: { label: 'Ошибка' },
};

const TAB_TITLES = {
  all: 'Все раздачи',
  downloading: 'Загрузки',
  seeding: 'Раздачи',
  completed: 'Завершённые',
  paused: 'На паузе',
};

const app = {
  torrents: [],
  byHash: new Map(),
  global: {},
  config: {},
  version: '',
  tab: 'all',
  query: '',
  rows: new Map(),
  history: [],
  online: true,
  drawerHash: null,
  filesTimer: null,
  pollTimer: null,
};

/* ---------- API ---------- */

const API = {
  async req(path, { method = 'GET', body } = {}) {
    const headers = { 'X-MBTorrent': '1' };
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    const res = await fetch(path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || `Ошибка ${res.status}`);
    return data;
  },
  state: () => API.req('/api/state'),
  add: (payload) => API.req('/api/torrents', { method: 'POST', body: payload }),
  action: (hash, act, query = '') =>
    API.req(`/api/torrents/${hash}/${act}${query}`, { method: 'POST', body: {} }),
  files: (hash) => API.req(`/api/torrents/${hash}/files`),
  saveSettings: (cfg) => API.req('/api/settings', { method: 'PUT', body: cfg }),
  quit: () => API.req('/api/quit', { method: 'POST', body: {} }),
};

/* ---------- форматирование ---------- */

const nf = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 1 });
const UNITS = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ', 'ПБ'];

function bytes(n) {
  if (!n || n < 1) return '0\u00A0Б';
  let v = n;
  let i = 0;
  while (v >= 1024 && i < UNITS.length - 1) {
    v /= 1024;
    i++;
  }
  const text = i === 0 ? String(Math.round(v)) : nf.format(v);
  return `${text}\u00A0${UNITS[i]}`;
}

function speed(n) {
  return `${bytes(n)}/с`;
}

function pct(p) {
  return `${Math.round((p || 0) * 100)}%`;
}

function eta(seconds, state) {
  if (state === 'completed' || state === 'seeding') return 'готово';
  if (seconds < 0) return '—';
  if (seconds === 0) return 'готово';
  if (seconds < 60) return `~${Math.max(1, Math.round(seconds))} с`;
  if (seconds < 3600) return `~${Math.round(seconds / 60)} мин`;
  const h = Math.floor(seconds / 3600);
  const m = Math.round((seconds % 3600) / 60);
  return m ? `~${h} ч ${m} мин` : `~${h} ч`;
}

function plural(n, forms) {
  const a = Math.abs(n) % 100;
  const b = a % 10;
  if (a > 10 && a < 20) return forms[2];
  if (b > 1 && b < 5) return forms[1];
  if (b === 1) return forms[0];
  return forms[2];
}

function when(iso) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleString('ru-RU', {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  });
}

/* ---------- тема ---------- */

function initTheme() {
  const saved = localStorage.getItem('mb-theme');
  if (saved === 'light' || saved === 'dark') {
    document.documentElement.dataset.theme = saved;
  }
  $('#btn-theme').addEventListener('click', () => {
    const next = document.documentElement.dataset.theme === 'light' ? 'dark' : 'light';
    document.documentElement.dataset.theme = next;
    localStorage.setItem('mb-theme', next);
  });
}

/* ---------- тосты ---------- */

function toast(message, kind = 'ok') {
  const el = document.createElement('div');
  el.className = `toast ${kind === 'err' ? 'err' : ''}`;
  el.innerHTML = kind === 'err' ? ICONS.alert : ICONS.check;
  const text = document.createElement('span');
  text.textContent = message;
  el.appendChild(text);
  $('#toasts').appendChild(el);
  setTimeout(() => {
    el.classList.add('out');
    setTimeout(() => el.remove(), 200);
  }, 3800);
}

/* ---------- модальные окна ---------- */

const modalStack = [];

function openModal(html, mount) {
  const overlay = document.createElement('div');
  overlay.className = 'overlay';
  overlay.innerHTML = `<div class="modal" role="dialog" aria-modal="true">${html}</div>`;
  $('#modal-root').appendChild(overlay);

  let closed = false;
  const close = () => {
    if (closed) return;
    closed = true;
    overlay.classList.remove('show');
    setTimeout(() => overlay.remove(), 200);
    const i = modalStack.indexOf(close);
    if (i >= 0) modalStack.splice(i, 1);
  };

  overlay.addEventListener('mousedown', (e) => {
    if (e.target === overlay) close();
  });
  $$('[data-close]', overlay).forEach((b) => b.addEventListener('click', close));

  modalStack.push(close);
  mount?.(overlay, close);
  requestAnimationFrame(() => overlay.classList.add('show'));
  return close;
}

/* ---------- строка списка ---------- */

function createRow() {
  const el = document.createElement('article');
  el.className = 'row';
  el.setAttribute('role', 'listitem');
  el.innerHTML = `
    <div class="ring" aria-hidden="true">
      <svg viewBox="0 0 44 44">
        <circle class="ring-bg" cx="22" cy="22" r="18"/>
        <circle class="ring-fg" cx="22" cy="22" r="18"/>
      </svg>
      <span class="ring-pct">0%</span>
    </div>
    <div class="row-body">
      <div class="row-head">
        <span class="row-name"></span>
        <span class="badge"></span>
      </div>
      <div class="row-sub">
        <span class="sub-size"></span>
        <span class="sep">·</span>
        <span class="sub-peers"></span>
        <span class="sep">·</span>
        <span class="sub-eta"></span>
      </div>
      <div class="track"><i class="bar"></i></div>
    </div>
    <div class="row-speeds">
      <span class="sp down">${ICONS.down}<span class="v"></span></span>
      <span class="sp up">${ICONS.up}<span class="v"></span></span>
    </div>
    <div class="row-actions">
      <button class="icon-btn" data-act="toggle" title="Пауза"></button>
      <button class="icon-btn" data-act="recheck" title="Проверить файлы">${ICONS.refresh}</button>
      <button class="icon-btn" data-act="reveal" title="Открыть папку">${ICONS.folder}</button>
      <button class="icon-btn danger" data-act="remove" title="Удалить">${ICONS.trash}</button>
    </div>`;
  return el;
}

function updateRow(el, t) {
  el.dataset.state = t.state;
  el.dataset.hash = t.hash;

  const name = $('.row-name', el);
  name.textContent = t.name;
  name.title = t.name;

  $('.badge', el).textContent = (STATES[t.state] || STATES.downloading).label;

  $('.sub-size', el).textContent = t.size > 0 ? bytes(t.size) : 'размер неизвестен';
  $('.sub-peers', el).textContent =
    `${t.seeds} ${plural(t.seeds, ['сид', 'сида', 'сидов'])} · ${t.peers} ${plural(t.peers, ['пир', 'пира', 'пиров'])}`;

  const etaEl = $('.sub-eta', el);
  etaEl.textContent = eta(t.eta, t.state);
  etaEl.classList.toggle('eta', t.state === 'downloading');

  const p = Math.min(1, Math.max(0, t.progress || 0));
  const C = 2 * Math.PI * 18;
  $('.ring-fg', el).style.strokeDashoffset = String(C * (1 - p));
  const done = p >= 1 || t.state === 'seeding' || t.state === 'completed';
  $('.ring-pct', el).textContent = done ? '✓' : pct(p);

  $('.track .bar', el).style.width = `${(p * 100).toFixed(2)}%`;

  const downEl = $('.down .v', el);
  const upEl = $('.up .v', el);
  downEl.textContent = speed(t.down_speed);
  upEl.textContent = speed(t.up_speed);
  $('.down', el).classList.toggle('zero', !t.down_speed);
  $('.up', el).classList.toggle('zero', !t.up_speed);

  const toggle = $('[data-act="toggle"]', el);
  const mode = t.state === 'paused' ? 'play' : 'pause';
  if (toggle.dataset.mode !== mode) {
    toggle.dataset.mode = mode;
    toggle.innerHTML = mode === 'play' ? ICONS.play : ICONS.pause;
    toggle.title = mode === 'play' ? 'Продолжить' : 'Пауза';
  }
}

/* ---------- рендер ---------- */

function matches(t) {
  if (app.query) {
    const q = app.query.toLowerCase();
    if (!t.name.toLowerCase().includes(q) && !t.hash.startsWith(q)) return false;
  }
  switch (app.tab) {
    case 'downloading':
      return ['downloading', 'stalled', 'metadata', 'checking'].includes(t.state);
    case 'seeding':
      return t.state === 'seeding';
    case 'completed':
      return t.state === 'completed';
    case 'paused':
      return t.state === 'paused';
    default:
      return true;
  }
}

function applyState(s) {
  app.torrents = Array.isArray(s.torrents) ? s.torrents : [];
  app.global = s.global || {};
  app.config = s.config || {};
  app.version = s.version || '';
  app.byHash = new Map(app.torrents.map((t) => [t.hash, t]));

  for (const hash of Array.from(app.rows.keys())) {
    if (!app.byHash.has(hash)) app.rows.delete(hash);
  }

  app.history.push(app.global.down_speed || 0);
  if (app.history.length > 60) app.history.shift();

  render();
}

function render() {
  renderMetrics();
  renderCounts();
  renderList();
  renderStatus();
  renderDrawer();
}

function renderMetrics() {
  const g = app.global;
  $('#m-down').textContent = speed(g.down_speed);
  $('#m-down-sub').textContent = `за сеанс ${bytes(g.session_down)}`;
  $('#m-up').textContent = speed(g.up_speed);
  $('#m-up-sub').textContent = `за сеанс ${bytes(g.session_up)}`;
  $('#m-count').textContent = String(g.torrents || 0);
  $('#m-count-sub').textContent = `активных ${g.active || 0}`;
  $('#m-free').textContent = g.free_space ? bytes(g.free_space) : '—';
  $('#m-free-sub').textContent = 'свободно на диске';

  drawSpark();
}

function drawSpark() {
  const h = app.history;
  const line = $('#spark-line');
  const area = $('#spark-area');
  if (h.length < 2) {
    line.setAttribute('points', '');
    area.setAttribute('points', '0,30 120,30');
    return;
  }
  const max = Math.max(...h, 0);
  const spark = $('.spark');
  spark.classList.toggle('idle', max === 0);
  if (max === 0) {
    line.setAttribute('points', '');
    area.setAttribute('points', '');
    return;
  }
  const w = 120;
  const ht = 30;
  const pts = h.map((v, i) => {
    const x = (i / (h.length - 1)) * w;
    const y = ht - 2 - (v / max) * (ht - 6);
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  });
  line.setAttribute('points', pts.join(' '));
  area.setAttribute('points', `0,${ht} ${pts.join(' ')} ${w},${ht}`);
}

function renderCounts() {
  const counts = { all: 0, downloading: 0, seeding: 0, completed: 0, paused: 0 };
  for (const t of app.torrents) {
    counts.all++;
    if (['downloading', 'stalled', 'metadata', 'checking'].includes(t.state)) counts.downloading++;
    else if (t.state === 'seeding') counts.seeding++;
    else if (t.state === 'completed') counts.completed++;
    else if (t.state === 'paused') counts.paused++;
  }
  for (const [key, value] of Object.entries(counts)) {
    $$(`[data-count="${key}"]`).forEach((el) => (el.textContent = String(value)));
  }
}

function renderList() {
  const visible = app.torrents.filter(matches);
  const list = $('#list');
  const current = Array.from(list.children);

  // Не переставляем DOM без необходимости: иначе каждый тик сбрасывается
  // фокус и hover у кнопок внутри строки.
  const same =
    current.length === visible.length &&
    current.every((el, i) => el.dataset.hash === visible[i].hash);

  if (same) {
    for (let i = 0; i < visible.length; i++) updateRow(current[i], visible[i]);
  } else {
    const frag = document.createDocumentFragment();
    for (const t of visible) {
      let el = app.rows.get(t.hash);
      if (!el) {
        el = createRow();
        app.rows.set(t.hash, el);
      }
      updateRow(el, t);
      frag.appendChild(el);
    }
    list.replaceChildren(frag);
  }

  const empty = $('#empty');
  empty.hidden = visible.length > 0;
  if (visible.length === 0) {
    if (app.query) {
      $('#empty-title').textContent = 'Ничего не найдено';
      $('#empty-text').textContent = 'Попробуйте изменить запрос или сбросить фильтр.';
    } else if (app.tab !== 'all') {
      $('#empty-title').textContent = 'В этой категории пусто';
      $('#empty-text').textContent = 'Переключитесь на «Все» — возможно, раздача в другом состоянии.';
    } else {
      $('#empty-title').textContent = 'Здесь пока пусто';
      $('#empty-text').textContent =
        'Перетащите .torrent в окно или вставьте magnet-ссылку — раздача появится мгновенно.';
    }
  }

  const total = app.torrents.length;
  $('#page-title').textContent = TAB_TITLES[app.tab] || 'Все раздачи';
  $('#page-sub').textContent = total
    ? `${visible.length} из ${total} ${plural(total, ['раздачи', 'раздач', 'раздач'])}`
    : 'Пока пусто';
  $('#list-hint').textContent = app.query ? `Найдено: ${visible.length}` : '';
}

function renderStatus() {
  const g = app.global;
  $('#s-port').textContent = g.port || '—';
  $('#s-session').textContent = `${bytes(g.session_down)} ↓ · ${bytes(g.session_up)} ↑`;
  $('#s-version').textContent = `mbtorrent ${app.version || ''}`.trim();
  if (app.version) $('#brand-version').textContent = `v${app.version}`;
}

function setOnline(online) {
  app.online = online;
  $('#conn').classList.toggle('is-off', !online);
  $('#conn-text').textContent = online ? 'Движок на связи' : 'Нет связи с движком';
}

/* ---------- действия ---------- */

async function doAction(hash, action, query = '') {
  try {
    await API.action(hash, action, query);
  } catch (e) {
    toast(e.message, 'err');
  }
  refresh();
}

async function togglePause(t) {
  await doAction(t.hash, t.state === 'paused' ? 'resume' : 'pause');
}

function confirmRemove(t) {
  openModal(
    `
    <div class="modal-head">
      <h2>Удалить раздачу?</h2>
      <button class="icon-btn" data-close title="Закрыть">${ICONS.x}</button>
    </div>
    <div class="modal-body">
      <p id="rm-name" style="margin:0 0 14px;font-weight:550"></p>
      <label class="check"><input type="checkbox" id="rm-data"> Удалить скачанные файлы с диска</label>
    </div>
    <div class="modal-foot">
      <button class="btn ghost" data-close>Отмена</button>
      <button class="btn danger" id="rm-ok">Удалить</button>
    </div>`,
    (overlay, close) => {
      $('#rm-name', overlay).textContent = t.name;
      $('#rm-ok', overlay).addEventListener('click', async () => {
        const del = $('#rm-data', overlay).checked;
        close();
        await doAction(t.hash, 'remove', del ? '?delete_data=1' : '');
        if (app.drawerHash === t.hash) closeDrawer();
        toast(del ? 'Раздача и файлы удалены' : 'Раздача удалена из списка');
      });
    }
  );
}

/* ---------- drawer ---------- */

function openDrawer(hash) {
  app.drawerHash = hash;
  const drawer = $('#drawer');
  const backdrop = $('#drawer-backdrop');
  drawer.hidden = false;
  backdrop.hidden = false;
  requestAnimationFrame(() => {
    drawer.classList.add('show');
    backdrop.classList.add('show');
  });
  renderDrawer();
  loadFiles(hash);
  clearInterval(app.filesTimer);
  app.filesTimer = setInterval(() => {
    if (app.drawerHash) loadFiles(app.drawerHash);
  }, 3000);
}

function closeDrawer() {
  app.drawerHash = null;
  clearInterval(app.filesTimer);
  const drawer = $('#drawer');
  const backdrop = $('#drawer-backdrop');
  drawer.classList.remove('show');
  backdrop.classList.remove('show');
  setTimeout(() => {
    if (!app.drawerHash) {
      drawer.hidden = true;
      backdrop.hidden = true;
    }
  }, 240);
}

function renderDrawer() {
  if (!app.drawerHash) return;
  const t = app.byHash.get(app.drawerHash);
  if (!t) {
    closeDrawer();
    return;
  }

  $('#d-badge').textContent = (STATES[t.state] || STATES.downloading).label;
  $('#d-name').textContent = t.name;
  $('#d-pct').textContent = pct(t.progress);
  $('#d-size').textContent = t.size > 0 ? `${bytes(t.done)} из ${bytes(t.size)}` : 'размер неизвестен';
  $('#d-bar').style.width = `${((t.progress || 0) * 100).toFixed(2)}%`;
  $('#d-down').textContent = `${speed(t.down_speed)}`;
  $('#d-up').textContent = `${speed(t.up_speed)}`;
  $('#d-ratio').textContent = t.ratio ? t.ratio.toFixed(2) : '0,00';
  $('#d-peers').textContent = `${t.seeds} сидов / ${t.peers} пиров`;
  $('#d-eta').textContent = eta(t.eta, t.state);
  $('#d-added').textContent = when(t.added);
  $('#d-path').textContent = t.save_path || '—';
  $('#d-files-count').textContent = t.file_count ? `(${t.file_count})` : '';

  const toggle = $('#d-toggle');
  toggle.textContent = t.state === 'paused' ? 'Продолжить' : 'Пауза';
  toggle.dataset.hash = t.hash;
  toggle.dataset.mode = t.state === 'paused' ? 'play' : 'pause';
}

async function loadFiles(hash) {
  const box = $('#d-files');
  try {
    const data = await API.files(hash);
    if (app.drawerHash !== hash) return;
    const files = data.files || [];
    if (!files.length) {
      box.innerHTML = '<p class="muted">Файлов нет</p>';
      return;
    }
    const frag = document.createDocumentFragment();
    for (const f of files) {
      const row = document.createElement('div');
      row.className = 'file';

      const name = document.createElement('span');
      name.className = 'file-name';
      name.textContent = f.path;
      name.title = f.path;

      const size = document.createElement('span');
      size.className = 'file-size';
      size.textContent = f.length > 0 ? bytes(f.length) : '—';

      const track = document.createElement('div');
      track.className = 'file-track';
      const bar = document.createElement('i');
      bar.style.width = `${Math.min(100, (f.progress || 0) * 100).toFixed(1)}%`;
      track.appendChild(bar);

      row.append(name, size, track);
      frag.appendChild(row);
    }
    box.replaceChildren(frag);
  } catch (e) {
    if (app.drawerHash === hash) {
      box.innerHTML = `<p class="muted">${e.message}</p>`;
    }
  }
}

/* ---------- добавление ---------- */

function toBase64(buffer) {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode.apply(null, bytes.subarray(i, i + chunk));
  }
  return btoa(binary);
}

async function addPayloads(payloads) {
  let added = 0;
  let dup = 0;
  let failed = 0;
  let firstError = '';

  for (const payload of payloads) {
    try {
      const res = await API.add(payload);
      if (res.added) added++;
      else dup++;
    } catch (e) {
      failed++;
      if (!firstError) firstError = e.message;
    }
  }

  if (added) toast(added === 1 ? 'Раздача добавлена' : `Добавлено раздач: ${added}`);
  if (dup) toast(dup === 1 ? 'Эта раздача уже в списке' : `Уже в списке: ${dup}`);
  if (failed) toast(firstError || `Не удалось добавить: ${failed}`, 'err');
  refresh();
}

function openAdd(prefill = '') {
  openModal(
    `
    <div class="modal-head">
      <h2>Добавить раздачу</h2>
      <button class="icon-btn" data-close title="Закрыть">${ICONS.x}</button>
    </div>
    <div class="modal-body">
      <label class="drop" id="add-drop">
        <input type="file" id="add-file" accept=".torrent" multiple hidden>
        ${ICONS.magnet}
        <b>Перетащите .torrent сюда</b>
        <span>или нажмите, чтобы выбрать файл</span>
      </label>

      <div class="field">
        <label for="add-links">Ссылки</label>
        <textarea class="input" id="add-links" rows="4"
          placeholder="magnet:?xt=urn:btih:...&#10;https://example.com/file.torrent"></textarea>
        <p class="hint">По одной ссылке в строке. Поддерживаются magnet и http(s).</p>
      </div>

      <label class="check"><input type="checkbox" id="add-paused"> Добавить на паузе</label>
    </div>
    <div class="modal-foot">
      <button class="btn ghost" data-close>Отмена</button>
      <button class="btn primary" id="add-submit">Добавить</button>
    </div>`,
    (overlay, close) => {
      const input = $('#add-links', overlay);
      const fileInput = $('#add-file', overlay);
      const drop = $('#add-drop', overlay);
      const submit = $('#add-submit', overlay);
      const chosen = [];

      input.value = prefill;

      fileInput.addEventListener('change', () => {
        chosen.push(...Array.from(fileInput.files));
        if (fileInput.files.length) {
          drop.querySelector('b').textContent = `Выбрано файлов: ${chosen.length}`;
        }
      });

      drop.addEventListener('dragover', (e) => {
        e.preventDefault();
        e.stopPropagation();
        drop.classList.add('is-over');
      });
      drop.addEventListener('dragleave', () => drop.classList.remove('is-over'));
      drop.addEventListener('drop', (e) => {
        e.preventDefault();
        e.stopPropagation();
        drop.classList.remove('is-over');
        const files = Array.from(e.dataTransfer.files || []).filter((f) =>
          f.name.toLowerCase().endsWith('.torrent')
        );
        if (files.length) {
          chosen.push(...files);
          drop.querySelector('b').textContent = `Выбрано файлов: ${chosen.length}`;
        }
      });

      submit.addEventListener('click', async () => {
        const links = input.value
          .split(/[\s\n]+/)
          .map((s) => s.trim())
          .filter(Boolean);
        const paused = $('#add-paused', overlay).checked;

        if (!links.length && !chosen.length) {
          toast('Вставьте ссылку или выберите .torrent', 'err');
          return;
        }

        submit.disabled = true;
        submit.textContent = 'Добавление…';

        const payloads = links.map((link) =>
          /^magnet:/i.test(link) ? { magnet: link, paused } : { url: link, paused }
        );

        for (const file of chosen) {
          try {
            const buf = await file.arrayBuffer();
            payloads.push({ torrent: toBase64(buf), paused });
          } catch {
            toast(`Не удалось прочитать ${file.name}`, 'err');
          }
        }

        close();
        await addPayloads(payloads);
      });

      setTimeout(() => input.focus(), 60);
    }
  );
}

/* ---------- настройки ---------- */

function openSettings() {
  const c = app.config;
  openModal(
    `
    <div class="modal-head">
      <h2>Настройки</h2>
      <button class="icon-btn" data-close title="Закрыть">${ICONS.x}</button>
    </div>
    <div class="modal-body">
      <div class="form-grid">
        <div class="field full">
          <label for="st-dir">Папка загрузок</label>
          <input class="input" id="st-dir" type="text" spellcheck="false">
        </div>
        <div class="field">
          <label for="st-listen">Порт BitTorrent</label>
          <input class="input" id="st-listen" type="number" min="1024" max="65535">
        </div>
        <div class="field">
          <label for="st-web">Порт панели</label>
          <input class="input" id="st-web" type="number" min="1024" max="65535">
        </div>
        <div class="field">
          <label for="st-down">Лимит скачивания, КБ/с</label>
          <input class="input" id="st-down" type="number" min="0">
        </div>
        <div class="field">
          <label for="st-up">Лимит раздачи, КБ/с</label>
          <input class="input" id="st-up" type="number" min="0">
        </div>
        <div class="field full">
          <label class="check" style="margin-top:0">
            <input type="checkbox" id="st-seed"> Раздавать после завершения
          </label>
          <p class="hint">0 в лимитах означает «без ограничений».</p>
        </div>
      </div>
    </div>
    <div class="modal-foot">
      <button class="btn ghost" data-close>Отмена</button>
      <button class="btn primary" id="st-save">Сохранить</button>
    </div>`,
    (overlay, close) => {
      $('#st-dir', overlay).value = c.download_dir || '';
      $('#st-listen', overlay).value = c.listen_port || 51413;
      $('#st-web', overlay).value = c.web_port || 8642;
      $('#st-down', overlay).value = c.down_limit_kb || 0;
      $('#st-up', overlay).value = c.up_limit_kb || 0;
      $('#st-seed', overlay).checked = c.seed !== false;

      $('#st-save', overlay).addEventListener('click', async () => {
        const next = {
          download_dir: $('#st-dir', overlay).value.trim(),
          listen_port: Number($('#st-listen', overlay).value),
          web_port: Number($('#st-web', overlay).value),
          down_limit_kb: Number($('#st-down', overlay).value),
          up_limit_kb: Number($('#st-up', overlay).value),
          seed: $('#st-seed', overlay).checked,
          start_paused: !!c.start_paused,
        };
        const btn = $('#st-save', overlay);
        btn.disabled = true;
        btn.textContent = 'Сохраняю…';
        try {
          const res = await API.saveSettings(next);
          close();
          toast('Настройки сохранены');
          if (res.restart_required) {
            toast('Новый порт панели заработает после перезапуска', 'err');
          }
          refresh();
        } catch (e) {
          toast(e.message, 'err');
          btn.disabled = false;
          btn.textContent = 'Сохранить';
        }
      });
    }
  );
}

function openQuit() {
  openModal(
    `
    <div class="modal-head">
      <h2>Закрыть mbtorrent?</h2>
      <button class="icon-btn" data-close title="Закрыть">${ICONS.x}</button>
    </div>
    <div class="modal-body">
      <p class="muted" style="margin-top:0">Активные загрузки остановятся. Прогресс сохранится — при следующем запуске всё продолжится с того же места.</p>
    </div>
    <div class="modal-foot">
      <button class="btn ghost" data-close>Отмена</button>
      <button class="btn danger" id="quit-ok">Выйти</button>
    </div>`,
    (overlay, close) => {
      $('#quit-ok', overlay).addEventListener('click', async () => {
        close();
        try {
          await API.quit();
          toast('mbtorrent закрывается…');
          setTimeout(() => {
            document.body.innerHTML =
              '<div style="display:grid;place-items:center;height:100dvh;font-family:system-ui;color:#8b98ad">mbtorrent закрыт. Вкладку можно закрыть.</div>';
          }, 900);
        } catch (e) {
          toast(e.message, 'err');
        }
      });
    }
  );
}

/* ---------- события ---------- */

function bindEvents() {
  $('#btn-add').addEventListener('click', () => openAdd());
  $('#empty-add').addEventListener('click', () => openAdd());
  $('#btn-settings').addEventListener('click', openSettings);
  $('#btn-quit').addEventListener('click', openQuit);
  $('#d-close').addEventListener('click', closeDrawer);
  $('#drawer-backdrop').addEventListener('click', closeDrawer);

  $('#nav').addEventListener('click', (e) => {
    const btn = e.target.closest('[data-tab]');
    if (btn) setTab(btn.dataset.tab);
  });
  $('#tabs').addEventListener('click', (e) => {
    const btn = e.target.closest('[data-tab]');
    if (btn) setTab(btn.dataset.tab);
  });

  $('#search').addEventListener('input', (e) => {
    app.query = e.target.value.trim();
    renderList();
  });

  $('#list').addEventListener('click', (e) => {
    const row = e.target.closest('.row');
    if (!row) return;
    const hash = row.dataset.hash;
    const t = app.byHash.get(hash);
    if (!t) return;

    const btn = e.target.closest('[data-act]');
    if (!btn) {
      openDrawer(hash);
      return;
    }
    switch (btn.dataset.act) {
      case 'toggle':
        togglePause(t);
        break;
      case 'recheck':
        doAction(hash, 'recheck');
        toast('Проверяю файлы…');
        break;
      case 'reveal':
        doAction(hash, 'reveal');
        break;
      case 'remove':
        confirmRemove(t);
        break;
    }
  });

  $('#d-toggle').addEventListener('click', (e) => {
    const t = app.byHash.get(e.currentTarget.dataset.hash);
    if (t) togglePause(t);
  });
  $('#d-reveal').addEventListener('click', () => {
    if (app.drawerHash) doAction(app.drawerHash, 'reveal');
  });
  $('#d-remove').addEventListener('click', () => {
    const t = app.byHash.get(app.drawerHash);
    if (t) confirmRemove(t);
  });

  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      if (modalStack.length) {
        modalStack[modalStack.length - 1]();
      } else if (app.drawerHash) {
        closeDrawer();
      }
      return;
    }
    const typing =
      e.target instanceof HTMLInputElement ||
      e.target instanceof HTMLTextAreaElement ||
      e.target.isContentEditable;

    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'n') {
      e.preventDefault();
      openAdd();
    } else if (e.key === '/' && !typing) {
      e.preventDefault();
      $('#search').focus();
    }
  });

  document.addEventListener('paste', (e) => {
    if (modalStack.length) return;
    const target = e.target;
    if (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement) return;
    const text = (e.clipboardData || window.clipboardData)?.getData('text')?.trim();
    if (text && /^magnet:/i.test(text)) {
      e.preventDefault();
      openAdd(text);
    }
  });

  bindDragAndDrop();

  document.addEventListener('visibilitychange', () => {
    if (!document.hidden) refresh();
  });
}

function setTab(tab) {
  app.tab = tab;
  $$('#nav .nav-item').forEach((b) => b.classList.toggle('is-active', b.dataset.tab === tab));
  $$('#tabs .tab').forEach((b) => b.classList.toggle('is-active', b.dataset.tab === tab));
  renderList();
}

let dragDepth = 0;

function bindDragAndDrop() {
  const zone = $('#dropzone');

  window.addEventListener('dragenter', (e) => {
    if (!e.dataTransfer || !Array.from(e.dataTransfer.types).includes('Files')) return;
    dragDepth++;
    zone.hidden = false;
  });
  window.addEventListener('dragleave', (e) => {
    if (e.relatedTarget) return;
    dragDepth = Math.max(0, dragDepth - 1);
    if (!dragDepth) zone.hidden = true;
  });
  window.addEventListener('dragover', (e) => e.preventDefault());
  window.addEventListener('drop', async (e) => {
    e.preventDefault();
    dragDepth = 0;
    zone.hidden = true;
    if (modalStack.length) return;

    const files = Array.from(e.dataTransfer.files || []).filter((f) =>
      f.name.toLowerCase().endsWith('.torrent')
    );
    if (files.length) {
      const payloads = [];
      for (const file of files) {
        try {
          const buf = await file.arrayBuffer();
          payloads.push({ torrent: toBase64(buf), paused: false });
        } catch {
          toast(`Не удалось прочитать ${file.name}`, 'err');
        }
      }
      await addPayloads(payloads);
      return;
    }

    const text = e.dataTransfer.getData('text/plain')?.trim();
    if (text && /^magnet:/i.test(text)) {
      await addPayloads([{ magnet: text, paused: false }]);
    }
  });
}

/* ---------- поллинг ---------- */

async function refresh() {
  try {
    const s = await API.state();
    applyState(s);
    setOnline(true);
  } catch {
    setOnline(false);
  }
}

async function tick() {
  await refresh();
  app.pollTimer = setTimeout(tick, document.hidden ? 2500 : 1000);
}

/* ---------- старт ---------- */

initTheme();
bindEvents();
tick();
