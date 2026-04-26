export const fixtureIndexHTML = `<!doctype html>
<html lang="en" data-accent="green" data-density="dense">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>lurker</title>
  </head>
  <body>
    <div id="app" class="app">
      <aside id="sidebar" class="sidebar" aria-label="Networks">
        <div id="sidebar-status" class="sb-status" aria-label="Connectivity status">
          <div id="backend-status" class="sb-status-item">
            <span class="sb-status-dot"></span>
            <span id="backend-status-text" class="sb-status-value">Connecting…</span>
          </div>
          <div id="tailscale-status" class="sb-status-item">
            <span class="sb-status-dot"></span>
            <span id="tailscale-status-text" class="sb-status-value">Connected</span>
          </div>
        </div>
        <div id="sb-scroll" class="sbscroll"></div>
        <div class="sb-footer">
          <button type="button" title="Account settings">
            <span>Account settings &amp; info</span>
          </button>
        </div>
      </aside>

      <main id="main" class="mainpane">
        <header id="buffer-header" class="topicbar">
          <div class="title">
            <button id="mobile-menu" class="mobile-menu-btn" type="button" aria-label="Open buffers"></button>
            <div id="buffer-name" class="chan">—</div>
          </div>
          <div id="buffer-topic" class="topic" title="Click to edit topic">
            <span class="topictext">—</span>
            <span class="edit" aria-hidden="true"></span>
          </div>
          <div class="actions">
            <span id="buffer-memcount" class="memcount" title="Members" hidden>
              <span id="member-count-inline">0</span>
            </span>
            <button class="icbtn" type="button" title="Channel info" aria-label="Channel info"></button>
            <button id="toggle-members" class="icbtn toggled" type="button" title="Toggle member list" aria-label="Toggle member list"></button>
          </div>
        </header>

        <section id="messages" class="messages" aria-live="polite"></section>
        <section id="status-view" class="statusview" hidden></section>

        <form id="input-form" class="inputbar">
          <div id="cmd-pop" class="cmdpop" hidden></div>
          <div class="row">
            <span class="prompt"><span id="input-nick" class="you">—</span></span>
            <input id="input" class="msgin" type="text" autocomplete="off" placeholder="Message channel" disabled />
            <div class="actions">
              <button class="icbtn" type="button" title="Slash commands" aria-label="Slash commands"></button>
              <button class="icbtn" type="submit" title="Send" aria-label="Send"></button>
            </div>
          </div>
        </form>
      </main>

      <aside id="member-pane" class="memberpane" aria-label="Members">
        <div class="mheader">
          <span class="lbl">members</span>
          <span id="member-count" class="count">0</span>
        </div>
        <div id="member-list" class="mlist"></div>
      </aside>
    </div>
  </body>
</html>`;
