// ---------------------------------------------------------------------------
// Initialize Lucide icons — call after DOM ready and after htmx swaps
// ---------------------------------------------------------------------------
function initIcons() {
  if (window.lucide) lucide.createIcons();
}
document.addEventListener('DOMContentLoaded', initIcons);
document.body.addEventListener('htmx:afterSettle', initIcons);

// Toggle inline note editor
function toggleNote(btn) {
  var card = btn.closest('.ticket-card');
  var edit = card.querySelector('.ticket-note-edit');
  var isVisible = edit.style.display !== 'none';
  // Close all open editors in this card
  card.querySelectorAll('.ticket-row-edit').forEach(function(el) { el.style.display = 'none'; });
  if (!isVisible) {
    edit.style.display = 'flex';
    edit.querySelector('input').focus();
  }
}

// Toggle inline review-after date picker


// ---------------------------------------------------------------------------
// Keyboard shortcut system
// ---------------------------------------------------------------------------
// Shared namespace for cross-IIFE communication
window.DeskTriage = window.DeskTriage || {};

(function() {
  'use strict';

  var focusedIndex = -1;
  var helpOverlay = null;
  // Stash detached preview nodes across DOM swaps, keyed by ticket ID
  var stashedPreviews = {};

  // --- Helpers --------------------------------------------------------------

  function getCards() {
    return Array.prototype.slice.call(
      document.querySelectorAll('.ticket-card')
    );
  }

  function clearFocus() {
    var cards = getCards();
    cards.forEach(function(c) { c.classList.remove('kb-focused'); });
    focusedIndex = -1;
  }

  function setFocus(idx, opts) {
    var cards = getCards();
    if (cards.length === 0) return;
    // Clamp
    if (idx < 0) idx = 0;
    if (idx >= cards.length) idx = cards.length - 1;

    var oldIdx = focusedIndex;
    var hadPreview = false;

    // Check if the previously focused card had a preview open
    if (oldIdx >= 0 && oldIdx < cards.length) {
      var oldCard = cards[oldIdx];
      hadPreview = !!oldCard.querySelector('.ticket-preview');
      // Collapse the old preview
      if (hadPreview) {
        var oldPreview = oldCard.querySelector('.ticket-preview');
        if (oldPreview) oldPreview.remove();
      }
    }

    // Remove old focus
    cards.forEach(function(c) { c.classList.remove('kb-focused'); });
    focusedIndex = idx;
    cards[focusedIndex].classList.add('kb-focused');

    // If the old card had a preview, expand the new one and centre it
    if (hadPreview && oldIdx !== idx) {
      togglePreview(cards[focusedIndex]);
      // Centre in viewport after preview loads
      scrollCardToCenter(cards[focusedIndex]);
    } else {
      // Scroll into view normally
      cards[focusedIndex].scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
  }

  function scrollCardToCenter(card) {
    // Small delay so the DOM can update with preview content
    setTimeout(function() {
      card.scrollIntoView({ block: 'center', behavior: 'smooth' });
    }, 50);
  }

  function getFocusedCard() {
    var cards = getCards();
    if (focusedIndex >= 0 && focusedIndex < cards.length) {
      return cards[focusedIndex];
    }
    return null;
  }

  function parseTicketId(card) {
    if (!card || !card.id) return null;
    var m = card.id.match(/^ticket-(\d+)$/);
    return m ? m[1] : null;
  }

  // Expose for command palette
  window.DeskTriage.getFocusedTicketId = function() {
    var card = getFocusedCard();
    return card ? parseTicketId(card) : null;
  };

  // --- Close editors in a card ---------------------------------------------

  function closeEditors(card) {
    if (!card) return;
    card.querySelectorAll('.ticket-row-edit').forEach(function(el) {
      el.style.display = 'none';
    });
  }

  // --- Help overlay ---------------------------------------------------------

  var SHORTCUTS = [
    ['j / \u2193', 'Move to next ticket'],
    ['k / \u2191', 'Move to previous ticket'],
    ['Enter / o', 'Open focused ticket'],
    ['Opt+Enter', 'Open ticket in Freshdesk'],
    ['Space', 'Expand/collapse ticket preview'],
    ['\u2190', 'Toggle Today'],
    ['\u2192', 'Defer to next weekday'],
    ['Shift+\u2192', 'Defer to next sprint'],
    ['t', 'Toggle Today'],
    ['b', 'Toggle Blocked'],
    ['p', 'Toggle Priority / Queue'],
    ['n', 'Open note editor'],
    ['?', 'Toggle this help'],
    ['⌘+Shift+P', 'Command palette'],
    ['Escape', 'Close overlay / clear focus']
  ];

  function createHelpOverlay() {
    var overlay = document.createElement('div');
    overlay.id = 'kb-help-overlay';
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-label', 'Keyboard shortcuts');

    // Styles for overlay
    overlay.style.cssText = [
      'position:fixed', 'inset:0', 'z-index:9999',
      'display:flex', 'align-items:center', 'justify-content:center',
      'background:rgba(0,0,0,0.5)'
    ].join(';');

    var modal = document.createElement('div');
    modal.style.cssText = [
      'background:var(--bg-card)', 'border-radius:8px', 'padding:24px 32px',
      'max-width:420px', 'width:90%', 'box-shadow:0 8px 32px rgba(0,0,0,0.25)',
      'font-family:system-ui,-apple-system,sans-serif', 'color:var(--text-primary)'
    ].join(';');

    var title = document.createElement('h2');
    title.textContent = 'Keyboard Shortcuts';
    title.style.cssText = 'margin:0 0 16px;font-size:18px;';
    modal.appendChild(title);

    var table = document.createElement('table');
    table.style.cssText = 'width:100%;border-collapse:collapse;';

    SHORTCUTS.forEach(function(pair) {
      var tr = document.createElement('tr');

      var tdKey = document.createElement('td');
      tdKey.style.cssText = 'padding:5px 16px 5px 0;white-space:nowrap;vertical-align:top;';
      // Render each key in a <kbd>
      pair[0].split(' / ').forEach(function(k, i) {
        if (i > 0) tdKey.appendChild(document.createTextNode(' / '));
        var kbd = document.createElement('kbd');
        kbd.textContent = k;
        kbd.style.cssText = [
          'display:inline-block', 'padding:2px 8px', 'border-radius:4px',
          'background:var(--bg-kbd)', 'border:1px solid var(--border)',
          'font-family:inherit', 'font-size:13px', 'min-width:24px',
          'text-align:center'
        ].join(';');
        tdKey.appendChild(kbd);
      });

      var tdDesc = document.createElement('td');
      tdDesc.style.cssText = 'padding:5px 0;color:var(--text-secondary);font-size:14px;';
      tdDesc.textContent = pair[1];

      tr.appendChild(tdKey);
      tr.appendChild(tdDesc);
      table.appendChild(tr);
    });

    modal.appendChild(table);
    overlay.appendChild(modal);

    // Click on backdrop closes
    overlay.addEventListener('click', function(e) {
      if (e.target === overlay) hideHelp();
    });

    document.body.appendChild(overlay);
    return overlay;
  }

  function showHelp() {
    if (!helpOverlay) helpOverlay = createHelpOverlay();
    helpOverlay.style.display = 'flex';
  }

  function hideHelp() {
    if (helpOverlay) helpOverlay.style.display = 'none';
  }

  function helpVisible() {
    return helpOverlay && helpOverlay.style.display !== 'none';
  }

  // --- Quick-action helpers -------------------------------------------------

  function clickCardButton(card, iconName) {
    if (!card) return;
    var buttons = card.querySelectorAll('.btn-icon');
    for (var i = 0; i < buttons.length; i++) {
      var icon = buttons[i].querySelector('[data-lucide="' + iconName + '"], svg.lucide-' + iconName);
      if (icon) {
        buttons[i].click();
        return;
      }
    }
  }

  // --- Ticket preview expand/collapse ---------------------------------------

  function markOverflowingPreviews(container) {
    var bodies = container.querySelectorAll('.preview-body');
    for (var i = 0; i < bodies.length; i++) {
      if (bodies[i].scrollHeight > bodies[i].clientHeight) {
        bodies[i].classList.add('is-overflowing');
      } else {
        bodies[i].classList.remove('is-overflowing');
      }
    }
  }

  function togglePreview(card) {
    if (!card) return;
    var id = parseTicketId(card);
    if (!id) return;

    var existing = card.querySelector('.ticket-preview');
    if (existing) {
      existing.remove();
      return;
    }

    // Close any other open previews first (only one at a time)
    getCards().forEach(function(c) {
      if (c !== card) {
        var p = c.querySelector('.ticket-preview');
        if (p) p.remove();
      }
    });

    // Fetch preview HTML from server
    var placeholder = document.createElement('div');
    placeholder.className = 'ticket-preview';
    placeholder.id = 'preview-' + id;
    placeholder.innerHTML = '<div class="preview-section"><div class="preview-label">Loading…</div></div>';
    card.appendChild(placeholder);

    // Centre the card while loading
    scrollCardToCenter(card);

    fetch('/ticket/' + id + '/preview')
      .then(function(resp) {
        if (!resp.ok) throw new Error('HTTP ' + resp.status);
        return resp.text();
      })
      .then(function(html) {
        // Remove the placeholder and insert the real content
        var old = card.querySelector('.ticket-preview');
        if (old) old.remove();
        var temp = document.createElement('div');
        temp.innerHTML = html;
        var preview = temp.firstElementChild;
        if (preview) card.appendChild(preview);
        // Mark bodies that are tall enough to need a fade-out
        markOverflowingPreviews(card);
        // Re-centre after real content is inserted (may be taller)
        scrollCardToCenter(card);
      })
      .catch(function(err) {
        var el = card.querySelector('.ticket-preview');
        if (el) el.innerHTML = '<div class="preview-section"><div class="preview-label">Failed to load preview</div></div>';
      });
  }




  // --- Main keydown handler -------------------------------------------------

  document.addEventListener('keydown', function(e) {
    // Don't intercept when typing in form fields
    var tag = (e.target.tagName || '').toLowerCase();
    if (tag === 'input' || tag === 'textarea' || tag === 'select') {
      // But still let Escape close things
      if (e.key === 'Escape') {
        e.target.blur();
        var card = e.target.closest('.ticket-card');
        if (card) closeEditors(card);
      }
      return;
    }

    // Don't fire on modified keys (Ctrl+C, etc.) except Shift for ? and Alt for Alt+Enter
    if (e.ctrlKey || e.metaKey) return;

    // Alt+Enter: open ticket in Freshdesk
    if (key === 'Enter' && e.altKey) {
      var card = getFocusedCard();
      if (!card) return;
      e.preventDefault();
      var id = parseTicketId(card);
      if (id) {
        var baseURL = document.body.getAttribute('data-freshdesk-url') || '';
        if (baseURL) window.open(baseURL + '/a/tickets/' + id, '_blank');
      }
      return;
    }

    if (e.altKey) return;

    var key = e.key;

    // --- Help overlay ---
    if (key === '?') {
      e.preventDefault();
      if (helpVisible()) {
        hideHelp();
      } else {
        showHelp();
      }
      return;
    }

    // --- Escape ---
    if (key === 'Escape') {
      e.preventDefault();
      if (helpVisible()) {
        hideHelp();
        return;
      }
      // Close editors and preview on focused card
      var fc = getFocusedCard();
      if (fc) {
        closeEditors(fc);
        var ep = fc.querySelector('.ticket-preview');
        if (ep) ep.remove();
      }
      clearFocus();
      return;
    }

    // If help overlay is open, don't process other shortcuts
    if (helpVisible()) return;

    // --- Navigation ---
    if (key === 'j' || key === 'ArrowDown') {
      if (getCards().length === 0) return;
      e.preventDefault();
      setFocus(focusedIndex + 1);
      return;
    }

    if (key === 'k' || key === 'ArrowUp') {
      if (getCards().length === 0) return;
      e.preventDefault();
      if (focusedIndex <= 0) {
        setFocus(0);
      } else {
        setFocus(focusedIndex - 1);
      }
      return;
    }

    if (key === 'Enter' || key === 'o') {
      var card = getFocusedCard();
      if (!card) return;
      e.preventDefault();
      var id = parseTicketId(card);
      if (id) window.location.href = '/ticket/' + id;
      return;
    }

    // --- Quick actions (require focused card) ---
    var focused = getFocusedCard();
    if (!focused) return;

    if (key === 't') {
      e.preventDefault();
      clickCardButton(focused, 'star');
      return;
    }

    if (key === 'b') {
      e.preventDefault();
      clickCardButton(focused, 'ban');
      return;
    }

    if (key === 'p') {
      e.preventDefault();
      // Try pin first, then x (remove from queue)
      var buttons = focused.querySelectorAll('.btn-icon');
      for (var i = 0; i < buttons.length; i++) {
        var icon = buttons[i].querySelector('[data-lucide="pin"], svg.lucide-pin, [data-lucide="x"], svg.lucide-x');
        if (icon) {
          buttons[i].click();
          return;
        }
      }
      return;
    }

    if (key === 'n') {
      e.preventDefault();
      // Only works on ticket-cards
      var noteBtn = focused.querySelector('.btn-note');
      if (noteBtn) toggleNote(noteBtn);
      return;
    }

    // --- Spacebar: expand/collapse ticket preview ---
    if (key === ' ') {
      e.preventDefault();
      togglePreview(focused);
      return;
    }

    // --- Left arrow: toggle today ---
    if (key === 'ArrowLeft') {
      e.preventDefault();
      clickCardButton(focused, 'star');
      return;
    }

    // --- Shift+Right arrow: defer to next sprint ---
    if (key === 'ArrowRight' && e.shiftKey) {
      e.preventDefault();
      clickCardButton(focused, 'fast-forward');
      return;
    }

    // --- Right arrow: defer to tomorrow ---
    if (key === 'ArrowRight') {
      e.preventDefault();
      clickCardButton(focused, 'arrow-right');
      return;
    }
  });

  // --- Inject focus styles --------------------------------------------------

  var style = document.createElement('style');
  style.textContent = [
    '.kb-focused {',
    '  outline: 2px solid var(--accent) !important;',
    '  outline-offset: 2px;',
    '  border-radius: 4px;',
    '  box-shadow: 0 0 0 4px var(--shadow-focus) !important;',
    '}'
  ].join('\n');
  document.head.appendChild(style);

  // --- Stash preview before htmx swaps, restore after ----------------------

  document.body.addEventListener('htmx:beforeSwap', function(evt) {
    var target = evt.detail.target;
    if (!target) return;
    var id = parseTicketId(target);
    if (!id) return;
    var preview = target.querySelector('.ticket-preview');
    if (preview) {
      preview.remove();
      stashedPreviews[id] = preview;
    }
  });

  document.body.addEventListener('htmx:afterSwap', function(evt) {
    // Re-attach any stashed previews immediately (before settle strips them)
    var ids = Object.keys(stashedPreviews);
    ids.forEach(function(id) {
      var newCard = document.getElementById('ticket-' + id);
      if (newCard) {
        newCard.appendChild(stashedPreviews[id]);
      }
      delete stashedPreviews[id];
    });
  });

  document.body.addEventListener('htmx:afterSettle', function() {
    // Re-apply keyboard focus after htmx has fully settled the DOM
    if (focusedIndex >= 0) {
      var cards = getCards();
      if (focusedIndex < cards.length) {
        cards.forEach(function(c) { c.classList.remove('kb-focused'); });
        cards[focusedIndex].classList.add('kb-focused');
      }
    }
    // Re-init Lucide icons for swapped content
    initIcons();
  });

  // --- Click on card body to focus + expand/centre ---
  document.addEventListener('click', function(e) {
    // Ignore clicks on interactive elements (links, buttons, inputs)
    var tag = (e.target.tagName || '').toLowerCase();
    if (tag === 'a' || tag === 'button' || tag === 'input' || tag === 'select' || tag === 'textarea') return;
    // Also ignore if the click target is inside a link or button
    if (e.target.closest('a, button, input, select, textarea')) return;
    // Also ignore clicks inside edit rows (note/review editors)
    if (e.target.closest('.ticket-row-edit')) return;

    var card = e.target.closest('.ticket-card');
    if (!card) return;

    // Don't handle clicks inside the preview itself
    if (e.target.closest('.ticket-preview')) return;

    var cards = getCards();
    var idx = cards.indexOf(card);
    if (idx === -1) return;

    e.preventDefault();

    if (focusedIndex === idx) {
      // Already focused — toggle preview
      togglePreview(card);
    } else {
      // Focus this card and expand preview
      cards.forEach(function(c) { c.classList.remove('kb-focused'); });
      // Collapse any other open preview
      cards.forEach(function(c) {
        if (c !== card) {
          var p = c.querySelector('.ticket-preview');
          if (p) p.remove();
        }
      });
      focusedIndex = idx;
      card.classList.add('kb-focused');
      // Expand preview if not already open
      if (!card.querySelector('.ticket-preview')) {
        togglePreview(card);
      }
      scrollCardToCenter(card);
    }
  });

})();

// ---------------------------------------------------------------------------
// htmx error handling
// ---------------------------------------------------------------------------
(function() {
  'use strict';

  var banner = null;
  var hideTimeout = null;

  function getBanner() {
    if (!banner) banner = document.getElementById('error-banner');
    return banner;
  }

  function showError(msg) {
    var b = getBanner();
    if (!b) return;
    b.textContent = msg;
    b.style.display = 'block';
    if (hideTimeout) clearTimeout(hideTimeout);
    hideTimeout = setTimeout(function() {
      b.style.display = 'none';
    }, 8000);
  }

  // htmx fires this after every request. Check for failures.
  document.body.addEventListener('htmx:responseError', function(e) {
    var xhr = e.detail.xhr;
    var status = xhr ? xhr.status : 0;
    if (status === 0) {
      showError('Network error — could not reach the server. Retrying automatically…');
    } else if (status >= 500) {
      showError('Server error (' + status + '). The Freshdesk API may be temporarily unavailable.');
    } else {
      showError('Request failed (HTTP ' + status + ')');
    }
  });

  document.body.addEventListener('htmx:sendError', function() {
    showError('Network error — could not reach the server.');
  });
})();

// ---------------------------------------------------------------------------
// Command Palette
// ---------------------------------------------------------------------------
(function() {
  'use strict';

  var overlay = null;
  var input = null;
  var resultsList = null;
  var activeIndex = 0;
  var currentResults = [];
  var debounceId = 0;

  // -- Static command sources ------------------------------------------------

  var staticCommands = [
    { label: 'Go to Dashboard', action: function() { window.location.href = '/'; } },
    { label: 'Go to Today',     action: function() { window.location.href = '/today'; } },
    { label: 'Hard refresh',    action: function() {
      var form = document.querySelector('form[action="/refresh"]');
      if (form) form.submit();
    }}
  ];

  // Command sources: each is fn(query) -> Result[] | Promise<Result[]>
  // Results are merged in source order; first source results appear first.
  var sources = [
    // Dynamic "Go to ticket #N" when query is numeric
    function ticketByNumber(q) {
      var trimmed = q.replace(/^#/, '').trim();
      if (/^\d+$/.test(trimmed) && trimmed.length > 0) {
        return [{ label: 'Go to ticket #' + trimmed, action: function() { window.location.href = '/ticket/' + trimmed; } }];
      }
      return [];
    },
    // Fuzzy-filtered static commands
    function filteredStatic(q) {
      var lower = q.toLowerCase();
      if (lower === '') return staticCommands;
      return staticCommands.filter(function(cmd) {
        return cmd.label.toLowerCase().indexOf(lower) !== -1;
      });
    },
    // Ticket state actions (when a ticket is in context)
    ticketStateCommands,
    // Set status to X (when a ticket is in context)
    statusCommands,
    // Handover (when focused ticket is assigned to current agent)
    handoverCommand
  ];

  // -- Ticket state commands source ------------------------------------------

  var stateActions = [
    { label: 'Defer to tomorrow',    field: 'defer_tomorrow',    value: '' },
    { label: 'Defer to next sprint', field: 'defer_next_sprint', value: '' },
    { label: 'Toggle today',         field: 'today',             value: 'toggle' }
  ];

  function ticketStateCommands(q) {
    var ticketId = getContextTicketId();
    if (!ticketId) return [];
    var lower = q.toLowerCase();
    return stateActions.filter(function(sa) {
      return lower === '' || sa.label.toLowerCase().indexOf(lower) !== -1;
    }).map(function(sa) {
      return { label: sa.label, action: makeStateAction(ticketId, sa.field, sa.value) };
    });
  }

  function makeStateAction(ticketId, field, value) {
    return function() {
      var csrfToken = document.querySelector('meta[name="csrf-token"]');
      var token = csrfToken ? csrfToken.content : '';
      var url = '/ticket/' + ticketId + '/state';
      var vals = { field: field, value: value, _csrf: token };

      if (isDetailPage()) {
        vals.from = 'ticket';
        htmx.ajax('PUT', url, {
          target: '#ticket-state',
          swap: 'innerHTML',
          values: vals
        });
      } else {
        htmx.ajax('PUT', url, {
          target: '#ticket-' + ticketId,
          swap: 'outerHTML',
          values: vals
        });
      }
    };
  }

  // -- Status commands source ------------------------------------------------

  var cachedStatusChoices = null;

  // Determine the ticket ID in context: detail page URL or focused card.
  function getContextTicketId() {
    var m = window.location.pathname.match(/^\/ticket\/(\d+)/);
    if (m) return m[1];
    if (window.DeskTriage && window.DeskTriage.getFocusedTicketId) {
      return window.DeskTriage.getFocusedTicketId();
    }
    return null;
  }

  function isDetailPage() {
    return /^\/ticket\/\d+/.test(window.location.pathname);
  }

  function fetchStatusChoices() {
    if (cachedStatusChoices) return Promise.resolve(cachedStatusChoices);
    return fetch('/api/status-choices').then(function(resp) {
      if (!resp.ok) return [];
      return resp.json();
    }).then(function(data) {
      cachedStatusChoices = data || [];
      return cachedStatusChoices;
    }).catch(function() {
      return [];
    });
  }

  function statusCommands(q) {
    var ticketId = getContextTicketId();
    if (!ticketId) return [];

    return fetchStatusChoices().then(function(choices) {
      var lower = q.toLowerCase();
      var results = [];
      choices.forEach(function(sc) {
        var label = 'Set status to ' + sc.label;
        if (lower === '' || label.toLowerCase().indexOf(lower) !== -1) {
          results.push({
            label: label,
            action: makeStatusAction(ticketId, sc.value)
          });
        }
      });
      return results;
    });
  }

  function makeStatusAction(ticketId, statusValue) {
    return function() {
      var csrfToken = document.querySelector('meta[name="csrf-token"]');
      var token = csrfToken ? csrfToken.content : '';
      var url = '/ticket/' + ticketId + '/freshdesk-status';

      if (isDetailPage()) {
        htmx.ajax('POST', url, {
          target: '#freshdesk-status',
          swap: 'innerHTML',
          values: { status: statusValue, from: 'ticket', _csrf: token }
        });
      } else {
        htmx.ajax('POST', url, {
          target: '#ticket-' + ticketId,
          swap: 'outerHTML',
          values: { status: statusValue, from: 'list', _csrf: token }
        });
      }
    };
  }

  // -- Handover command source ------------------------------------------------

  function getFocusedCard() {
    if (window.DeskTriage && window.DeskTriage.getFocusedTicketId) {
      var id = window.DeskTriage.getFocusedTicketId();
      return id ? document.getElementById('ticket-' + id) : null;
    }
    return null;
  }

  function handoverCommand(q) {
    if (isDetailPage()) return []; // handled by the ticket detail page button
    var lower = q.toLowerCase();
    if (lower !== '' && 'handover'.indexOf(lower) === -1) return [];
    var card = getFocusedCard();
    if (!card || !card.hasAttribute('data-assigned')) return [];
    var ticketId = card.id.replace('ticket-', '');
    return [{
      label: 'Handover',
      action: function() { showHandoverDialog(ticketId); }
    }];
  }

  // -- Handover dialog --------------------------------------------------------

  var handoverOverlay = null;

  function showHandoverDialog(ticketId) {
    if (handoverOverlay) handoverOverlay.remove();

    handoverOverlay = document.createElement('div');
    handoverOverlay.className = 'cmd-palette-overlay';
    handoverOverlay.style.display = 'flex';

    var modal = document.createElement('div');
    modal.className = 'cmd-palette';

    var title = document.createElement('div');
    title.className = 'handover-dialog-title';
    title.textContent = 'Handover #' + ticketId;

    var textarea = document.createElement('textarea');
    textarea.className = 'handover-dialog-textarea';
    textarea.placeholder = 'Write a private note\u2026';
    textarea.rows = 5;

    var actions = document.createElement('div');
    actions.className = 'handover-dialog-actions';

    var errorEl = document.createElement('span');
    errorEl.className = 'note-error';

    var cancelBtn = document.createElement('button');
    cancelBtn.className = 'btn btn-cancel';
    cancelBtn.textContent = 'Cancel';
    cancelBtn.type = 'button';

    var submitBtn = document.createElement('button');
    submitBtn.className = 'btn btn-handover';
    submitBtn.textContent = 'Handover';
    submitBtn.type = 'button';

    actions.appendChild(errorEl);
    actions.appendChild(cancelBtn);
    actions.appendChild(submitBtn);
    modal.appendChild(title);
    modal.appendChild(textarea);
    modal.appendChild(actions);
    handoverOverlay.appendChild(modal);
    document.body.appendChild(handoverOverlay);

    textarea.focus();

    function closeDialog() {
      if (handoverOverlay) {
        handoverOverlay.remove();
        handoverOverlay = null;
      }
    }

    function submit() {
      var body = textarea.value.trim();
      if (!body) {
        errorEl.textContent = 'Note body cannot be empty';
        return;
      }
      submitBtn.disabled = true;
      cancelBtn.disabled = true;
      textarea.disabled = true;
      errorEl.textContent = '';

      var csrfMeta = document.querySelector('meta[name="csrf-token"]');
      var token = csrfMeta ? csrfMeta.content : '';

      fetch('/ticket/' + ticketId + '/handover', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/x-www-form-urlencoded',
          'X-CSRF-Token': token
        },
        body: 'body=' + encodeURIComponent(body) + '&from=list'
      }).then(function(resp) {
        if (!resp.ok) {
          return resp.text().then(function(msg) { throw new Error(msg.trim() || 'Handover failed'); });
        }
        closeDialog();
        // Grey out the ticket card (same as deferred)
        var card = document.getElementById('ticket-' + ticketId);
        if (card) {
          card.classList.add('is-deferred');
          card.removeAttribute('data-assigned');
        }
      }).catch(function(err) {
        errorEl.textContent = err.message || 'Handover failed';
        submitBtn.disabled = false;
        cancelBtn.disabled = false;
        textarea.disabled = false;
      });
    }

    cancelBtn.addEventListener('click', closeDialog);
    handoverOverlay.addEventListener('click', function(e) {
      if (e.target === handoverOverlay) closeDialog();
    });
    submitBtn.addEventListener('click', submit);
    textarea.addEventListener('keydown', function(e) {
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        closeDialog();
      }
      if (e.key === 'Enter' && e.metaKey) {
        e.preventDefault();
        submit();
      }
    });
  }

  // -- DOM construction (lazy) -----------------------------------------------

  function ensureDOM() {
    if (overlay) return;

    overlay = document.createElement('div');
    overlay.className = 'cmd-palette-overlay';
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-label', 'Command palette');

    var modal = document.createElement('div');
    modal.className = 'cmd-palette';

    input = document.createElement('input');
    input.className = 'cmd-palette-input';
    input.type = 'text';
    input.placeholder = 'Type a command\u2026';
    input.setAttribute('autocomplete', 'off');
    input.setAttribute('spellcheck', 'false');
    input.setAttribute('role', 'combobox');
    input.setAttribute('aria-expanded', 'true');
    input.setAttribute('aria-controls', 'cmd-palette-results');

    resultsList = document.createElement('ul');
    resultsList.className = 'cmd-palette-results';
    resultsList.id = 'cmd-palette-results';
    resultsList.setAttribute('role', 'listbox');

    modal.appendChild(input);
    modal.appendChild(resultsList);
    overlay.appendChild(modal);
    document.body.appendChild(overlay);

    // -- Event wiring --------------------------------------------------------

    overlay.addEventListener('click', function(e) {
      if (e.target === overlay) close();
    });

    input.addEventListener('input', function() {
      scheduleResolve();
    });

    input.addEventListener('keydown', function(e) {
      if (e.key === 'Escape') {
        e.preventDefault();
        close();
        return;
      }
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setActive(activeIndex + 1);
        return;
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault();
        setActive(activeIndex - 1);
        return;
      }
      if (e.key === 'Enter') {
        e.preventDefault();
        executeActive();
        return;
      }
    });

    resultsList.addEventListener('click', function(e) {
      var li = e.target.closest('[data-index]');
      if (!li) return;
      var idx = parseInt(li.getAttribute('data-index'), 10);
      if (idx >= 0 && idx < currentResults.length) {
        close();
        currentResults[idx].action();
      }
    });
  }

  // -- Resolve sources -------------------------------------------------------

  function scheduleResolve() {
    var id = ++debounceId;
    requestAnimationFrame(function() {
      if (id !== debounceId) return;
      resolve();
    });
  }

  function resolve() {
    var q = input.value;
    var pending = sources.map(function(src) { return src(q); });

    Promise.all(pending).then(function(arrays) {
      var merged = [];
      arrays.forEach(function(arr) {
        if (arr && arr.length) merged = merged.concat(arr);
      });
      currentResults = merged;
      activeIndex = 0;
      render();
    });
  }

  // -- Render ----------------------------------------------------------------

  function render() {
    resultsList.innerHTML = '';
    currentResults.forEach(function(cmd, i) {
      var li = document.createElement('li');
      li.className = 'cmd-palette-item' + (i === activeIndex ? ' active' : '');
      li.setAttribute('role', 'option');
      li.setAttribute('data-index', i);
      li.textContent = cmd.label;
      resultsList.appendChild(li);
    });
  }

  function setActive(idx) {
    if (currentResults.length === 0) return;
    if (idx < 0) idx = currentResults.length - 1;
    if (idx >= currentResults.length) idx = 0;
    activeIndex = idx;
    var items = resultsList.querySelectorAll('.cmd-palette-item');
    items.forEach(function(el, i) {
      el.classList.toggle('active', i === activeIndex);
    });
    if (items[activeIndex]) items[activeIndex].scrollIntoView({ block: 'nearest' });
  }

  function executeActive() {
    if (activeIndex >= 0 && activeIndex < currentResults.length) {
      var action = currentResults[activeIndex].action;
      close();
      action();
    }
  }

  // -- Open / Close ----------------------------------------------------------

  function open() {
    ensureDOM();
    input.value = '';
    overlay.style.display = 'flex';
    resolve();
    input.focus();
  }

  function close() {
    if (overlay) overlay.style.display = 'none';
  }

  function isOpen() {
    return overlay && overlay.style.display !== 'none';
  }

  // -- Global keyboard trigger -----------------------------------------------

  document.addEventListener('keydown', function(e) {
    // Cmd+Shift+P (Mac) or Ctrl+Shift+P (Win/Linux)
    if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key.toLowerCase() === 'p') {
      e.preventDefault();
      if (isOpen()) {
        close();
      } else {
        open();
      }
      return;
    }
    // Escape closes the palette from anywhere
    if (e.key === 'Escape' && isOpen()) {
      e.preventDefault();
      close();
    }
  });
})();

// Register service worker for PWA installability
if ('serviceWorker' in navigator) {
  navigator.serviceWorker.register('/sw.js');
}
