<script setup>
import { onMounted, onBeforeUnmount, ref, defineAsyncComponent } from 'vue'
import { withBase } from 'vitepress'
import * as D from '../landing-data.js'

// VitePress's real local docs search, lazy-loaded and opened from the nav / ⌘K.
const VPLocalSearchBox = defineAsyncComponent(
  () => import('vitepress/dist/client/theme-default/components/VPLocalSearchBox.vue')
)
const showSearch = ref(false)

const root = ref(null)

// Track every timer / listener / observer so SPA navigation tears them down.
let alive = true
const timers = new Set()
const observers = []
const cleanups = []
let prevScrollBehavior = ''

function later(fn, ms) {
  const id = setTimeout(() => { timers.delete(id); if (alive) fn() }, ms)
  timers.add(id)
  return id
}

onMounted(() => {
  const el = root.value
  if (!el) return
  const $ = (s) => el.querySelector(s)
  const $$ = (s) => [...el.querySelectorAll(s)]

  prevScrollBehavior = document.documentElement.style.scrollBehavior
  document.documentElement.style.scrollBehavior = 'smooth'

  /* ---------- Glyph injection ---------- */
  $$('[data-logo]').forEach((n) => { n.innerHTML = D.glyph(n.dataset.logo) })

  /* ---------- Nav: sticky shadow ---------- */
  const nav = $('#nav')
  const onScroll = () => nav && nav.classList.toggle('stuck', window.scrollY > 12)
  onScroll()
  window.addEventListener('scroll', onScroll, { passive: true })
  cleanups.push(() => window.removeEventListener('scroll', onScroll))

  /* ---------- Mobile menu (hamburger) ---------- */
  const burger = $('#nav-burger')
  const closeMenu = () => { nav.classList.remove('menu-open'); burger && burger.setAttribute('aria-expanded', 'false') }
  if (burger) {
    const onBurger = () => burger.setAttribute('aria-expanded', String(nav.classList.toggle('menu-open')))
    burger.addEventListener('click', onBurger)
    cleanups.push(() => burger.removeEventListener('click', onBurger))
  }
  $$('.nav-links a').forEach((a) => {
    a.addEventListener('click', closeMenu)
    cleanups.push(() => a.removeEventListener('click', closeMenu))
  })

  /* ---------- Reveal on scroll ---------- */
  const motionOK = !document.hidden &&
    (!window.matchMedia || matchMedia('(prefers-reduced-motion: no-preference)').matches)
  if (motionOK) el.classList.add('anim')

  const io = new IntersectionObserver((entries) => {
    entries.forEach((e) => { if (e.isIntersecting) { e.target.classList.add('in'); io.unobserve(e.target) } })
  }, { threshold: 0.08, rootMargin: '0px 0px -6% 0px' })
  observers.push(io)
  function observeReveal(n) {
    const r = n.getBoundingClientRect()
    if (r.top < (window.innerHeight || 800) * 0.96) n.classList.add('in')
    else io.observe(n)
  }
  $$('.reveal').forEach(observeReveal)
  later(() => $$('.reveal:not(.in)').forEach((n) => {
    const r = n.getBoundingClientRect()
    if (r.top < (window.innerHeight || 800)) n.classList.add('in')
  }), 1200)

  /* ---------- Feature tilt glow (cursor-follow) ---------- */
  $$('.feat[data-tilt]').forEach((card) => {
    const move = (e) => {
      const r = card.getBoundingClientRect()
      card.style.setProperty('--mx', `${e.clientX - r.left}px`)
      card.style.setProperty('--my', `${e.clientY - r.top}px`)
    }
    card.addEventListener('pointermove', move)
    cleanups.push(() => card.removeEventListener('pointermove', move))
  })

  /* ---------- Typewriter helper ---------- */
  function typeInto(node, text, speed, done) {
    node.textContent = ''
    let i = 0
    const cur = document.createElement('span')
    cur.className = 'cursor'
    node.appendChild(cur)
    ;(function tick() {
      if (i <= text.length) {
        cur.remove()
        node.textContent = text.slice(0, i)
        node.appendChild(cur)
        i++
        later(tick, speed)
      } else if (done) { done() }
    })()
  }

  /* ---------- Hero: install command ---------- */
  const installCmd = $('#install-cmd')
  typeInto(installCmd, D.INSTALL, 22)

  const copyBtn = $('#copy-install')
  const copyIcon = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h10"/></svg>`
  const onCopy = async () => {
    try { await navigator.clipboard.writeText(D.INSTALL) } catch (e) {}
    copyBtn.classList.add('done')
    copyBtn.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20 6 9 17l-5-5"/></svg>`
    later(() => { copyBtn.classList.remove('done'); copyBtn.innerHTML = copyIcon }, 1600)
  }
  copyBtn.addEventListener('click', onCopy)
  cleanups.push(() => copyBtn.removeEventListener('click', onCopy))

  /* ---------- asciinema players (hero, quick-start) ---------- */
  let AP = null
  const players = []
  const loadAP = async () => (AP ||= await import('asciinema-player'))
  // fit:'width' scales the font to the container so the terminal is responsive.
  // (A fixed terminalFontSize would pin the size and stop it resizing.)
  // Respect reduced-motion: don't auto-play/loop, and expose controls instead.
  const reduceMotion = Boolean(window.matchMedia && matchMedia('(prefers-reduced-motion: reduce)').matches)
  const castOpts = { autoPlay: !reduceMotion, loop: !reduceMotion, controls: reduceMotion, fit: 'width' }
  async function mountCast(sel, src, extra) {
    const el = $(sel)
    if (!el) return null
    const mod = await loadAP()
    if (!alive) return null
    const p = mod.create(withBase(src), el, { ...castOpts, ...extra })
    players.push(p)
    return p
  }
  cleanups.push(() => { players.forEach((p) => { try { p.dispose() } catch (e) {} }); players.length = 0 })

  /* ---------- Hero terminal (asciinema) ---------- */
  const heroIO = new IntersectionObserver((es) => {
    es.forEach((e) => { if (e.isIntersecting) { heroIO.disconnect(); mountCast('#hero-cast', '/casts/hero.cast') } })
  }, { threshold: 0.3 })
  heroIO.observe($('#hero-cast'))
  observers.push(heroIO)


  /* ---------- Comparison table ---------- */
  function cmpCell(v) {
    if (v === 'yes') return `<span class="cmp-yes"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4"><path d="M20 6 9 17l-5-5"/></svg></span>`
    if (v === 'no') return `<span class="cmp-no"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4"><path d="M18 6 6 18M6 6l12 12"/></svg></span>`
    return `<span class="cmp-partial"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4"><path d="M5 12h14"/></svg></span>`
  }
  const cmpTable = $('#cmp-table')
  const logoSrc = withBase('/assets/logo.svg')
  const headCells = D.CMP.cols.map((c, idx) => idx === 0
    ? `<th scope="col" class="servlo-col"><span class="cmp-lead"><img class="brand-logo sm" src="${logoSrc}" alt="" width="22" height="22" />${c}</span></th>`
    : `<th scope="col">${c}</th>`).join('')
  cmpTable.innerHTML = `
    <thead><tr><th></th>${headCells}</tr></thead>
    <tbody>${D.CMP.rows.map((r) => `<tr><th scope="row">${r.f}</th>${r.v.map((v, idx) => `<td class="${idx === 0 ? 'servlo-col' : ''}">${cmpCell(v)}</td>`).join('')}</tr>`).join('')}</tbody>`

  /* ---------- Services showcase ---------- */
  $('#svc-cards').innerHTML = D.SVC_SHOW.map((s) => `
    <div class="card svc-card reveal">
      <span class="gl" style="display:block">${D.glyph(s.logo)}</span>
      <b>${s.name}</b><span>${s.port}</span>
    </div>`).join('')
  $$('#svc-cards .reveal').forEach(observeReveal)

  /* ---------- Quick-start stepper ---------- */
  const stepList = $('#step-list')
  stepList.innerHTML = D.STEPS.map((s, i) => `
    <div class="step ${i === 0 ? 'active' : ''}" data-step="${i}">
      <span class="step-num">${s.n}</span>
      <div><h4>${s.title}</h4><p>${s.desc}</p></div>
    </div>`).join('')
  const stepFile = $('#step-file')
  const STEP_CASTS = ['/casts/step-01.cast', '/casts/step-02.cast']
  let stepPlayer = null
  async function showStep(idx) {
    $$('.step').forEach((s) => s.classList.toggle('active', +s.dataset.step === idx))
    stepFile.textContent = D.STEPS[idx].file
    const mod = await loadAP()
    if (!alive) return
    if (stepPlayer) { try { stepPlayer.dispose() } catch (e) {} stepPlayer = null }
    stepPlayer = mod.create(withBase(STEP_CASTS[idx]), $('#step-cast'), castOpts)
  }
  cleanups.push(() => { if (stepPlayer) { try { stepPlayer.dispose() } catch (e) {} } })
  $$('.step').forEach((s) => {
    const click = () => showStep(+s.dataset.step)
    s.addEventListener('click', click)
    cleanups.push(() => s.removeEventListener('click', click))
  })
  showStep(0)

  /* ---------- Docs search (VitePress local search) ---------- */
  const openSearch = () => { showSearch.value = true }
  const searchBtn = $('#open-cmdk')
  searchBtn.addEventListener('click', openSearch)
  cleanups.push(() => searchBtn.removeEventListener('click', openSearch))
  const onSearchKey = (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); showSearch.value = true }
  }
  document.addEventListener('keydown', onSearchKey)
  cleanups.push(() => document.removeEventListener('keydown', onSearchKey))

  /* ---------- Boot ---------- */
  setOS('linux')
})

onBeforeUnmount(() => {
  alive = false
  timers.forEach(clearTimeout); timers.clear()
  observers.forEach((o) => o.disconnect())
  cleanups.forEach((fn) => fn())
  document.documentElement.style.scrollBehavior = prevScrollBehavior
})
</script>

<template>
  <div class="servlo-landing" ref="root">
    <div class="bg-field"></div>

    <!-- ============ NAV ============ -->
    <nav class="nav" id="nav">
      <div class="wrap nav-inner">
        <a class="brand" href="#top">
          <img class="brand-logo" :src="withBase('/assets/logo.svg')" alt="Servlo logo" width="32" height="32" />
          Servlo
        </a>
        <div class="nav-links">
          <a href="#features">Features</a>
          <a href="#dashboard">Dashboard</a>
          <a href="#compare">Why Servlo</a>
          <a href="#services">Services</a>
          <a href="#start">Get started</a>
          <a :href="withBase('/getting-started/requirements')">Docs</a>
        </div>
        <div class="nav-cta">
          <button class="cmdk-btn" id="open-cmdk" aria-label="Open command menu">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/></svg>
            <span class="lbl">Search</span>
            <span class="kbd">⌘K</span>
          </button>
          <a class="btn btn-primary nav-install" :href="withBase('/getting-started/installation')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M5 12h14M13 6l6 6-6 6"/></svg>
            Install
          </a>
          <button class="nav-burger" id="nav-burger" aria-label="Menu" aria-expanded="false">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" d="M4 6h16M4 12h16M4 18h16"/></svg>
          </button>
        </div>
      </div>
    </nav>

    <main id="top">

      <!-- ============ HERO ============ -->
      <section class="hero">
        <div class="wrap hero-grid">
          <div class="hero-copy">
            <span class="eyebrow reveal"><span class="dot"></span>Open-source · Podman-native · Rootless</span>
            <h1 class="h-display reveal d1">Local PHP development for <span class="accent">Linux</span><span class="hero-mac"> &amp; macOS</span></h1>
            <p class="lead reveal d2">Servlo runs Nginx, PHP-FPM and your services as rootless Podman containers. Automatic <code class="kbd">.test</code> domains, per-project PHP &amp; Node, one-command TLS. No Docker daemon, no sudo, no system pollution.</p>

            <div class="hero-meta reveal d4">
              <span class="mi"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2 4 6v6c0 5 3.5 8 8 10 4.5-2 8-5 8-10V6l-8-4z"/></svg><b>Rootless</b> · no sudo</span>
              <span class="mi"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13 2 3 14h7l-1 8 10-12h-7l1-8z"/></svg><b>No Docker</b> daemon</span>
              <span class="mi"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9"/><path d="M9 12l2 2 4-4"/></svg>PHP <b>7.4 – 8.5</b></span>
            </div>
          </div>

          <div class="hero-term-wrap reveal d2">
            <div class="win term">
              <div class="win-bar">
                <span class="win-dots"><i></i><i></i><i></i></span>
                <span class="win-title">~/code/acme · servlo</span>
              </div>
              <div class="cast" id="hero-cast" role="img" aria-label="Terminal recording: servlo link auto-detecting a Laravel project and provisioning HTTPS"></div>
            </div>

            <div class="install reveal d3" id="install">
              <div class="cmd-row">
                <span class="prompt">$</span>
                <span class="cmd-text" id="install-cmd"></span>
                <button class="copy-btn" id="copy-install" aria-label="Copy install command">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h10"/></svg>
                </button>
              </div>
            </div>

            <div class="hero-actions reveal d4">
              <a class="btn btn-primary" :href="withBase('/getting-started/requirements')">Get started in 60 seconds</a>
              <a class="btn btn-ghost" href="#dashboard">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="m9 18 6-6-6-6"/></svg>
                See the Web UI
              </a>
              <a class="btn btn-ghost btn-icon" href="https://github.com/realrashid/servlo" target="_blank" rel="noopener" aria-label="GitHub">
                <svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23A11.509 11.509 0 0 1 12 5.803c1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222 0 1.606-.014 2.898-.014 3.293 0 .322.216.694.825.576C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12"/></svg>
              </a>
            </div>
          </div>
        </div>
      </section>
      <!-- ============ FRAMEWORK STRIP ============ -->
      <div class="strip">
        <div class="wrap strip-inner">
          <span class="strip-label">Auto-detected on servlo link</span>
          <div class="strip-items">
            <span class="strip-item"><span class="gl" data-logo="laravel"></span> Laravel</span>
            <span class="strip-item"><span class="gl" data-logo="symfony"></span> Symfony</span>
            <span class="strip-item"><span class="gl" data-logo="wordpress"></span> WordPress</span>
            <span class="strip-item"><span class="gl" data-logo="drupal"></span> Drupal</span>
            <span class="strip-item"><span class="gl" data-logo="magento"></span> Magento</span>
            <span class="strip-item"><span class="gl" data-logo="cake"></span> CakePHP</span>
            <span class="strip-item"><span class="gl" data-logo="statamic"></span> Statamic</span>
            <span class="strip-item"><span class="gl" data-logo="codeigniter"></span> CodeIgniter</span>
            <span class="strip-item"><span class="gl" data-logo="tempest"></span> Tempest</span>
          </div>
        </div>
      </div>

      <!-- ============ FEATURES (BENTO) ============ -->
      <section id="features">
        <div class="wrap">
          <div class="sec-head reveal">
            <span class="eyebrow"><span class="dot"></span>Built for developer experience</span>
            <h2 class="h-section" style="margin-top:18px">Everything the Linux PHP dev<br/>never had, in one binary.</h2>
            <p class="lead">From <code class="kbd">servlo link</code> to a live HTTPS site in seconds. Then the deep stuff: a dump debugger and worker self-heal, surfaced across CLI, dashboard and TUI.</p>
          </div>

          <div class="bento">
            <div class="feat col-3 reveal" data-tilt>
              <div class="feat-ic"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M12 2 3 7v10l9 5 9-5V7l-9-5z"/><path d="m3 7 9 5 9-5M12 12v10"/></svg></div>
              <h3>Rootless Podman, zero daemon</h3>
              <p>Nginx, PHP-FPM and services run as <em>your</em> user via systemd user units, dual-stack IPv4 + IPv6 where available. No Docker, no background daemon, no sudo.</p>
              <span class="feat-tag">// no system pollution</span>
            </div>
            <div class="feat col-3 reveal d1" data-tilt>
              <div class="feat-ic"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="3" y="4" width="18" height="14" rx="2"/><path d="M3 9h18M7 14h2"/></svg></div>
              <h3>Automatic .test domains + TLS</h3>
              <p>One command provisions a trusted certificate via mkcert. Or opt out and use <code class="kbd">*.localhost</code>, no dnsmasq, no resolver tweaks, no sudo for the DNS bits.</p>
              <span class="feat-tag">// project.test → https</span>
            </div>

            <div class="feat col-2 reveal" data-tilt>
              <div class="feat-ic"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M16 18 22 12 16 6M8 6 2 12l6 6"/></svg></div>
              <h3>Per-project PHP &amp; Node</h3>
              <p>PHP 8.1–8.5 plus a frozen 7.4 / 8.0 legacy tier. Switch with one command, isolated per repo.</p>
            </div>
            <div class="feat col-2 reveal d1" data-tilt>
              <div class="feat-ic"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><path d="M12 2v4M12 18v4M5 5l2.5 2.5M16.5 16.5 19 19M2 12h4M18 12h4M5 19l2.5-2.5M16.5 7.5 19 5"/><circle cx="12" cy="12" r="3"/></svg></div>
              <h3>Worker self-heal</h3>
              <p>Queue, schedule, Horizon and Reverb workers plus the Stripe listener, monitored everywhere and recovered with one click.</p>
            </div>

          </div>

          <div class="feat-more">
            <span class="feat-more-label">Also includes</span>
            <span class="feat-chip">FrankenPHP &amp; Octane</span>
            <span class="feat-chip">Tabbed mouse-driven TUI</span>
            <span class="feat-chip">Polyglot sites · Node, Python, Go &amp; Ruby</span>
          </div>
        </div>
      </section>

      <!-- ============ DASHBOARD PREVIEW ============ -->
      <section id="dashboard">
        <div class="wrap">
          <div class="sec-head reveal">
            <span class="eyebrow"><span class="dot"></span>The built-in Web UI · installable as a PWA</span>
            <h2 class="h-section" style="margin-top:18px">A dashboard that actually<br/>manages your stack.</h2>
            <p class="lead">Switch PHP versions, toggle services and tail live logs, all from the browser. Click around; this is the real thing.</p>
          </div>

          <div class="win reveal d1">
            <div class="win-bar">
              <span class="win-dots"><i></i><i></i><i></i></span>
              <span class="win-title">
                <img class="win-favicon" :src="withBase('/assets/logo.svg')" alt="" width="14" height="14" />
                servlo.localhost
              </span>
            </div>
            <iframe class="app-frame" :src="withBase('/demo/index.html')" title="Servlo dashboard live demo" loading="lazy"></iframe>
          </div>
        </div>
      </section>

      <!-- ============ COMPARISON ============ -->
      <section id="compare">
        <div class="wrap">
          <div class="sec-head reveal">
            <span class="eyebrow"><span class="dot"></span>Why Servlo</span>
            <h2 class="h-section" style="margin-top:18px">The Herd workflow, without<br/>the Docker tax.</h2>
            <p class="lead">A zero-config shared stack you can drop any project into without touching its files. Here's how it lines up against the tools Linux PHP devs reach for today.</p>
          </div>
          <div class="card cmp-wrap reveal d1" style="padding:8px 8px 0">
            <table class="cmp" id="cmp-table"></table>
          </div>
          <p class="cmp-note reveal d2">Coming from a Mac or from Windows? Herd has no Linux build and Laragon is Windows-only. Servlo gives you both workflows natively, see <a :href="withBase('/getting-started/herd-linux')">Laravel Herd for Linux</a>, <a :href="withBase('/getting-started/laragon-linux')">Laragon for Linux</a> and the <a :href="withBase('/getting-started/comparison')">full comparison</a>.</p>
        </div>
      </section>

      <!-- ============ SERVICES ============ -->
      <section id="services">
        <div class="wrap">
          <div class="sec-head reveal">
            <span class="eyebrow"><span class="dot"></span>Bundled, rootless, on-demand</span>
            <h2 class="h-section" style="margin-top:18px">Every service your app needs.</h2>
            <p class="lead">Toggle them per workspace from the CLI or dashboard. Need something else? Drop a <code class="kbd">Containerfile.servlo</code> to run Node, Python, Ruby or Go alongside your PHP sites.</p>
          </div>
          <div class="svc-grid" id="svc-cards"></div>
        </div>
      </section>

      <!-- ============ QUICK START ============ -->
      <section id="start">
        <div class="wrap">
          <div class="sec-head reveal">
            <span class="eyebrow"><span class="dot"></span>Get started</span>
            <h2 class="h-section" style="margin-top:18px">Live in two commands.</h2>
          </div>
          <div class="steps">
            <div class="step-list reveal d1" id="step-list"></div>
            <div class="step-vis reveal d2">
              <div class="win term">
                <div class="win-bar"><span class="win-dots"><i></i><i></i><i></i></span><span class="win-title" id="step-file">~/code/acme</span></div>
                <div class="cast" id="step-cast" role="img" aria-label="Terminal recording: servlo quick-start commands"></div>
              </div>
            </div>
          </div>
        </div>
      </section>


    </main>

    <!-- ============ FOOTER ============ -->
    <footer class="footer">
      <div class="wrap">
        <div class="footer-grid">
          <div class="footer-brand">
            <a class="brand" href="#top">
              <img class="brand-logo" :src="withBase('/assets/logo.svg')" alt="Servlo logo" width="32" height="32" />
              Servlo
            </a>
            <p>Open-source, Herd-like local PHP development for Linux &amp; macOS. Podman-native, rootless, daemonless.</p>
            <div class="footer-social">
              <a href="https://github.com/realrashid/servlo" target="_blank" rel="noopener" aria-label="GitHub"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23A11.509 11.509 0 0 1 12 5.803c1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222 0 1.606-.014 2.898-.014 3.293 0 .322.216.694.825.576C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12"/></svg></a>
              <a href="https://discord.gg/5JK54s7xCC" target="_blank" rel="noopener" aria-label="Discord"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M20.317 4.3698a19.7913 19.7913 0 00-4.8851-1.5152.0741.0741 0 00-.0785.0371c-.211.3753-.4447.8648-.6083 1.2495-1.8447-.2762-3.68-.2762-5.4868 0-.1636-.3933-.4058-.8742-.6177-1.2495a.077.077 0 00-.0785-.037 19.7363 19.7363 0 00-4.8852 1.515.0699.0699 0 00-.0321.0277C.5334 9.0458-.319 13.5799.0992 18.0578a.0824.0824 0 00.0312.0561c2.0528 1.5076 4.0413 2.4228 5.9929 3.0294a.0777.0777 0 00.0842-.0276c.4616-.6304.8731-1.2952 1.226-1.9942a.076.076 0 00-.0416-.1057c-.6528-.2476-1.2743-.5495-1.8722-.8923a.077.077 0 01-.0076-.1277c.1258-.0943.2517-.1923.3718-.2914a.0743.0743 0 01.0776-.0105c3.9278 1.7933 8.18 1.7933 12.0614 0a.0739.0739 0 01.0785.0095c.1202.099.246.1981.3728.2924a.077.077 0 01-.0066.1276 12.2986 12.2986 0 01-1.873.8914.0766.0766 0 00-.0407.1067c.3604.698.7719 1.3628 1.225 1.9932a.076.076 0 00.0842.0286c1.961-.6067 3.9495-1.5219 6.0023-3.0294a.077.077 0 00.0313-.0552c.5004-5.177-.8382-9.6739-3.5485-13.6604a.061.061 0 00-.0312-.0286zM8.02 15.3312c-1.1825 0-2.1569-1.0857-2.1569-2.419 0-1.3332.9555-2.4189 2.157-2.4189 1.2108 0 2.1757 1.0952 2.1568 2.419 0 1.3332-.9555 2.4189-2.1569 2.4189zm7.9748 0c-1.1825 0-2.1569-1.0857-2.1569-2.419 0-1.3332.9554-2.4189 2.1569-2.4189 1.2108 0 2.1757 1.0952 2.1568 2.419 0 1.3332-.946 2.4189-2.1568 2.4189Z"/></svg></a>
              <a href="https://reddit.com/r/servlo" target="_blank" rel="noopener" aria-label="Reddit"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 0A12 12 0 0 0 0 12a12 12 0 0 0 12 12 12 12 0 0 0 12-12A12 12 0 0 0 12 0zm5.01 4.744c.688 0 1.25.561 1.25 1.249a1.25 1.25 0 0 1-2.498.056l-2.597-.547-.8 3.747c1.824.07 3.48.632 4.674 1.488.308-.309.73-.491 1.207-.491.968 0 1.754.786 1.754 1.754 0 .716-.435 1.333-1.01 1.614a3.111 3.111 0 0 1 .042.52c0 2.694-3.13 4.87-7.004 4.87-3.874 0-7.004-2.176-7.004-4.87 0-.183.015-.366.043-.534A1.748 1.748 0 0 1 4.028 12c0-.968.786-1.754 1.754-1.754.463 0 .898.196 1.207.49 1.207-.883 2.878-1.43 4.744-1.487l.885-4.182a.342.342 0 0 1 .14-.197.35.35 0 0 1 .238-.042l2.906.617a1.214 1.214 0 0 1 1.108-.701zM9.25 12C8.561 12 8 12.562 8 13.25c0 .687.561 1.248 1.25 1.248.687 0 1.248-.561 1.248-1.249 0-.688-.561-1.249-1.249-1.249zm5.5 0c-.687 0-1.248.561-1.248 1.25 0 .687.561 1.248 1.249 1.248.688 0 1.249-.561 1.249-1.249 0-.687-.562-1.249-1.25-1.249zm-5.466 3.99a.327.327 0 0 0-.231.094.33.33 0 0 0 0 .463c.842.842 2.484.913 2.961.913.477 0 2.105-.056 2.961-.913a.361.361 0 0 0 .029-.463.33.33 0 0 0-.464 0c-.547.533-1.684.73-2.512.73-.828 0-1.979-.196-2.512-.73a.326.326 0 0 0-.232-.095z"/></svg></a>
            </div>
          </div>
          <div>
            <h5>Product</h5>
            <a href="#features">Features</a>
            <a href="#dashboard">Web UI</a>
            <a href="#compare">vs Herd / Laragon / DDEV</a>
          </div>
          <div>
            <h5>Resources</h5>
            <a :href="withBase('/getting-started/installation')">Installation</a>
            <a :href="withBase('/getting-started/requirements')">Documentation</a>
            <a :href="withBase('/usage/sites')">Site management</a>
            <a :href="withBase('/getting-started/laravel')">Laravel walkthrough</a>
            <a :href="withBase('/getting-started/herd-linux')">Laravel Herd for Linux</a>
            <a :href="withBase('/getting-started/laragon-linux')">Laragon for Linux</a>
          </div>
          <div>
            <h5>Community</h5>
            <a href="https://github.com/realrashid/servlo" target="_blank" rel="noopener">GitHub</a>
            <a :href="withBase('/changelog')">Changelog</a>
            <a :href="withBase('/getting-started/comparison')">Comparison</a>
            <a :href="withBase('/contributing/building')">Contributing</a>
          </div>
        </div>
        <div class="footer-bot">
          <span>© 2026 Servlo · MIT-licensed open source</span>
          <span class="mono">servlo self-update</span>
        </div>
      </div>
    </footer>

    <!-- ============ DOCS SEARCH ============ -->
    <VPLocalSearchBox v-if="showSearch" @close="showSearch = false" />
  </div>
</template>
