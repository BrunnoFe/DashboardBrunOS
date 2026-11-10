// Widget de Notas (J3): texto livre ou lista de tarefas, escrito direto na tela inicial.
// O texto fica no servidor (/api/notes/{id do widget}); a gravação espera uma pausa na digitação.
// Quando outro aparelho muda a nota, chega um aviso pela conexão ao vivo e esta tela busca de novo
// (se ninguém estiver digitando nela agora). Vale a última gravação.
import { api } from './api.js';
import { $, $$, esc, svg, toastError } from './ui.js';

const MAX = 20000;
const notes = new Map(); // id → { text, at, loaded, timer, saving, stale }

const parseList = (text) => (text ? text.split('\n') : ['']).map((ln) => {
  const m = /^\[( |x|X)\] ?(.*)$/.exec(ln);
  return m ? { done: m[1] !== ' ', text: m[2] } : { done: false, text: ln };
});
const joinList = (items) => items.map((x) => `[${x.done ? 'x' : ' '}] ${x.text}`).join('\n');

function get(id) {
  let n = notes.get(id);
  if (!n) { n = { text: '', at: 0, loaded: false }; notes.set(id, n); }
  return n;
}

async function load(id) {
  const n = get(id);
  if (n.loading) { n.again = true; return; } // já está buscando: busca de novo ao terminar (a resposta pode ser velha)
  n.loading = true;
  try { const r = await api.notes.get(id); if (!n.timer && !n.saving) { n.text = r.text || ''; n.at = r.at || 0; } n.loaded = true; }
  catch (e) { if (e.status !== 423) toastError(e, 'Não deu para abrir a nota'); }
  finally { n.loading = false; }
  if (n.again) { n.again = false; load(id); return; }
  $$(`.wf-notes[data-note="${CSS.escape(id)}"]`).forEach((el) => { if (!el.contains(document.activeElement)) draw(el); });
}

/** A conexão ao vivo (re)conectou: busca de novo as notas na tela (avisos podem ter se perdido). */
export function refreshNotes() {
  for (const [id, n] of notes) {
    if (!n.loaded || n.timer || n.saving) continue;
    const el = $(`.wf-notes[data-note="${CSS.escape(id)}"]`);
    if (el && el.contains(document.activeElement)) { n.stale = true; continue; }
    if (el) load(id);
  }
}

function status(el, txt) { const s = $('.wn-st', el); if (s) s.textContent = txt; }

function schedule(id, el) {
  const n = get(id);
  clearTimeout(n.timer);
  status(el, '');
  n.timer = setTimeout(() => save(id, el), 900);
}
async function save(id, el) {
  const n = get(id);
  clearTimeout(n.timer); n.timer = 0;
  if (n.text.length > MAX) { status(el, 'grande demais'); return; }
  n.saving = true; status(el, 'salvando…');
  try { const r = await api.notes.put(id, n.text); n.at = r.at; status(el, 'salvo'); setTimeout(() => status(el, ''), 1500); }
  catch (e) { status(el, 'não salvou'); toastError(e, 'Não deu para salvar a nota'); }
  finally { n.saving = false; }
}

function draw(el) {
  const id = el.dataset.note, n = get(id), mode = el.dataset.mode;
  if (!n.loaded) { el.innerHTML = '<span class="wn-wait">carregando…</span>'; return; }
  if (mode === 'list') {
    const items = parseList(n.text);
    el.innerHTML = `<span class="wn-items">${items.map((x, i) => `<span class="wn-item ${x.done ? 'done' : ''}" data-i="${i}"><button type="button" class="wn-box" data-box="${i}" aria-label="${x.done ? 'Desmarcar' : 'Marcar'}">${x.done ? svg('check') : ''}</button><input class="wn-in" data-in="${i}" value="${esc(x.text)}" placeholder="${i === items.length - 1 && !x.text ? 'Novo item…' : ''}" maxlength="500" aria-label="Item ${i + 1}"></span>`).join('')}</span><span class="wn-st"></span>`;
  } else {
    el.innerHTML = `<textarea class="wn-area" placeholder="Escreva aqui…" maxlength="${MAX}" aria-label="Nota">${esc(n.text)}</textarea><span class="wn-st"></span>`;
  }
}

function wire(el) {
  const id = el.dataset.note;
  const items = () => parseList(get(id).text);
  const setItems = (list, focus) => {
    get(id).text = joinList(list);
    draw(el); schedule(id, el);
    if (focus != null) { const inp = $(`[data-in="${focus.i}"]`, el); if (inp) { inp.focus(); const p = focus.at ?? (focus.end ? inp.value.length : 0); inp.setSelectionRange(p, p); } }
  };
  el.addEventListener('input', (e) => {
    const t = e.target, n = get(id);
    if (t.matches('.wn-area')) { n.text = t.value; schedule(id, el); }
    else if (t.matches('.wn-in')) { const list = items(); list[+t.dataset.in].text = t.value.replace(/\n/g, ' '); n.text = joinList(list); schedule(id, el); }
  });
  el.addEventListener('click', (e) => {
    e.stopPropagation(); // não abre o clique do widget
    const b = e.target.closest('[data-box]'); if (!b) return;
    const list = items(), i = +b.dataset.box;
    list[i].done = !list[i].done;
    setItems(list);
  });
  el.addEventListener('keydown', (e) => {
    const t = e.target; if (!t.matches('.wn-in')) return;
    const i = +t.dataset.in, list = items();
    if (e.key === 'Enter') { e.preventDefault(); const at = t.selectionStart; const rest = list[i].text.slice(at); list[i].text = list[i].text.slice(0, at); list.splice(i + 1, 0, { done: false, text: rest }); setItems(list, { i: i + 1 }); }
    else if (e.key === 'Backspace' && t.selectionStart === 0 && t.selectionEnd === 0 && i > 0) { e.preventDefault(); const prev = list[i - 1].text; list[i - 1].text = prev + list[i].text; list.splice(i, 1); setItems(list, { i: i - 1, at: prev.length }); }
    else if (e.key === 'ArrowDown' && i < list.length - 1) { e.preventDefault(); $(`[data-in="${i + 1}"]`, el)?.focus(); }
    else if (e.key === 'ArrowUp' && i > 0) { e.preventDefault(); $(`[data-in="${i - 1}"]`, el)?.focus(); }
  });
  // saiu da nota: grava na hora e, se outro aparelho mudou enquanto digitava, busca a nova
  el.addEventListener('focusout', () => setTimeout(() => {
    if (el.contains(document.activeElement)) return;
    const n = get(id);
    if (n.timer) save(id, el);
    else if (n.stale) { n.stale = false; load(id); }
  }, 0));
  // botão direito e arrastar ficam com o texto, não com o widget
  el.addEventListener('contextmenu', (e) => e.stopPropagation());
  el.addEventListener('touchstart', (e) => e.stopPropagation(), { passive: true });
}

/** Monta as notas que apareceram na tela (depois de desenhar a tela inicial). */
export function mountNotes(root = document) {
  for (const el of $$('.wf-notes[data-note]:not([data-ready])', root)) {
    el.dataset.ready = '1';
    wire(el);
    const n = get(el.dataset.note);
    draw(el);
    if (!n.loaded) load(el.dataset.note);
  }
}

/** Onde estava o cursor numa nota (para não perder ao redesenhar a tela inicial). */
export function noteFocus() {
  const a = document.activeElement, el = a?.closest?.('.wf-notes[data-note]');
  if (!el) return null;
  return { id: el.dataset.note, sel: a.dataset.in ?? 'area', start: a.selectionStart, end: a.selectionEnd };
}
export function restoreNoteFocus(f) {
  if (!f) return;
  const el = $(`.wf-notes[data-note="${CSS.escape(f.id)}"]`); if (!el) return;
  const inp = f.sel === 'area' ? $('.wn-area', el) : $(`[data-in="${f.sel}"]`, el);
  if (!inp) return;
  inp.focus();
  try { inp.setSelectionRange(f.start, f.end); } catch { /* campo mudou */ }
}

/** Aviso da conexão ao vivo: a nota mudou (em outro aparelho ou aba). */
export function noteChanged({ id, at }) {
  const n = notes.get(id); if (!n) return;
  if ((at && at === n.at) || n.saving || n.timer) return; // foi esta tela que gravou (ou vai gravar agora)
  const el = $(`.wf-notes[data-note="${CSS.escape(id)}"]`);
  if (el && el.contains(document.activeElement)) { n.stale = true; return; } // digitando: busca ao sair
  load(id);
}

/** Widget de Notas removido: apaga o texto do servidor. */
export function deleteNote(id) {
  notes.delete(id);
  api.notes.del(id).catch(() => { /* sem servidor: fica para a próxima */ });
}
