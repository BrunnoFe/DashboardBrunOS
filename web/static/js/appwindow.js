// Janela de app (0.17.0, decisão 58): o app do Runtipi (ou um link) dentro do BrunOS, num iframe.
// Sem sandbox (quebraria login e envio de arquivos dos apps). O navegador não deixa o BrunOS ver em
// qual página do app você está, então recarregar e "abrir em nova aba" levam à página inicial do app.
import { esc, svg } from './ui.js';

// a = app, url = endereço, iconHTML = ícone pronto, blocked = texto explicando por que não abre aqui
export function appWindow(a, url, { iconHTML = '', originEl, blocked = '' } = {}) {
  let frame = null, body = null;
  const newTab = () => window.open(url, '_blank', 'noopener');
  return {
    id: a.id, title: a.name, icon: iconHTML, originEl,
    tools: [
      { icon: 'refresh', label: 'Recarregar (volta à página inicial do app)', run: () => {
        if (!frame) return;
        body.classList.remove('loaded');
        frame.addEventListener('load', () => body.classList.add('loaded'), { once: true });
        frame.src = url;
      } },
      { icon: 'external', label: 'Abrir em nova aba', run: newTab },
    ],
    mount(el) {
      body = el;
      if (blocked) {
        el.innerHTML = `<div class="win-blocked">${iconHTML}<h3>Este app não abre dentro do BrunOS</h3><p>${esc(blocked)}</p>
          <button class="pill-btn primary" data-open>${svg('external')}Abrir em nova aba</button></div>`;
        el.querySelector('[data-open]').addEventListener('click', newTab);
        return null;
      }
      el.innerHTML = '<div class="win-loading" aria-hidden="true"></div>';
      frame = document.createElement('iframe');
      frame.title = a.name;
      frame.allow = 'fullscreen; clipboard-write; autoplay; picture-in-picture';
      frame.referrerPolicy = 'no-referrer';
      frame.addEventListener('load', () => el.classList.add('loaded'), { once: true });
      // se o app demorar, tira o "carregando" mesmo assim (alguns nunca disparam load por causa de streams)
      setTimeout(() => el.classList.add('loaded'), 15000);
      frame.src = url;
      el.append(frame);
      return () => { frame.src = 'about:blank'; frame = null; };
    },
  };
}
