// Teabag experimental frontend — htmxui
//
// Everything here is behaviour that neither plain HTML nor HTMX can express.
// Anything that can be a form, a link, <details> or an hx-* attribute lives in
// the templates instead, and is deliberately absent from this file.
'use strict';

// Clipboard: the async Clipboard API is not reachable from a plain link or
// htmx attribute, so the few places that need it opt in with data-x-copy.
// Delegated on document so it keeps working across htmx swaps.
document.addEventListener('click', (ev) => {
	const trigger = ev.target.closest('[data-x-copy]');
	if (!trigger) return;
	const text = trigger.dataset.xCopy;
	if (!text) return;

	ev.preventDefault();
	const done = (ok) => {
		trigger.classList.toggle('x-copy-done', ok);
		setTimeout(() => trigger.classList.remove('x-copy-done'), 1200);
	};
	// clipboard API needs a secure context; fall back to a hidden textarea + execCommand
	if (navigator.clipboard) {
		navigator.clipboard.writeText(text).then(() => done(true), () => done(false));
		return;
	}
	const ta = document.createElement('textarea');
	ta.value = text;
	ta.setAttribute('readonly', '');
	ta.style.position = 'fixed';
	ta.style.opacity = '0';
	document.body.appendChild(ta);
	ta.select();
	let ok = false;
	try { ok = document.execCommand('copy'); } catch { ok = false; }
	ta.remove();
	done(ok);
});
