import './style.css';
import { ParseFile, MergeFiles, ParseText, BuildHTML, SaveHTML, OpenInBrowser, LoadFromIBM, SaveRawLog } from './wailsjs/go/main/App.js';
import { EventsOn } from './wailsjs/runtime/runtime.js';

// ── State ──────────────────────────────────────────────────────────────────
let activeTab     = 'file';
let loadedRows    = '';
let loadedCount   = 0;
let isMerge       = false;
let generatedHTML = '';
let ibmRawContent = '';
let ibmLogName    = 'maximo.log';
let sortDir       = [];

// ── IBM progress events ────────────────────────────────────────────────────
EventsOn('ibm:progress', data => {
  document.getElementById('ibmFill').style.width = data.pct + '%';
  document.getElementById('ibmMsg').textContent  = data.message;
});

// ── Tab switching ──────────────────────────────────────────────────────────
window.switchTab = function(tab) {
  activeTab = tab;
  document.querySelectorAll('.tab').forEach((t, i) => {
    const ids = ['file','merge','ibm','paste'];
    t.classList.toggle('active', ids[i] === tab);
  });
  ['file','merge','ibm','paste'].forEach(id => {
    document.getElementById('tab-' + id).classList.toggle('active', id === tab);
  });
};

// ── Load single file ───────────────────────────────────────────────────────
window.doLoadFile = async function() {
  setStatus('Opening file dialog...', '');
  const result = await ParseFile();
  if (!result || result.error) {
    setStatus(result?.error ? '⚠ ' + result.error : 'Cancelled.', result?.error ? 'error' : '');
    return;
  }
  if (result.count === 0) { setStatus('⚠ No SQL queries found.', 'warn'); return; }
  loadedRows  = result.rows;
  loadedCount = result.count;
  isMerge     = false;
  showPreview('filePreview', result.count + ' queries found.');
  setStatus('✔ ' + result.count.toLocaleString() + ' queries found. Click Generate HTML.', 'success');
};

// ── Merge files ────────────────────────────────────────────────────────────
window.doMergeFiles = async function() {
  setStatus('Opening multi-select dialog...', '');
  const result = await MergeFiles();
  if (!result || result.error) {
    setStatus(result?.error ? '⚠ ' + result.error : 'Cancelled.', result?.error ? 'error' : '');
    return;
  }
  if (result.count === 0) { setStatus('⚠ No SQL queries found in selected files.', 'warn'); return; }
  loadedRows  = result.rows;
  loadedCount = result.count;
  isMerge     = true;
  showPreview('mergePreview', result.count + ' queries found across merged files.');
  setStatus('✔ Merged — ' + result.count.toLocaleString() + ' queries found. Click Generate HTML.', 'success');
};

// ── IBM Editor ─────────────────────────────────────────────────────────────
window.doLoadIBM = async function() {
  const ibmURL    = document.getElementById('ibmURL').value.trim();
  const ibmCookie = document.getElementById('ibmCookie').value.trim();
  if (!ibmURL || !ibmCookie) { setStatus('⚠ URL and Cookie are required.', 'error'); return; }

  const hashPart = ibmURL.split('#')[1] || '';
  const segs = hashPart.split('/').filter(Boolean);
  ibmLogName = segs.length ? segs[segs.length - 1] : 'maximo.log';

  document.getElementById('ibmProgress').style.display = 'block';
  document.getElementById('ibmFill').style.width = '0%';
  document.getElementById('ibmMsg').textContent  = 'Connecting...';
  document.getElementById('btnIBMLoad').disabled = true;
  setStatus('Connecting to IBM Editor...', '');

  const result = await LoadFromIBM(ibmURL, ibmCookie);
  document.getElementById('btnIBMLoad').disabled = false;

  if (!result || result.error) {
    document.getElementById('ibmProgress').style.display = 'none';
    setStatus('⚠ ' + (result?.error || 'Unknown error'), 'error');
    return;
  }
  if (result.count === 0) {
    document.getElementById('ibmProgress').style.display = 'none';
    setStatus('⚠ No SQL queries found.', 'warn');
    return;
  }

  loadedRows    = result.rows;
  loadedCount   = result.count;
  isMerge       = false;
  ibmRawContent = result.rawContent || '';
  ibmLogName    = result.logName    || 'maximo.log';
  document.getElementById('btnDownloadLog').style.display = 'inline-block';
  document.getElementById('ibmFill').style.width = '100%';
  document.getElementById('ibmMsg').textContent  = 'Done!';
  setStatus('✔ IBM Editor: ' + result.count.toLocaleString() + ' queries found. Click Generate HTML.', 'success');
};

window.doDownloadLog = async function() {
  if (!ibmRawContent) { setStatus('⚠ No raw log content available.', 'warn'); return; }
  const path = await SaveRawLog(ibmRawContent, ibmLogName);
  if (path && !path.startsWith('error:')) setStatus('✔ Log saved: ' + path, 'success');
  else if (path && path.startsWith('error:')) setStatus('⚠ ' + path, 'error');
};

// ── Generate HTML ──────────────────────────────────────────────────────────
window.generateHTML = async function() {
  let rows  = loadedRows;
  let count = loadedCount;
  let merge = isMerge;

  if (activeTab === 'paste') {
    const text = document.getElementById('pasteArea').value;
    if (!text.trim()) { setStatus('⚠ Paste log content first.', 'error'); return; }
    setStatus('Parsing...', '');
    const result = await ParseText(text);
    if (!result || result.count === 0) { setStatus('⚠ No SQL queries found.', 'warn'); return; }
    rows  = result.rows;
    count = result.count;
    merge = false;
  }

  if (!rows || count === 0) { setStatus('⚠ Load or paste log content first.', 'error'); return; }

  // compute stats from rows HTML
  let totalTime = 0, slowCount = 0, maxTime = 0;
  const tdRe = /<td[^>]*>(\d+)<\/td>/g;
  let m;
  while ((m = tdRe.exec(rows)) !== null) {
    const t = parseInt(m[1], 10);
    totalTime += t;
    if (t >= 5000) slowCount++;
    if (t > maxTime) maxTime = t;
  }
  const avgTime = count > 0 ? Math.round(totalTime / count) : 0;

  setStatus('Building HTML...', '');
  generatedHTML = await BuildHTML(rows, count, slowCount, avgTime, maxTime, merge);

  renderTable(rows, merge);

  document.getElementById('stats').innerHTML =
    '<span>Total: <b>' + count.toLocaleString() + '</b></span>' +
    '<span class="' + (slowCount > 0 ? 'slow-stat' : '') + '">Slow (>=5s): <b>' + slowCount.toLocaleString() + '</b></span>' +
    '<span>Avg: <b>' + avgTime.toLocaleString() + ' ms</b></span>' +
    '<span>Max: <b>' + maxTime.toLocaleString() + ' ms</b></span>';

  document.getElementById('resultSection').style.display = 'block';
  document.getElementById('btnSave').disabled    = false;
  document.getElementById('btnOpenNew').disabled = false;
  setStatus('✔ ' + count.toLocaleString() + ' queries. Use Save As to export.', 'success');
  document.getElementById('resultSection').scrollIntoView({ behavior: 'smooth' });
};

// ── Render preview table ───────────────────────────────────────────────────
function renderTable(rows, merge) {
  const thead = document.getElementById('resultHead');
  if (merge) {
    thead.innerHTML = '<tr><th onclick="sortCol(0)" style="width:100px">Time (ms)</th><th onclick="sortCol(1)">File</th><th onclick="sortCol(2)">SQL</th></tr>';
    sortDir = [false, false, false];
  } else {
    thead.innerHTML = '<tr><th onclick="sortCol(0)" style="width:110px">Time (ms)</th><th onclick="sortCol(1)">SQL</th></tr>';
    sortDir = [false, false];
  }
  const tmp = document.createElement('tbody');
  tmp.innerHTML = rows;
  const tbody = document.getElementById('resultBody');
  tbody.innerHTML = '';
  tbody.appendChild(tmp);
}

// ── Sort ───────────────────────────────────────────────────────────────────
window.sortCol = function(col) {
  const tbody   = document.getElementById('resultBody');
  const rowsArr = Array.from(tbody.querySelectorAll('tr'));
  const dir     = sortDir[col];
  rowsArr.sort((a, b) => {
    if (col === 0) {
      return dir
        ? (parseInt(a.cells[0].textContent)||0) - (parseInt(b.cells[0].textContent)||0)
        : (parseInt(b.cells[0].textContent)||0) - (parseInt(a.cells[0].textContent)||0);
    }
    return dir
      ? a.cells[col].textContent.localeCompare(b.cells[col].textContent)
      : b.cells[col].textContent.localeCompare(a.cells[col].textContent);
  });
  sortDir[col] = !dir;
  rowsArr.forEach(r => tbody.appendChild(r));
  document.querySelectorAll('#resultTable th').forEach((th, i) => {
    th.classList.remove('sorted-asc', 'sorted-desc');
    if (i === col) th.classList.add(sortDir[col] ? 'sorted-desc' : 'sorted-asc');
  });
};

// ── Save HTML ──────────────────────────────────────────────────────────────
window.doSaveHTML = async function() {
  if (!generatedHTML) return;
  const path = await SaveHTML(generatedHTML, 'queries.html');
  if (path && !path.startsWith('error:')) setStatus('✔ Saved: ' + path, 'success');
  else if (path && path.startsWith('error:')) setStatus('⚠ ' + path, 'error');
};

// ── Open in default browser (temp file) ───────────────────────────────────
window.doOpenBrowser = async function() {
  if (!generatedHTML) return;
  await OpenInBrowser(generatedHTML);
};

// ── Clear ──────────────────────────────────────────────────────────────────
window.clearAll = function() {
  loadedRows = ''; loadedCount = 0; isMerge = false;
  generatedHTML = ''; ibmRawContent = '';
  document.getElementById('filePreview').style.display   = 'none';
  document.getElementById('mergePreview').style.display  = 'none';
  document.getElementById('pasteArea').value  = '';
  document.getElementById('ibmURL').value     = '';
  document.getElementById('ibmProgress').style.display   = 'none';
  document.getElementById('btnDownloadLog').style.display = 'none';
  document.getElementById('resultSection').style.display = 'none';
  document.getElementById('resultBody').innerHTML = '';
  document.getElementById('btnSave').disabled    = true;
  document.getElementById('btnOpenNew').disabled = true;
  setStatus('Cleared.', '');
};

// ── Helpers ────────────────────────────────────────────────────────────────
function showPreview(id, msg) {
  const el = document.getElementById(id);
  el.textContent   = msg;
  el.style.display = 'block';
}

function setStatus(msg, type) {
  const el    = document.getElementById('status');
  el.textContent = msg;
  el.className   = 'status' + (type ? ' ' + type : '');
}
