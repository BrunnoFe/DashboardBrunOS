// Editor de texto do gerenciador de arquivos (CodeMirror 6, carregado só quando abre).
// Salva com Ctrl+S; se o arquivo mudou no servidor desde que abriu, avisa em vez de sobrescrever.
import { api } from './api.js';
import { $, esc, fmtBytes, svg, toast, toastError, openDialog, confirmDialog } from './ui.js';

let cmPromise = null;
const loadCM = () => (cmPromise ||= import('../vendor/codemirror/codemirror.mjs'));

export async function openEditor({ root, path, name, onSaved }) {
  let ed = null, file = null, dirty = false, saving = false;
  const d = openDialog(`<div class="ed">
      <div class="ed-head">${svg('edit')}<b class="ed-name">${esc(name)}</b><span class="ed-state"></span><span class="f-spacer"></span>
        <button class="pill-btn" data-ed="search" title="Procurar e substituir (Ctrl+F)">${svg('search')}<span class="hide-sm">Procurar</span></button>
        <button class="pill-btn primary" data-ed="save" disabled title="Salvar (Ctrl+S)">${svg('check')}Salvar</button>
        <button class="icon-btn" data-ed="close" aria-label="Fechar">${svg('x')}</button></div>
      <div class="ed-host"><div class="f-loading"><i></i><i></i><i></i></div></div>
    </div>`, {
    cls: 'ed-dialog', width: 1100, closeBtn: false, backdropClose: false,
    beforeClose: async () => !dirty || confirmDialog('Sair sem salvar?', `As alterações em “${name}” serão perdidas.`, 'Sair sem salvar', { danger: true }),
    onClose: () => ed?.destroy(),
  });
  const state = (txt, cls = '') => { const el = $('.ed-state', d.el); el.textContent = txt; el.className = `ed-state ${cls}`; };
  const setDirty = (v) => { dirty = v; $('[data-ed="save"]', d.el).disabled = !v || file?.readOnly; state(v ? 'alterado' : file?.readOnly ? 'somente leitura' : 'salvo', v ? 'dirty' : ''); };

  const save = async () => {
    if (!ed || !dirty || saving || file.readOnly) return;
    saving = true; state('salvando…');
    try {
      const r = await api.files.saveText(root, path, ed.text(), file.mtime);
      file.mtime = r.mtime; setDirty(false);
      toast('Salvo', name); onSaved?.();
    } catch (e) {
      state('não salvo', 'dirty');
      if (e.status === 409) {
        // outro programa salvou: oferece sobrescrever mesmo assim
        if (await confirmDialog('O arquivo mudou no servidor', `${e.message} Quer substituir pela sua versão mesmo assim?`, 'Substituir', { danger: true })) {
          file.mtime = 0; saving = false; return save();
        }
      } else toastError(e, 'Não foi possível salvar');
    }
    saving = false;
  };

  d.el.addEventListener('click', (e) => {
    const b = e.target.closest('[data-ed]'); if (!b || b.disabled) return;
    if (b.dataset.ed === 'save') save();
    else if (b.dataset.ed === 'search') ed?.search();
    else if (b.dataset.ed === 'close') d.close();
  });
  // Ctrl+S também quando o foco está fora do editor (nos botões)
  d.el._keys = (e) => { if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') { e.preventDefault(); save(); } };

  const host = $('.ed-host', d.el);
  try {
    const [cm, f] = await Promise.all([loadCM(), api.files.text(root, path)]);
    file = f;
    host.innerHTML = '';
    ed = cm.createEditor({ parent: host, doc: f.text, name, readOnly: f.readOnly, onChange: () => { if (!dirty) setDirty(true); }, onSave: save });
    setDirty(false);
    state(f.readOnly ? 'somente leitura' : `${fmtBytes(f.size)}`);
    ed.focus();
  } catch (e) {
    host.innerHTML = `<div class="pv-none"><p>${esc(e.message || 'Não foi possível abrir o arquivo.')}</p></div>`;
    state('');
  }
}
