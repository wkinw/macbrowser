(() => {
'use strict';

const $ = (id) => document.getElementById(id);
const enc = encodeURIComponent;
const BLANK_IMG = 'data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw==';

const COLOR_HEX = {1:'var(--c1)',2:'var(--c2)',3:'var(--c3)',4:'var(--c4)',5:'var(--c5)',6:'var(--c6)',7:'var(--c7)'};
const COLOR_NAME = {1:'灰',2:'綠',3:'紫',4:'藍',5:'黃',6:'紅',7:'橘'};

const state = {
  path: '',
  back: [],
  fwd: [],
  entries: [],
  view: [],
  parent: null,
  kind: 'local',
  labelMap: new Map(),
  selected: new Set(),
  anchor: -1,
  search: '',
  searchQuery: '',
  searchMode: false,
  roots: null,
  prefs: {tileSize:168, sortKey:'name', sortDir:'asc', showHidden:false, showLabels:true, foldersFirst:true, hoverDelay:500},
  labelFilter: new Set(),
  clipboard: null,
  lightbox: {open:false, list:[], idx:0, timer:null, preloadT:null},
  navSeq: 0,
  tileW: 168,
  tileH: 198,
  cols: 1,
  pool: [],
};

async function api(path, opts) {
  const r = await fetch(path, opts);
  const ct = r.headers.get('content-type') || '';
  const data = ct.includes('json') ? await r.json() : await r.text();
  if (!r.ok) throw new Error((data && data.error) || ('HTTP ' + r.status));
  return data;
}
function post(path, body) {
  return api(path, {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify(body)});
}

const isSMB = (p) => p.startsWith('smb://');
const isRootLike = (p) => !p ||
  /^[a-zA-Z]:\\?$/.test(p) ||
  /^smb:\/\/[^\/]+\/?$/.test(p) ||
  /^\\\\[^\\]+\\?$/.test(p);

function joinV(base, name) {
  if (!base) return name;
  if (isSMB(base)) return base.replace(/\/+$/, '') + '/' + name;
  if (/[\\\/]$/.test(base)) return base + name;
  return base + '\\' + name;
}

function parentV(p) {
  if (!p) return '';
  if (isSMB(p)) {
    const t = p.replace(/\/+$/, '');
    const i = t.lastIndexOf('/');
    if (i <= 6) return '';
    return t.slice(0, i);
  }
  const t = p.replace(/[\\\/]+$/, '');
  if (!t) return '';
  if (/^[a-zA-Z]:$/.test(t)) return '';
  if (/^\\\\[^\\]+\\[^\\]+$/.test(t)) return '';
  const i = Math.max(t.lastIndexOf('\\'), t.lastIndexOf('/'));
  if (i < 0) return '';
  let par = t.slice(0, i);
  if (/^[a-zA-Z]:$/.test(par)) par += '\\';
  return par;
}

function entryPath(e) { return e.path || joinV(state.path, e.name); }
function entryLabel(e) { return e.label || state.labelMap.get(e.name) || null; }

function formatBytes(n) {
  n = n || 0;
  if (n < 1024) return n + ' B';
  const units = ['KB','MB','GB','TB'];
  let v = n / 1024, i = 0;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return (v >= 100 ? v.toFixed(0) : v.toFixed(1)) + ' ' + units[i];
}

function formatDate(s) {
  if (!s) return '';
  const d = new Date(s);
  if (isNaN(d)) return '';
  const p = (x) => String(x).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth()+1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
}

function fileIcon(name) {
  const ext = (name.split('.').pop() || '').toLowerCase();
  if (['mp4','mkv','avi','mov','wmv','webm','m4v'].includes(ext)) return '🎬';
  if (['mp3','flac','wav','m4a','aac','ogg','wma'].includes(ext)) return '🎵';
  if (['zip','rar','7z','tar','gz'].includes(ext)) return '🗜️';
  if (ext === 'pdf') return '📕';
  if (['doc','docx','rtf','odt'].includes(ext)) return '📘';
  if (['xls','xlsx','csv','ods'].includes(ext)) return '📗';
  if (['ppt','pptx','odp'].includes(ext)) return '📙';
  if (['txt','md','log','json','xml','yml','yaml','ini','cfg'].includes(ext)) return '📄';
  if (['exe','msi','bat','cmd','ps1'].includes(ext)) return '⚙️';
  if (['heic','heif','avif','svg','psd','raw','cr2','nef','arw'].includes(ext)) return '🖼️';
  if (['iso','img','vhd'].includes(ext)) return '💿';
  return '📄';
}

let toastTimer = 0;
function toast(msg, isErr) {
  const t = $('toast');
  t.textContent = msg;
  t.className = isErr ? 'err' : '';
  t.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { t.hidden = true; }, isErr ? 6000 : 2500);
}

function openModal(title, bodyHTML, buttons) {
  $('modalTitle').textContent = title;
  $('modalBody').innerHTML = bodyHTML;
  const bb = $('modalBtns');
  bb.replaceChildren();
  for (const b of buttons || []) {
    const el = document.createElement('button');
    el.textContent = b.label;
    if (b.cls) el.className = b.cls;
    el.addEventListener('click', () => {
      const r = b.onClick ? b.onClick() : undefined;
      if (r !== false) closeModal();
    });
    bb.appendChild(el);
  }
  $('modal').hidden = false;
}
function closeModal() {
  $('modal').hidden = true;
  $('modalBody').innerHTML = '';
}
function formModal(title, label, initial, onSubmit, okText) {
  openModal(title, `<div class="frm"><label>${escapeHtml(label)}</label><input id="fmInput" value=""></div>`, [
    {label:'取消'},
    {label: okText || '確定', cls:'primary', onClick: () => {
      const v = $('fmInput').value.trim();
      if (!v) { toast('名稱不可為空', true); return false; }
      Promise.resolve(onSubmit(v)).catch((e) => toast(e.message, true));
    }},
  ]);
  const inp = $('fmInput');
  inp.value = initial || '';
  setTimeout(() => { inp.focus(); inp.select(); }, 30);
  inp.addEventListener('keydown', (ev) => {
    if (ev.key === 'Enter') {
      ev.preventDefault();
      const btn = $('modalBtns').querySelector('.primary');
      if (btn) btn.click();
    }
  });
}

let saveTimer = 0;
function savePrefs() {
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => {
    post('/api/config', {prefs: state.prefs, lastPath: state.path}).catch(() => {});
  }, 400);
}

function applyPrefsToUI() {
  $('sort').value = state.prefs.sortKey;
  $('sortDir').textContent = state.prefs.sortDir === 'asc' ? '↑' : '↓';
  $('tileSize').value = state.prefs.tileSize;
  $('hoverDelay').value = (state.prefs.hoverDelay || 500) / 1000;
  $('showLabels').checked = state.prefs.showLabels;
  $('showHidden').checked = state.prefs.showHidden;
}

function rootsEntries() {
  const out = [];
  for (const d of (state.roots ? state.roots.drives : [])) {
    out.push({name: d.name, dir:true, path:d.path, size:0, mod:null, thumb:false});
  }
  for (const c of (state.roots ? state.roots.connections : [])) {
    out.push({name: c.name, dir:true, path:c.path, size:0, mod:null, thumb:false});
  }
  return out;
}

function sideItem(icon, name, path, opts) {
  opts = opts || {};
  const el = document.createElement('div');
  el.className = 'side-item';
  el.dataset.path = path;
  const ic = document.createElement('span');
  ic.className = 'ic';
  ic.textContent = icon;
  const nm = document.createElement('span');
  nm.className = 'nm';
  nm.textContent = name;
  el.append(ic, nm);
  if (opts.onDelete) {
    const del = document.createElement('button');
    del.className = 'mini del';
    del.textContent = '✕';
    del.title = '刪除連線';
    del.addEventListener('click', (ev) => { ev.stopPropagation(); opts.onDelete(); });
    el.appendChild(del);
  }
  el.addEventListener('click', () => navigate(path, {push:true}));
  if (opts.onEdit) {
    el.addEventListener('dblclick', opts.onEdit);
    el.title = '雙擊編輯連線';
  }
  return el;
}

function renderSidebar() {
  const places = $('places'), drives = $('drives'), conns = $('conns');
  places.replaceChildren(); drives.replaceChildren(); conns.replaceChildren();
  if (!state.roots) return;
  places.appendChild(sideItem('🖥️', '電腦', ''));
  for (const p of state.roots.places) {
    const icon = p.type === 'home' ? '🏠' : '📁';
    places.appendChild(sideItem(icon, p.name, p.path));
  }
  for (const d of state.roots.drives) {
    drives.appendChild(sideItem('💽', d.name, d.path));
  }
  for (const c of state.roots.connections) {
    conns.appendChild(sideItem('🌐', c.name, c.path, {
      onEdit: () => {
        const full = findConnection(c.name);
        connModal(full || {name: c.name, host:'', port:445});
      },
      onDelete: () => deleteConnection(c.name),
    }));
  }
  highlightActive();
}

function findConnection(name) {
  for (const c of (state.roots ? state.roots.connections : [])) {
    if (c.name === name) return c;
  }
  return null;
}

async function reloadRoots() {
  state.roots = await api('/api/roots');
  renderSidebar();
}

function highlightActive() {
  for (const el of document.querySelectorAll('.side-item')) {
    el.classList.toggle('active', el.dataset.path === state.path && state.path !== '');
  }
}

function connModal(conn) {
  const editing = !!conn && !!conn.host;
  openModal(editing ? '編輯 SMB 連線' : '新增 SMB 連線', `
    <div class="frm">
      <label>名稱</label><input id="cfName" placeholder="nas">
      <label>主機</label><input id="cfHost" placeholder="192.168.1.10 或 nas.local">
      <label>連接埠</label><input id="cfPort" placeholder="445">
      <label>共享名稱</label><input id="cfShare" placeholder="留空＝先列出共享清單">
      <label>子目錄</label><input id="cfBase" placeholder="（選填）">
      <label>帳號</label><input id="cfUser">
      <label>網域</label><input id="cfDomain">
      <label>密碼</label><input id="cfPass" type="password">
    </div>
    <div class="hint">密碼以明文儲存於本機設定檔：%AppData%\\macbrowser\\config.json</div>
  `, [
    {label:'取消'},
    ...(editing ? [{label:'刪除', cls:'danger', onClick: () => deleteConnection(conn.name)}] : []),
    {label:'儲存', cls:'primary', onClick: () => {
      const c = {
        name: $('cfName').value.trim(),
        host: $('cfHost').value.trim(),
        port: parseInt($('cfPort').value, 10) || 445,
        share: $('cfShare').value.trim(),
        base: $('cfBase').value.trim(),
        user: $('cfUser').value.trim(),
        domain: $('cfDomain').value.trim(),
        password: $('cfPass').value,
      };
      if (!c.name || !c.host) { toast('名稱與主機不可為空', true); return false; }
      post('/api/connections', {action:'save', connection:c})
        .then(() => { toast('已儲存連線'); return reloadRoots(); })
        .catch((e) => toast(e.message, true));
    }},
  ]);
  const c = conn || {};
  $('cfName').value = c.name || '';
  $('cfHost').value = c.host || '';
  $('cfPort').value = c.port || 445;
  $('cfShare').value = c.share || '';
  $('cfBase').value = c.base || '';
  $('cfUser').value = c.user || '';
  $('cfDomain').value = c.domain || '';
  $('cfPass').value = c.password || '';
}

function deleteConnection(name) {
  openModal('刪除連線', `<div>確定要刪除 SMB 連線「${escapeHtml(name)}」嗎？</div>`, [
    {label:'取消'},
    {label:'刪除', cls:'danger', onClick: () => {
      post('/api/connections', {action:'delete', connection:{name}})
        .then(() => { toast('已刪除連線'); return reloadRoots(); })
        .catch((e) => toast(e.message, true));
    }},
  ]);
}

async function navigate(p, opts) {
  opts = opts || {};
  if (opts.push && state.path !== p) {
    state.back.push(state.path);
    state.fwd = [];
  }
  state.path = p;
  state.selected.clear();
  state.anchor = -1;
  state.search = '';
  state.searchQuery = '';
  state.searchMode = false;
  $('search').value = '';
  const seq = ++state.navSeq;
  try {
    if (p === '') {
      state.entries = rootsEntries();
      state.kind = 'roots';
      state.parent = null;
    } else {
      const data = await api('/api/list?p=' + enc(p));
      if (seq !== state.navSeq) return;
      state.entries = data.entries || [];
      state.parent = data.parent || '';
      state.kind = data.kind || 'local';
    }
  } catch (e) {
    if (seq !== state.navSeq) return;
    toast(e.message, true);
    state.entries = [];
    state.parent = null;
  }
  $('addr').value = p;
  updateNavButtons();
  state.labelMap = new Map();
  refresh();
  savePrefs();
  highlightActive();
  if (state.prefs.showLabels && p !== '') refreshLabels(p);
}

function updateNavButtons() {
  $('up').disabled = !state.parent && state.path !== '';
  $('back').disabled = state.back.length === 0;
  $('fwd').disabled = state.fwd.length === 0;
}

function goBack() {
  if (!state.back.length) return;
  const p = state.back.pop();
  state.fwd.push(state.path);
  navigate(p);
}

function goFwd() {
  if (!state.fwd.length) return;
  const p = state.fwd.pop();
  state.back.push(state.path);
  navigate(p);
}

function goUp() {
  if (state.path === '') return;
  const par = state.parent !== null ? state.parent : parentV(state.path);
  navigate(par || '', {push:true});
}

async function doRefresh() {
  const p = state.path;
  const seq = ++state.navSeq;
  try {
    if (p === '') {
      state.entries = rootsEntries();
    } else {
      const data = await api('/api/list?p=' + enc(p));
      if (seq !== state.navSeq) return;
      state.entries = data.entries || [];
      state.parent = data.parent || '';
      state.kind = data.kind || 'local';
    }
  } catch (e) {
    toast(e.message, true);
  }
  state.searchMode = false;
  refresh();
  if (state.prefs.showLabels && p !== '') refreshLabels(p);
}

async function refreshLabels(p) {
  try {
    const data = await api('/api/labels?p=' + enc(p));
    if (state.path !== p) return;
    const map = new Map();
    for (const name of Object.keys(data.labels || {})) {
      map.set(name, data.labels[name]);
    }
    state.labelMap = map;
    if (state.labelFilter.size > 0) refresh();
    else { renderVisible(true); updateStatus(); }
  } catch (e) {
  }
}

function sortView() {
  const key = state.prefs.sortKey;
  const dir = state.prefs.sortDir === 'asc' ? 1 : -1;
  state.view.sort((a, b) => {
    if (state.prefs.foldersFirst && a.dir !== b.dir) return a.dir ? -1 : 1;
    let r = 0;
    if (key === 'size') r = (a.size || 0) - (b.size || 0);
    else if (key === 'mod') r = new Date(a.mod || 0) - new Date(b.mod || 0);
    else if (key === 'type') r = (a.name.split('.').pop() || '').toLowerCase().localeCompare((b.name.split('.').pop() || '').toLowerCase());
    else r = a.name.localeCompare(b.name, 'zh-Hant', {numeric:true, sensitivity:'base'});
    if (r === 0) r = a.name.localeCompare(b.name, 'zh-Hant', {numeric:true, sensitivity:'base'});
    return r * dir;
  });
}

function applyFilter() {
  const q = state.search.trim().toLowerCase();
  state.view = state.entries.filter((e) => {
    if (e.hidden && !state.prefs.showHidden) return false;
    if (q && !e.name.toLowerCase().includes(q)) return false;
    if (state.labelFilter.size > 0) {
      const lbl = entryLabel(e);
      const c = lbl ? lbl.color : 0;
      if (!state.labelFilter.has(c)) return false;
    }
    return true;
  });
  sortView();
}

function refresh() {
  state.searchMode = false;
  state.searchQuery = '';
  applyFilter();
  layout();
  updateStatus();
}

function layout() {
  const wrap = $('gridwrap');
  const tw = state.prefs.tileSize;
  const th = tw + 30;
  state.tileW = tw;
  state.tileH = th;
  document.documentElement.style.setProperty('--tw', tw + 'px');
  document.documentElement.style.setProperty('--th', th + 'px');
  const gap = 8;
  const avail = Math.max(tw + gap, wrap.clientWidth - gap);
  state.cols = Math.max(1, Math.floor(avail / (tw + gap)));
  const rows = Math.ceil(state.view.length / state.cols);
  $('grid').style.height = (rows * th + gap) + 'px';
  $('empty').hidden = state.view.length > 0;
  renderVisible(true);
}

function ensurePool(count) {
  const grid = $('grid');
  while (state.pool.length < count) {
    const el = document.createElement('div');
    el.className = 'tile';
    el.innerHTML = `<div class="thumb-box"><i class="ftab"></i><div class="ficon"></div><img alt=""><span class="dot"></span></div><div class="cap"></div>`;
    const img = el.querySelector('img');
    img.src = BLANK_IMG;
    img.addEventListener('load', () => {
      const want = img.dataset.want || '';
      if (want && img.getAttribute('src') === want) {
        img.style.display = 'block';
        el.querySelector('.ficon').style.display = 'none';
      }
    });
    img.addEventListener('error', () => {
      img.style.display = 'none';
      const icon = el.querySelector('.ficon');
      if (el.dataset.isdir) {
        const p = el.dataset.p || '';
        if (/^[a-zA-Z]:\\?$/.test(p)) icon.textContent = '💽';
        else if (isSMB(p)) icon.textContent = '🌐';
        else icon.textContent = '📁';
      } else if (icon.textContent === '') {
        icon.textContent = '🖼️';
      }
      icon.style.display = '';
    });
    el.addEventListener('click', (ev) => {
      hideHoverPreview();
      const i = parseInt(el.dataset.i || '-1', 10);
      if (i >= 0) onTileClick(ev, i);
    });
    el.addEventListener('dblclick', () => {
      hideHoverPreview();
      const i = parseInt(el.dataset.i || '-1', 10);
      if (i >= 0) openEntry(i);
    });
    el.addEventListener('contextmenu', (ev) => {
      hideHoverPreview();
      ev.preventDefault();
      const i = parseInt(el.dataset.i || '-1', 10);
      if (i < 0) return;
      if (!state.selected.has(state.view[i].name)) {
        state.selected.clear();
        state.selected.add(state.view[i].name);
        state.anchor = i;
        renderVisible(true);
        updateStatus();
      }
      showCtx(ev, state.view[i]);
    });
    el.draggable = true;
    el.addEventListener('dragstart', (ev) => {
      hideHoverPreview();
      const i = parseInt(el.dataset.i || '-1', 10);
      const e = state.view[i];
      if (!e || e.dir || !ev.dataTransfer) {
        ev.preventDefault();
        return;
      }
      const url = new URL('/api/file?p=' + enc(entryPath(e)), location.href).href;
      const mime = e.thumb ? 'image/jpeg' : 'application/octet-stream';
      ev.dataTransfer.setData('DownloadURL', mime + ':' + e.name + ':' + url);
      ev.dataTransfer.effectAllowed = 'copy';
    });
    el.addEventListener('mouseenter', () => armHoverPreview(el));
    el.addEventListener('mouseleave', () => hideHoverPreview());
    grid.appendChild(el);
    state.pool.push(el);
  }
  return state.pool;
}

function unlockTiles(from) {
  for (let k = from; k < state.pool.length; k++) state.pool[k].hidden = true;
}

function renderVisible(force) {
  const wrap = $('gridwrap');
  const st = wrap.scrollTop;
  const h = wrap.clientHeight;
  const th = state.tileH;
  const cols = state.cols;
  const n = state.view.length;
  const totalRows = Math.ceil(n / cols);
  const row0 = Math.max(0, Math.floor(st / th) - 2);
  const row1 = Math.min(totalRows, Math.ceil((st + h) / th) + 2);
  const i0 = Math.min(n, row0 * cols);
  const i1 = Math.min(n, row1 * cols);
  const need = Math.max(0, i1 - i0);
  const pool = ensurePool(need);
  const gap = 8;
  let k = 0;
  for (let i = i0; i < i1; i++, k++) {
    const el = pool[k];
    const col = i % cols;
    const row = (i / cols) | 0;
    el.style.transform = `translate(${gap + col * (state.tileW + gap)}px, ${gap + row * th}px)`;
    if (force || el.dataset.i !== String(i)) bind(el, i);
    el.hidden = false;
  }
  unlockTiles(k);
}

function bind(el, i) {
  const e = state.view[i];
  el.dataset.i = i;
  const img = el.querySelector('img');
  const icon = el.querySelector('.ficon');
  const cap = el.querySelector('.cap');
  const dot = el.querySelector('.dot');
  cap.textContent = e.name;
  cap.title = e.name;
  el.classList.toggle('sel', state.selected.has(e.name));
  el.classList.toggle('cut', isCut(e));
  const lbl = entryLabel(e);
  const color = lbl ? lbl.color : 0;
  if (color > 0 && state.prefs.showLabels) {
    dot.style.background = COLOR_HEX[color] || 'var(--c1)';
    dot.style.display = 'block';
    const names = lbl.tags && lbl.tags.length ? lbl.tags.join('、') : (COLOR_NAME[color] || '');
    dot.title = names;
  } else {
    dot.style.display = 'none';
  }
  const box = el.querySelector('.thumb-box');
  const tab = el.querySelector('.ftab');
  if (e.dir) {
    box.classList.add('fold');
    tab.style.background = color > 0 && state.prefs.showLabels ? (COLOR_HEX[color] || '') : '';
  } else {
    box.classList.remove('fold');
  }
  img.dataset.want = '';
  const pth = entryPath(e);
  el.dataset.p = pth;
  el.dataset.isdir = e.dir ? '1' : '';
  if (e.dir) {
    if (!isRootLike(pth)) {
      icon.style.display = 'none';
      icon.textContent = '';
      const url = thumbURL(e);
      if (img.getAttribute('src') === url && img.style.display === 'block') return;
      img.dataset.want = url;
      img.style.display = 'none';
      img.setAttribute('src', url);
      return;
    }
    img.style.display = 'none';
    img.setAttribute('src', BLANK_IMG);
    icon.style.display = '';
    if (/^[a-zA-Z]:\\?$/.test(pth)) icon.textContent = '💽';
    else if (isSMB(pth)) icon.textContent = '🌐';
    else icon.textContent = '📁';
    return;
  }
  if (e.thumb) {
    const url = thumbURL(e);
    icon.style.display = 'none';
    icon.textContent = '';
    if (img.getAttribute('src') === url && img.style.display === 'block') return;
    img.dataset.want = url;
    img.style.display = 'none';
    img.setAttribute('src', url);
    return;
  }
  img.style.display = 'none';
  img.setAttribute('src', BLANK_IMG);
  icon.style.display = '';
  icon.textContent = fileIcon(e.name);
}

function thumbURL(e, size) {
  const s = size || (state.prefs.tileSize <= 128 ? 192 : (state.prefs.tileSize <= 224 ? 256 : 384));
  const m = e.mod ? Date.parse(e.mod) : '';
  const z = e.size != null ? e.size : '';
  return `/api/thumb?p=${enc(entryPath(e))}&s=${s}&m=${enc(String(m || ''))}&z=${enc(String(z))}`;
}

let hoverTimer = 0;
let hoverTile = null;

function hoverPreviewURL(e) {
  if (e.dir) return isRootLike(entryPath(e)) ? '' : thumbURL(e, 1600);
  return e.thumb ? thumbURL(e, 1600) : '';
}

function hideHoverPreview() {
  clearTimeout(hoverTimer);
  hoverTimer = 0;
  hoverTile = null;
  const hp = $('hoverpreview');
  if (hp) {
    hp.hidden = true;
    const img = hp.querySelector('img');
    img.onload = null;
    img.onerror = null;
    img.removeAttribute('src');
  }
}

function armHoverPreview(el) {
  hideHoverPreview();
  const i = parseInt(el.dataset.i || '-1', 10);
  const e = state.view[i];
  if (!e || !$('lightbox').hidden) return;
  const url = hoverPreviewURL(e);
  if (!url) return;
  hoverTile = el;
  hoverTimer = setTimeout(() => {
    if (hoverTile !== el || !$('lightbox').hidden) return;
    const hp = $('hoverpreview');
    const img = hp.querySelector('img');
    const cap = hp.querySelector('.hpcap');
    img.onload = () => {
      if (hoverTile !== el) return;
      cap.textContent = e.name + (e.dir || e.size == null ? '' : '　' + formatBytes(e.size));
      hp.style.visibility = 'hidden';
      hp.hidden = false;
      positionHoverPreview(el, hp);
      hp.style.visibility = '';
    };
    img.onerror = () => hideHoverPreview();
    img.src = url;
  }, state.prefs.hoverDelay > 0 ? state.prefs.hoverDelay : 500);
}

function positionHoverPreview(el, hp) {
  const r = el.getBoundingClientRect();
  const w = hp.offsetWidth;
  const h = hp.offsetHeight;
  let x = r.right + 14;
  if (x + w > window.innerWidth - 10) x = r.left - w - 14;
  if (x < 10) x = Math.max(10, Math.min(window.innerWidth - w - 10, r.left));
  let y = r.top + (r.height - h) / 2;
  y = Math.max(10, Math.min(window.innerHeight - h - 10, y));
  hp.style.left = x + 'px';
  hp.style.top = y + 'px';
}

function onTileClick(ev, idx) {
  const e = state.view[idx];
  if (!e) return;
  hideCtx();
  if (ev.shiftKey && state.anchor >= 0) {
    const a = Math.min(state.anchor, idx);
    const b = Math.max(state.anchor, idx);
    if (!ev.ctrlKey) state.selected.clear();
    for (let i = a; i <= b; i++) state.selected.add(state.view[i].name);
  } else if (ev.ctrlKey) {
    if (state.selected.has(e.name)) state.selected.delete(e.name);
    else state.selected.add(e.name);
    state.anchor = idx;
  } else {
    state.selected.clear();
    state.selected.add(e.name);
    state.anchor = idx;
  }
  renderVisible(true);
  updateStatus();
}

function openEntry(idx) {
  hideHoverPreview();
  const e = state.view[idx];
  if (!e) return;
  if (e.dir) {
    const p = entryPath(e);
    if (p) navigate(p, {push:true});
    return;
  }
  if (e.thumb) {
    openLightbox(e);
    return;
  }
  post('/api/open', {p: entryPath(e)})
    .then((r) => toast('已開啟：' + r.path))
    .catch((err) => toast(err.message, true));
}

function updateStatus() {
  let files = 0, dirs = 0, bytes = 0;
  for (const e of state.view) {
    if (e.dir) dirs++;
    else { files++; bytes += e.size || 0; }
  }
  let left;
  if (state.searchMode) {
    left = `搜尋「${state.searchQuery}」：${state.view.length} 項`;
  } else {
    left = `${files} 個檔案、${dirs} 個資料夾`;
    if (state.search) left += `（篩選後 ${state.view.length}/${state.entries.length}）`;
    if (state.labelFilter.size > 0) left += `（色標篩選）`;
  }
  const sel = state.selected.size;
  let right = formatBytes(bytes);
  if (sel > 0) right = `已選 ${sel} 項　|　` + right;
  $('statusL').textContent = left;
  $('statusR').textContent = right;
}

function moveSel(e, key) {
  const cols = state.cols;
  const n = state.view.length;
  if (n === 0) return;
  let idx = state.anchor >= 0 ? state.anchor : 0;
  if (state.anchor < 0) {
    state.selected.clear();
    state.selected.add(state.view[0].name);
    state.anchor = 0;
    ensureVisible(0);
    renderVisible(true);
    updateStatus();
    return;
  }
  if (key === 'ArrowLeft') idx -= 1;
  else if (key === 'ArrowRight') idx += 1;
  else if (key === 'ArrowUp') idx -= cols;
  else if (key === 'ArrowDown') idx += cols;
  if (idx < 0) idx = 0;
  if (idx >= n) idx = n - 1;
  state.anchor = idx;
  state.selected.clear();
  state.selected.add(state.view[idx].name);
  ensureVisible(idx);
  renderVisible(true);
  updateStatus();
}

function ensureVisible(idx) {
  const wrap = $('gridwrap');
  const cols = state.cols;
  const row = (idx / cols) | 0;
  const top = row * state.tileH;
  const bottom = top + state.tileH;
  if (top < wrap.scrollTop) wrap.scrollTop = top;
  else if (bottom > wrap.scrollTop + wrap.clientHeight) wrap.scrollTop = bottom - wrap.clientHeight;
}

function colorRow(entry) {
  const wrap = document.createElement('div');
  wrap.className = 'ctx-colors';
  const head = document.createElement('span');
  head.className = 'ctx-head';
  head.textContent = '色標';
  wrap.appendChild(head);
  const cur = entry ? entryLabel(entry) : null;
  const curColor = cur && cur.color ? cur.color : 0;
  const mk = (c, title) => {
    const b = document.createElement('button');
    b.className = 'sw' + (c === curColor ? ' cur' : '');
    b.title = title;
    if (c === 0) b.innerHTML = '<span class="none">✕</span>';
    else b.style.background = `var(--c${c})`;
    b.addEventListener('click', (ev) => {
      ev.stopPropagation();
      hideCtx();
      const targets = (entry && state.selected.has(entry.name))
        ? state.view.filter((x) => state.selected.has(x.name))
        : [entry];
      setColorOn(targets, c);
    });
    return b;
  };
  wrap.appendChild(mk(0, '無色標'));
  for (let c = 1; c <= 7; c++) wrap.appendChild(mk(c, COLOR_NAME[c]));
  return wrap;
}

async function setColorOn(targets, color) {
  const list = targets.filter(Boolean);
  if (!list.length) return;
  const paths = list.map(entryPath);
  try {
    const r = await post('/api/labels', {paths, color});
    for (const e of list) {
      if (color > 0) {
        const old = entryLabel(e);
        state.labelMap.set(e.name, {color, tags: (old && old.tags) || undefined});
      } else {
        state.labelMap.delete(e.name);
      }
    }
    if (r && r.errors && r.errors.length) {
      toast(r.errors.slice(0, 5).join('\n'), true);
    } else {
      toast(color > 0 ? '已設定色標' : '已移除色標');
    }
    renderVisible(true);
    if (state.lightbox.open) refreshLightboxMeta();
  } catch (err) {
    toast(err.message, true);
  }
}

function isCut(e) {
  return !!(state.clipboard && state.clipboard.mode === 'cut' &&
    state.clipboard.items.some((it) => it.path === entryPath(e)));
}

function selEntries() {
  return state.view.filter((x) => state.selected.has(x.name));
}

function copySel(cut) {
  const list = selEntries();
  if (!list.length) { toast('請先選取項目', true); return; }
  const cl = {mode: cut ? 'cut' : 'copy', items: list.map((e) => ({name: e.name, path: entryPath(e)}))};
  state.clipboard = cl;
  toast((cut ? '已剪下 ' : '已複製 ') + list.length + ' 項');
  renderVisible(true);
  post('/api/copyos', {paths: cl.items.map((i) => i.path), move: cut})
    .then((r) => { if (state.clipboard === cl) cl.seq = r.seq; })
    .catch(() => {});
}

function fmtErrors(errors) {
  const txt = (errors || []).slice(0, 5).join('\n');
  if (/存取被拒|拒絕存取|Access is denied/i.test(txt)) {
    return txt + '\n（目標資料夾可能沒有寫入權限，例如磁碟根目錄或系統資料夾）';
  }
  return txt;
}

async function pasteTo(dest) {
  if (!dest) { toast('請先進入資料夾', true); return; }
  let cl = state.clipboard;
  if (cl && cl.items.length && typeof cl.seq === 'number') {
    try {
      const s = await api('/api/clipseq');
      if (s.seq !== cl.seq) {
        state.clipboard = null;
        cl = null;
      }
    } catch (e) {}
  }
  if (!cl || !cl.items.length) {
    try {
      toast('貼上中…');
      const r = await post('/api/pasteos', {dest});
      if (r.errors && r.errors.length) {
        toast(fmtErrors(r.errors), true);
      } else {
        toast('已貼上 ' + r.copied + ' 項');
      }
      doRefresh();
    } catch (e) {
      toast(e.message, true);
    }
    return;
  }
  try {
    toast(cl.mode === 'cut' ? '移動中…' : '複製中…');
    const r = await post('/api/copy', {paths: cl.items.map((i) => i.path), dest, move: cl.mode === 'cut'});
    if (r.errors && r.errors.length) {
      toast(fmtErrors(r.errors), true);
    } else {
      toast((cl.mode === 'cut' ? '已移動 ' : '已複製 ') + r.copied + ' 項');
    }
    if (cl.mode === 'cut' && (!r.errors || !r.errors.length)) state.clipboard = null;
    doRefresh();
  } catch (e) {
    toast(e.message, true);
  }
}

async function downloadSelected() {
  const list = selEntries();
  if (!list.length) return;
  try {
    toast('下載中…');
    const r = await post('/api/download', {paths: list.map(entryPath)});
    if (r.errors && r.errors.length) {
      toast(r.errors.slice(0, 5).join('\n'), true);
    } else {
      toast('已下載 ' + r.paths.length + ' 項至 ' + r.dir);
    }
  } catch (e) {
    toast(e.message, true);
  }
}

async function walkDirEntry(dirEntry, rel) {
  const out = [];
  const reader = dirEntry.createReader();
  for (;;) {
    const batch = await new Promise((res, rej) => reader.readEntries(res, rej));
    if (!batch.length) break;
    for (const en of batch) {
      if (en.isFile) {
        const f = await new Promise((res, rej) => en.file(res, rej));
        out.push({rel, file: f});
      } else if (en.isDirectory) {
        const sub = await walkDirEntry(en, rel + en.name + '/');
        for (const t of sub) out.push(t);
      }
    }
  }
  return out;
}

async function uploadAll(tasks, dest) {
  if (!tasks.length) { toast('沒有可上傳的檔案', true); return; }
  const fd = new FormData();
  for (const t of tasks) fd.append('up:' + (t.rel || ''), t.file, t.file.name);
  try {
    toast('上傳 ' + tasks.length + ' 個檔案中…');
    const r = await api('/api/upload?p=' + enc(dest), {method: 'POST', body: fd});
    if (r.errors && r.errors.length) {
      toast(r.errors.slice(0, 5).join('\n'), true);
    } else {
      toast('已上傳 ' + r.uploaded + ' 個檔案');
    }
    if (dest === state.path) doRefresh();
  } catch (e) {
    toast(e.message, true);
  }
}

async function handleDrop(dt, dest) {
  const entries = [];
  if (dt.items && dt.items.length) {
    for (const it of dt.items) {
      if (it.kind !== 'file') continue;
      let en = null;
      try { en = it.webkitGetAsEntry ? it.webkitGetAsEntry() : null; } catch (e) { en = null; }
      if (en) entries.push(en);
      else if (it.getAsFile) {
        let f = null;
        try { f = it.getAsFile(); } catch (e) { f = null; }
        if (f) entries.push({isFile: true, name: f.name, _f: f});
      }
    }
  }
  if (!entries.length) {
    for (const f of dt.files || []) entries.push({isFile: true, name: f.name, _f: f});
  }
  const tasks = [];
  for (const en of entries) {
    try {
      if (en.isFile) {
        const f = en._f || await new Promise((res, rej) => en.file(res, rej));
        tasks.push({rel: '', file: f});
      } else if (en.isDirectory) {
        const sub = await walkDirEntry(en, en.name + '/');
        for (const t of sub) tasks.push(t);
      }
    } catch (e) {
      toast(en.name + '：' + e.message, true);
    }
  }
  await uploadAll(tasks, dest);
}

function showCtx(ev, e) {
  const m = $('ctxmenu');
  const items = [];
  if (e) {
    items.push({colors: e});
    items.push({sep:true});
    items.push({label: e.dir ? '開啟' : '以預設程式開啟', act: () => {
      const idx = state.view.indexOf(e);
      if (idx >= 0) openEntry(idx);
    }});
    if (!e.dir) items.push({label:'以其他應用程式開啟…', act: () => {
      post('/api/openwith', {p: entryPath(e)})
        .then((r) => toast('已開啟：' + r.path))
        .catch((err) => toast(err.message, true));
    }});
    if (e.thumb) items.push({label:'用燈箱檢視', act: () => openLightbox(e)});
    if (!isSMB(entryPath(e))) {
      items.push({label:'在檔案總管中顯示', act: () => {
        post('/api/reveal', {p: entryPath(e)}).catch((err) => toast(err.message, true));
      }});
    }
    items.push({sep:true});
    items.push({label:'複製', act: () => copySel(false)});
    items.push({label:'剪下', act: () => copySel(true)});
    if (e.dir) {
      items.push({label:'貼上到此資料夾', act: () => pasteTo(entryPath(e))});
    }
    if (!e.dir) {
      items.push({sep:true});
      items.push({label:'下載到此電腦', act: () => downloadSelected()});
    }
    items.push({sep:true});
    items.push({label:'複製路徑', act: () => {
      const p = entryPath(e);
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(p).then(() => toast('已複製路徑'), () => toast(p));
      } else toast(p);
    }});
    items.push({label:'重新命名…', act: () => renameSelected()});
    items.push({label:'刪除', act: () => deleteSelected()});
  } else {
    items.push({label:'上傳檔案到此資料夾…', act: () => {
      if (!state.path) { toast('請先進入資料夾', true); return; }
      $('fileInput').click();
    }});
    items.push({label:'貼上', act: () => pasteTo(state.path)});
    items.push({sep:true});
    items.push({label:'新增資料夾…', act: () => newFolder()});
    items.push({label:'重新整理', act: () => doRefresh()});
  }
  m.replaceChildren();
  for (const it of items) {
    if (it.colors) {
      m.appendChild(colorRow(it.colors));
      continue;
    }
    if (it.sep) {
      const s = document.createElement('div');
      s.className = 'ctx-sep';
      m.appendChild(s);
      continue;
    }
    const el = document.createElement('div');
    el.className = 'ctx-item';
    el.textContent = it.label;
    el.addEventListener('click', () => { hideCtx(); it.act(); });
    m.appendChild(el);
  }
  m.hidden = false;
  const x = Math.min(ev.clientX, window.innerWidth - m.offsetWidth - 8);
  const y = Math.min(ev.clientY, window.innerHeight - m.offsetHeight - 8);
  m.style.left = Math.max(4, x) + 'px';
  m.style.top = Math.max(4, y) + 'px';
}

function hideCtx() { $('ctxmenu').hidden = true; }

function renameSelected() {
  const names = [...state.selected];
  if (names.length !== 1) { toast('請先選擇單一項目', true); return; }
  const e = state.view.find((x) => x.name === names[0]);
  if (!e) return;
  const p = entryPath(e);
  formModal('重新命名', '新名稱', e.name, async (val) => {
    await post('/api/rename', {p, name: val});
    toast('已重新命名');
    state.selected.clear();
    await doRefresh();
  });
}

function deleteSelected() {
  const list = state.view.filter((x) => state.selected.has(x.name));
  if (!list.length) { toast('請先選擇要刪除的項目', true); return; }
  const paths = list.map(entryPath);
  const sample = list.slice(0, 15).map((x) => escapeHtml(x.name)).join('<br>');
  openModal('刪除確認', `<div>確定要永久刪除 <b>${list.length}</b> 個項目嗎？此操作無法復原。</div>
    <div class="hint" style="max-height:150px;overflow:auto">${sample}${list.length > 15 ? '<br>…' : ''}</div>`, [
    {label:'取消'},
    {label:'刪除', cls:'danger', onClick: () => {
      post('/api/delete', {paths})
        .then(() => { toast('已刪除 ' + list.length + ' 項'); state.selected.clear(); return doRefresh(); })
        .catch((e) => toast(e.message, true));
    }},
  ]);
}

function newFolder() {
  if (state.path === '') { toast('請先進入資料夾', true); return; }
  formModal('新增資料夾', '資料夾名稱', '新資料夾', async (val) => {
    await post('/api/mkdir', {p: state.path, name: val});
    toast('已建立資料夾');
    await doRefresh();
  });
}

async function deepFind() {
  const q = $('search').value.trim();
  if (!q) { toast('請先在上方篩選框輸入關鍵字', true); return; }
  if (state.path === '') { toast('請先進入資料夾', true); return; }
  try {
    toast('搜尋中…');
    const data = await api(`/api/find?p=${enc(state.path)}&q=${enc(q)}`);
    state.view = data.entries || [];
    state.searchMode = true;
    state.searchQuery = q;
    state.selected.clear();
    state.anchor = -1;
    layout();
    updateStatus();
    toast(`找到 ${state.view.length} 項（掃描 ${data.scanned} 項）`);
  } catch (e) {
    toast(e.message, true);
  }
}

function exitSearchMode() {
  state.searchMode = false;
  state.searchQuery = '';
  refresh();
}

function openLightbox(target) {
  const list = state.view.filter((x) => !x.dir && x.thumb);
  if (!list.length) return;
  let idx = list.findIndex((x) => x.name === target.name && entryPath(x) === entryPath(target));
  if (idx < 0) idx = 0;
  state.lightbox = {open:true, list, idx, timer:null, preloadT:null};
  $('lightbox').hidden = false;
  showLightboxImage();
}

function lbMetaText(e) {
  const lbl = entryLabel(e);
  let meta = `${formatBytes(e.size)}　${formatDate(e.mod)}　${state.lightbox.idx + 1}/${state.lightbox.list.length}`;
  if (lbl && lbl.color) meta += `　● ${(lbl.tags && lbl.tags.join('、')) || COLOR_NAME[lbl.color] || ''}`;
  return meta;
}

function refreshLightboxMeta() {
  const lb = state.lightbox;
  const e = lb.list[lb.idx];
  if (!e || $('lightbox').hidden) return;
  $('lbMeta').textContent = lbMetaText(e);
  const cur = (entryLabel(e) || {}).color || 0;
  for (const b of document.querySelectorAll('#lbColors .lbsw')) {
    b.classList.toggle('cur', parseInt(b.dataset.c, 10) === cur);
  }
}

function showLightboxImage() {
  const lb = state.lightbox;
  const e = lb.list[lb.idx];
  if (!e) { closeLightbox(); return; }
  if (lb.preloadT) { clearTimeout(lb.preloadT); lb.preloadT = null; }
  $('lbName').textContent = e.name;
  refreshLightboxMeta();
  const fast = $('lbFast');
  const tUrl = e.thumb ? thumbURL(e) : '';
  if (tUrl) {
    fast.src = tUrl;
    fast.style.display = 'block';
  } else {
    fast.style.display = 'none';
  }
  const img = $('lbImg');
  img.classList.remove('zoom', 'ready');
  img.src = '/api/file?p=' + enc(entryPath(e));
  const stage = $('lbStage');
  stage.scrollTop = 0;
  stage.scrollLeft = 0;
}

function preloadNeighbor(d) {
  const lb = state.lightbox;
  const n = lb.idx + d;
  if (n >= 0 && n < lb.list.length) {
    const im = new Image();
    im.src = '/api/file?p=' + enc(entryPath(lb.list[n]));
  }
}

function lbStep(d) {
  const lb = state.lightbox;
  const n = lb.idx + d;
  if (n < 0 || n >= lb.list.length) {
    if (lb.timer) stopSlide();
    return;
  }
  lb.idx = n;
  showLightboxImage();
}

function toggleSlide() {
  const lb = state.lightbox;
  if (lb.timer) { stopSlide(); return; }
  $('lbSlide').textContent = '⏸ 暫停';
  lb.timer = setInterval(() => lbStep(1), 3500);
  lbStep(1);
}

function stopSlide() {
  const lb = state.lightbox;
  if (lb.timer) {
    clearInterval(lb.timer);
    lb.timer = null;
  }
  $('lbSlide').textContent = '▶ 幻燈片';
}

function closeLightbox() {
  stopSlide();
  $('lightbox').hidden = true;
  $('lbImg').removeAttribute('src');
  $('lbFast').removeAttribute('src');
  state.lightbox = {open:false, list:[], idx:0, timer:null, preloadT:null};
}

function lightboxKey(ev) {
  if (ev.key === 'Escape') closeLightbox();
  else if (ev.key === 'ArrowRight') { stopSlide(); lbStep(1); }
  else if (ev.key === 'ArrowLeft') { stopSlide(); lbStep(-1); }
  else if (ev.key === ' ') { ev.preventDefault(); toggleSlide(); }
  else if (ev.key === 'Home') { state.lightbox.idx = 0; showLightboxImage(); }
  else if (ev.key === 'End') { state.lightbox.idx = state.lightbox.list.length - 1; showLightboxImage(); }
  else if (/^[0-7]$/.test(ev.key) && !ev.ctrlKey && !ev.altKey && !ev.metaKey) {
    const e = state.lightbox.list[state.lightbox.idx];
    if (e) setColorOn([e], parseInt(ev.key, 10));
  }
}

let lbDrag = null;
function wireLightbox() {
  $('lbPrev').addEventListener('click', (ev) => { ev.stopPropagation(); stopSlide(); lbStep(-1); });
  $('lbNext').addEventListener('click', (ev) => { ev.stopPropagation(); stopSlide(); lbStep(1); });
  $('lbClose').addEventListener('click', closeLightbox);
  $('lbSlide').addEventListener('click', toggleSlide);
  for (const b of document.querySelectorAll('#lbColors .lbsw')) {
    b.addEventListener('click', (ev) => {
      ev.stopPropagation();
      b.blur();
      const e = state.lightbox.list[state.lightbox.idx];
      if (e) setColorOn([e], parseInt(b.dataset.c, 10));
    });
  }
  $('lbImg').addEventListener('load', () => {
    $('lbImg').classList.add('ready');
    $('lbFast').style.display = 'none';
    const lb = state.lightbox;
    if (!lb.open) return;
    if (lb.preloadT) clearTimeout(lb.preloadT);
    lb.preloadT = setTimeout(() => {
      lb.preloadT = null;
      preloadNeighbor(1);
      preloadNeighbor(-1);
    }, 400);
  });
  $('lbImg').addEventListener('error', () => {
    const lb = state.lightbox;
    if (lb.open) $('lbMeta').textContent += '　（無法載入原圖）';
  });
  $('lbStage').addEventListener('click', (ev) => {
    if (ev.target === $('lbImg') || ev.target === $('lbStage')) {
      $('lbImg').classList.toggle('zoom');
    }
  });
  $('lbStage').addEventListener('mousedown', (ev) => {
    if (!$('lbImg').classList.contains('zoom')) return;
    lbDrag = {x: ev.clientX, y: ev.clientY, sl: $('lbStage').scrollLeft, st: $('lbStage').scrollTop};
    ev.preventDefault();
  });
  window.addEventListener('mousemove', (ev) => {
    if (!lbDrag) return;
    const stage = $('lbStage');
    stage.scrollLeft = lbDrag.sl - (ev.clientX - lbDrag.x);
    stage.scrollTop = lbDrag.st - (ev.clientY - lbDrag.y);
  });
  window.addEventListener('mouseup', () => { lbDrag = null; });
  $('lightbox').addEventListener('click', (ev) => {
    if (ev.target === $('lightbox')) closeLightbox();
  });
}

function wireToolbar() {
  $('back').addEventListener('click', goBack);
  $('fwd').addEventListener('click', goFwd);
  $('up').addEventListener('click', goUp);
  $('refresh').addEventListener('click', () => doRefresh());
  $('addr').addEventListener('keydown', (ev) => {
    if (ev.key !== 'Enter') return;
    let v = $('addr').value.trim();
    if (!v) { navigate('', {push:true}); return; }
    if (!isSMB(v)) v = v.replace(/\//g, '\\');
    if (/^[a-zA-Z]:$/.test(v)) v += '\\';
    navigate(v, {push:true});
  });
  $('search').addEventListener('input', () => {
    state.search = $('search').value;
    if (state.searchMode) state.searchMode = false;
    refresh();
  });
  $('findBtn').addEventListener('click', deepFind);
  $('uploadBtn').addEventListener('click', () => {
    if (!state.path) { toast('請先進入資料夾', true); return; }
    $('fileInput').click();
  });
  $('fileInput').addEventListener('change', () => {
    const files = [...$('fileInput').files];
    $('fileInput').value = '';
    if (!files.length || !state.path) return;
    uploadAll(files.map((f) => ({rel: '', file: f})), state.path);
  });
  $('sort').addEventListener('change', () => {
    state.prefs.sortKey = $('sort').value;
    refresh();
    savePrefs();
  });
  $('sortDir').addEventListener('click', () => {
    state.prefs.sortDir = state.prefs.sortDir === 'asc' ? 'desc' : 'asc';
    $('sortDir').textContent = state.prefs.sortDir === 'asc' ? '↑' : '↓';
    refresh();
    savePrefs();
  });
  $('tileSize').addEventListener('input', () => {
    state.prefs.tileSize = parseInt($('tileSize').value, 10) || 168;
    layout();
    savePrefs();
  });
  $('hoverDelay').addEventListener('input', () => {
    const sec = parseFloat($('hoverDelay').value);
    state.prefs.hoverDelay = Math.round((sec > 0 ? sec : 1) * 1000);
    savePrefs();
  });
  $('showLabels').addEventListener('change', () => {
    state.prefs.showLabels = $('showLabels').checked;
    if (state.prefs.showLabels && state.labelMap.size === 0 && state.path !== '') refreshLabels(state.path);
    renderVisible(true);
    savePrefs();
  });
  $('showHidden').addEventListener('change', () => {
    state.prefs.showHidden = $('showHidden').checked;
    refresh();
    savePrefs();
  });
  $('addConnBtn').addEventListener('click', () => connModal(null));
  for (const chip of document.querySelectorAll('.chip')) {
    chip.addEventListener('click', () => {
      const c = parseInt(chip.dataset.color, 10);
      if (c === 0) {
        state.labelFilter.clear();
      } else if (state.labelFilter.has(c)) {
        state.labelFilter.delete(c);
      } else {
        state.labelFilter.add(c);
      }
      for (const ch of document.querySelectorAll('.chip')) {
        const cc = parseInt(ch.dataset.color, 10);
        ch.classList.toggle('on', cc === 0 ? state.labelFilter.size === 0 : state.labelFilter.has(cc));
      }
      refresh();
    });
  }
  const chipAll = document.querySelector('.chip[data-color="0"]');
  if (chipAll) chipAll.classList.add('on');
}

let bandSel = null;

function drawBand(ev) {
  const wrap = $('gridwrap');
  const wr = wrap.getBoundingClientRect();
  const x1 = Math.max(0, Math.min(ev.clientX - wr.left + wrap.scrollLeft, wrap.scrollWidth));
  const y1 = Math.max(0, Math.min(ev.clientY - wr.top + wrap.scrollTop, wrap.scrollHeight));
  const x = Math.min(bandSel.x0, x1);
  const y = Math.min(bandSel.y0, y1);
  const w = Math.abs(x1 - bandSel.x0);
  const h = Math.abs(y1 - bandSel.y0);
  const el = $('band');
  el.hidden = false;
  el.style.left = x + 'px';
  el.style.top = y + 'px';
  el.style.width = w + 'px';
  el.style.height = h + 'px';
  const tw = state.tileW, th = state.tileH, gap = 8;
  state.selected = new Set(bandSel.base);
  const rows = Math.ceil(state.view.length / state.cols);
  const r0 = Math.max(0, Math.floor((y - gap) / th));
  const r1 = Math.min(rows - 1, Math.floor((y + h - gap) / th));
  for (let r = r0; r <= r1; r++) {
    for (let c = 0; c < state.cols; c++) {
      const i = r * state.cols + c;
      if (i >= state.view.length) break;
      const tx = gap + c * (tw + gap), ty = gap + r * th;
      if (tx < x + w && tx + tw > x && ty < y + h && ty + th > y) {
        state.selected.add(state.view[i].name);
      }
    }
  }
  renderVisible(true);
  updateStatus();
}

function wireGlobal() {
  document.addEventListener('click', (ev) => {
    if (!ev.target.closest('#ctxmenu')) hideCtx();
  });
  document.addEventListener('contextmenu', (ev) => {
    if (ev.target.closest('.tile')) return;
    if (!ev.target.closest('#gridwrap')) return;
    ev.preventDefault();
    showCtx(ev, null);
  });
  const wrapEl = $('gridwrap');
  wrapEl.addEventListener('mousedown', (ev) => {
    if (ev.button !== 0) return;
    if (ev.target.closest && ev.target.closest('.tile')) return;
    hideHoverPreview();
    const wr = wrapEl.getBoundingClientRect();
    if (ev.clientX - wr.left > wrapEl.clientWidth || ev.clientY - wr.top > wrapEl.clientHeight) return;
    hideCtx();
    const ctrl = ev.ctrlKey || ev.metaKey;
    bandSel = {
      x0: ev.clientX - wr.left + wrapEl.scrollLeft,
      y0: ev.clientY - wr.top + wrapEl.scrollTop,
      base: ctrl ? new Set(state.selected) : new Set(),
    };
    if (!ctrl) {
      state.selected.clear();
      renderVisible(true);
      updateStatus();
    }
    ev.preventDefault();
    drawBand(ev);
  });
  window.addEventListener('mousemove', (ev) => { if (bandSel) drawBand(ev); });
  window.addEventListener('mouseup', () => {
    if (!bandSel) return;
    bandSel = null;
    $('band').hidden = true;
  });
  let scrollRaf = 0;
  $('gridwrap').addEventListener('scroll', () => {
    hideHoverPreview();
    if (scrollRaf) return;
    scrollRaf = requestAnimationFrame(() => {
      scrollRaf = 0;
      renderVisible(false);
    });
  });
  window.addEventListener('blur', () => hideHoverPreview());
  new ResizeObserver(() => layout()).observe($('gridwrap'));
  const wrap = $('gridwrap');
  let dragDepth = 0;
  wrap.addEventListener('dragenter', (ev) => {
    if (!ev.dataTransfer || ![...ev.dataTransfer.types].includes('Files')) return;
    ev.preventDefault();
    dragDepth++;
    wrap.classList.add('dragover');
  });
  wrap.addEventListener('dragover', (ev) => {
    if (!ev.dataTransfer || ![...ev.dataTransfer.types].includes('Files')) return;
    ev.preventDefault();
    ev.dataTransfer.dropEffect = 'copy';
  });
  wrap.addEventListener('dragleave', () => {
    dragDepth--;
    if (dragDepth <= 0) {
      dragDepth = 0;
      wrap.classList.remove('dragover');
    }
  });
  wrap.addEventListener('drop', (ev) => {
    if (!ev.dataTransfer || ![...ev.dataTransfer.types].includes('Files')) return;
    ev.preventDefault();
    dragDepth = 0;
    wrap.classList.remove('dragover');
    let dest = state.path;
    const tile = ev.target.closest ? ev.target.closest('.tile') : null;
    if (tile) {
      const i = parseInt(tile.dataset.i || '-1', 10);
      const e = state.view[i];
      if (e && e.dir) dest = entryPath(e);
    }
    if (!dest) { toast('請先進入資料夾再拖入檔案', true); return; }
    handleDrop(ev.dataTransfer, dest);
  });
  document.addEventListener('keydown', (ev) => {
    if (!$('lightbox').hidden) { lightboxKey(ev); return; }
    if (!$('modal').hidden) {
      if (ev.key === 'Escape') closeModal();
      return;
    }
    const tag = (ev.target.tagName || '').toLowerCase();
    const typing = tag === 'input' || tag === 'select' || tag === 'textarea';
    if (ev.altKey && ev.key === 'ArrowLeft') { ev.preventDefault(); goBack(); return; }
    if (ev.altKey && ev.key === 'ArrowRight') { ev.preventDefault(); goFwd(); return; }
    if (ev.altKey && ev.key === 'ArrowUp') { ev.preventDefault(); goUp(); return; }
    if (ev.ctrlKey && (ev.key === 'f' || ev.key === 'F')) { ev.preventDefault(); $('search').focus(); return; }
    if (ev.key === 'F5' || (ev.ctrlKey && (ev.key === 'r' || ev.key === 'R'))) { ev.preventDefault(); doRefresh(); return; }
    if (typing) {
      if (ev.key === 'Escape') ev.target.blur();
      return;
    }
    if (ev.key === 'Escape') {
      if (state.searchMode) { exitSearchMode(); return; }
      state.selected.clear();
      hideCtx();
      renderVisible(true);
      updateStatus();
      return;
    }
    if (ev.ctrlKey && (ev.key === 'a' || ev.key === 'A')) {
      ev.preventDefault();
      for (const x of state.view) state.selected.add(x.name);
      renderVisible(true);
      updateStatus();
      return;
    }
    if (ev.ctrlKey && (ev.key === 'c' || ev.key === 'C')) { ev.preventDefault(); copySel(false); return; }
    if (ev.ctrlKey && (ev.key === 'x' || ev.key === 'X')) { ev.preventDefault(); copySel(true); return; }
    if (ev.ctrlKey && (ev.key === 'v' || ev.key === 'V')) { ev.preventDefault(); pasteTo(state.path); return; }
    if (['ArrowDown','ArrowUp','ArrowLeft','ArrowRight'].includes(ev.key)) {
      ev.preventDefault();
      moveSel(ev, ev.key);
      return;
    }
    if (ev.key === 'Enter') {
      if (state.anchor >= 0) openEntry(state.anchor);
      return;
    }
    if (ev.key === 'F2') { renameSelected(); return; }
    if (ev.key === 'Delete') { deleteSelected(); return; }
    if (ev.key === 'Backspace') { ev.preventDefault(); goUp(); return; }
  });
}

async function init() {
  try {
    const cfg = await api('/api/config');
    if (cfg.prefs) Object.assign(state.prefs, cfg.prefs);
    applyPrefsToUI();
    state.roots = await api('/api/roots');
    renderSidebar();
    const drives = state.roots.drives || [];
    const start = cfg.lastPath || state.roots.default || (drives[0] && drives[0].path) || 'C:\\';
    await navigate(start);
  } catch (e) {
    toast('初始化失敗：' + e.message, true);
  }
}

wireToolbar();
wireLightbox();
wireGlobal();
init();

})();
