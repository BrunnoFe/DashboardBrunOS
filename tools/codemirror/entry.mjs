// Ponto de entrada do pacote do CodeMirror usado pelo editor de texto do BrunOS (web/static/js/editor.js).
// Tudo que o editor precisa sai daqui num arquivo só, sem dependências em tempo de execução.
import { EditorState, Compartment } from '@codemirror/state';
import {
  EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter, drawSelection,
  highlightSpecialChars, rectangularSelection, crosshairCursor, dropCursor,
} from '@codemirror/view';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { searchKeymap, highlightSelectionMatches, openSearchPanel } from '@codemirror/search';
import { closeBrackets, closeBracketsKeymap } from '@codemirror/autocomplete';
import {
  syntaxHighlighting, HighlightStyle, indentOnInput, bracketMatching, foldGutter, foldKeymap, StreamLanguage, indentUnit,
} from '@codemirror/language';
import { tags as t } from '@lezer/highlight';
import { markdown } from '@codemirror/lang-markdown';
import { json } from '@codemirror/lang-json';
import { yaml } from '@codemirror/lang-yaml';
import { javascript } from '@codemirror/lang-javascript';
import { python } from '@codemirror/lang-python';
import { html } from '@codemirror/lang-html';
import { css } from '@codemirror/lang-css';
import { xml } from '@codemirror/lang-xml';
import { shell } from '@codemirror/legacy-modes/mode/shell';
import { toml } from '@codemirror/legacy-modes/mode/toml';
import { properties } from '@codemirror/legacy-modes/mode/properties';
import { dockerFile } from '@codemirror/legacy-modes/mode/dockerfile';
import { go } from '@codemirror/legacy-modes/mode/go';
import { nginx } from '@codemirror/legacy-modes/mode/nginx';
import { diff } from '@codemirror/legacy-modes/mode/diff';

const legacy = (m) => StreamLanguage.define(m);

// Linguagem pelo nome do arquivo (a extensão ou nomes conhecidos como Dockerfile).
export function languageFor(name) {
  const n = String(name).toLowerCase();
  const ext = n.includes('.') ? n.split('.').pop() : '';
  if (n === 'dockerfile' || ext === 'dockerfile') return legacy(dockerFile);
  if (n === '.env' || n.startsWith('.env.') || ext === 'env') return legacy(properties);
  switch (ext) {
    case 'md': case 'markdown': return markdown();
    case 'json': case 'webmanifest': return json();
    case 'yml': case 'yaml': return yaml();
    case 'js': case 'mjs': case 'cjs': return javascript();
    case 'ts': return javascript({ typescript: true });
    case 'py': return python();
    case 'html': case 'htm': return html();
    case 'css': return css();
    case 'xml': case 'svg': return xml();
    case 'sh': case 'bash': case 'zsh': return legacy(shell);
    case 'toml': return legacy(toml);
    case 'ini': case 'conf': case 'cfg': case 'properties': return legacy(properties);
    case 'go': return legacy(go);
    case 'diff': case 'patch': return legacy(diff);
    case 'nginx': return legacy(nginx);
    default: return null;
  }
}

// Cores no estilo do BrunOS (vidro escuro, destaque azul).
const theme = EditorView.theme({
  '&': { color: '#e6e8ee', backgroundColor: 'transparent', height: '100%', fontSize: '13px' },
  '.cm-scroller': { fontFamily: "ui-monospace, 'SF Mono', Menlo, Consolas, monospace", lineHeight: '1.6' },
  '.cm-content': { caretColor: '#fff', padding: '10px 0' },
  '.cm-cursor, .cm-dropCursor': { borderLeftColor: '#fff' },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': { backgroundColor: 'rgba(120,160,255,.32) !important' },
  '.cm-gutters': { backgroundColor: 'transparent', color: 'rgba(255,255,255,.3)', border: 'none' },
  '.cm-activeLineGutter': { backgroundColor: 'transparent', color: 'rgba(255,255,255,.75)' },
  '.cm-activeLine': { backgroundColor: 'rgba(255,255,255,.04)' },
  '.cm-matchingBracket': { backgroundColor: 'rgba(255,255,255,.12)', outline: 'none' },
  '.cm-selectionMatch': { backgroundColor: 'rgba(255,255,255,.08)' },
  '.cm-foldPlaceholder': { backgroundColor: 'rgba(255,255,255,.1)', border: 'none', color: '#ccc' },
  '.cm-panels': { backgroundColor: 'rgba(20,22,28,.95)', color: '#e6e8ee' },
  '.cm-panels.cm-panels-bottom': { borderTop: '1px solid rgba(255,255,255,.08)' },
  '.cm-search input, .cm-search button': { borderRadius: '8px' },
  '.cm-textfield': { backgroundColor: 'rgba(255,255,255,.08)', border: '1px solid rgba(255,255,255,.12)', color: '#fff' },
  '.cm-button': { backgroundImage: 'none', backgroundColor: 'rgba(255,255,255,.1)', border: 'none', color: '#fff' },
  '.cm-searchMatch': { backgroundColor: 'rgba(250,204,21,.25)' },
  '.cm-searchMatch.cm-searchMatch-selected': { backgroundColor: 'rgba(250,204,21,.5)' },
}, { dark: true });

const colors = HighlightStyle.define([
  { tag: [t.keyword, t.operatorKeyword, t.modifier], color: '#c792ea' },
  { tag: [t.string, t.special(t.string), t.regexp], color: '#a5e075' },
  { tag: [t.number, t.bool, t.null, t.atom], color: '#f78c6c' },
  { tag: [t.comment, t.lineComment, t.blockComment], color: '#6b7488', fontStyle: 'italic' },
  { tag: [t.propertyName, t.attributeName], color: '#82aaff' },
  { tag: [t.definition(t.variableName), t.function(t.variableName), t.function(t.propertyName)], color: '#7fd6e8' },
  { tag: [t.typeName, t.className, t.namespace], color: '#ffcb6b' },
  { tag: [t.tagName], color: '#f07178' },
  { tag: [t.heading], color: '#82aaff', fontWeight: '700' },
  { tag: [t.strong], fontWeight: '700' },
  { tag: [t.emphasis], fontStyle: 'italic' },
  { tag: [t.link, t.url], color: '#7fd6e8', textDecoration: 'underline' },
  { tag: [t.meta, t.processingInstruction], color: '#89ddff' },
  { tag: [t.inserted], color: '#a5e075' },
  { tag: [t.deleted], color: '#f07178' },
  { tag: t.invalid, color: '#ff5370' },
]);

// createEditor monta o editor dentro de parent. onChange(doc) a cada mudança; onSave() no Ctrl+S.
export function createEditor({ parent, doc = '', name = '', readOnly = false, onChange, onSave }) {
  const lang = languageFor(name);
  const ro = new Compartment();
  const view = new EditorView({
    parent,
    state: EditorState.create({
      doc,
      extensions: [
        lineNumbers(), highlightActiveLineGutter(), highlightSpecialChars(), history(), foldGutter(), drawSelection(),
        dropCursor(), EditorState.allowMultipleSelections.of(true), indentOnInput(), bracketMatching(), closeBrackets(),
        rectangularSelection(), crosshairCursor(), highlightActiveLine(), highlightSelectionMatches(),
        indentUnit.of(/\.(ya?ml|py)$/i.test(name) ? '    '.slice(0, /\.ya?ml$/i.test(name) ? 2 : 4) : '  '),
        keymap.of([
          { key: 'Mod-s', preventDefault: true, run: () => { onSave?.(); return true; } },
          ...closeBracketsKeymap, ...defaultKeymap, ...searchKeymap, ...historyKeymap, ...foldKeymap, indentWithTab,
        ]),
        theme, syntaxHighlighting(colors), EditorView.lineWrapping,
        ro.of(EditorState.readOnly.of(readOnly)),
        ...(lang ? [lang] : []),
        EditorView.updateListener.of((u) => { if (u.docChanged) onChange?.(u.state.doc); }),
      ],
    }),
  });
  return {
    view,
    text: () => view.state.doc.toString(),
    focus: () => view.focus(),
    search: () => openSearchPanel(view),
    setReadOnly: (v) => view.dispatch({ effects: ro.reconfigure(EditorState.readOnly.of(v)) }),
    destroy: () => view.destroy(),
  };
}
