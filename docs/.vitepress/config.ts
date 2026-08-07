import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vitepress'

// GitHub Pages for realrashid/servlo. PRD §0: servlo.sh is unregistered and
// must not be named anywhere until it exists.
const SITE_URL = 'https://realrashid.github.io/servlo'
const OG_IMAGE = `${SITE_URL}/assets/social-preview.png`

// Read the version off the Go source of truth so the structured data can't
// drift behind a release.
const VERSION_GO = fileURLToPath(new URL('../../internal/version/version.go', import.meta.url))
const SOFTWARE_VERSION = readFileSync(VERSION_GO, 'utf8').match(/Version\s*=\s*"([^"]+)"/)?.[1] ?? ''

export default defineConfig({
  title: 'Servlo',
  description: 'Open-source, Herd-like local PHP development for Linux and macOS. Automatic .test domains, HTTPS, per-project PHP and Node, rootless Podman, no Docker daemon.',
  base: '/servlo/',
  lang: 'en-US',
  cleanUrls: true,

  sitemap: {
    hostname: SITE_URL,
  },

  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/assets/logo.svg' }],

    // Display fonts for the home page hero (Archivo + JetBrains Mono)
    ['link', { rel: 'preconnect', href: 'https://fonts.googleapis.com' }],
    ['link', { rel: 'preconnect', href: 'https://fonts.gstatic.com', crossorigin: '' }],
    ['link', { rel: 'stylesheet', href: 'https://fonts.googleapis.com/css2?family=Archivo:wght@400;600;700;800;900&family=Instrument+Sans:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500;600;700&display=swap' }],

    ['meta', { name: 'theme-color', content: '#FF2D20' }],

    // Open Graph
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:site_name', content: 'Servlo' }],
    ['meta', { property: 'og:locale', content: 'en_US' }],
    ['meta', { property: 'og:image', content: OG_IMAGE }],
    ['meta', { property: 'og:image:type', content: 'image/png' }],
    ['meta', { property: 'og:image:width', content: '1499' }],
    ['meta', { property: 'og:image:height', content: '787' }],
    ['meta', { property: 'og:image:alt', content: 'Servlo, local PHP development for Linux and macOS' }],

    // Twitter / X
    ['meta', { name: 'twitter:card', content: 'summary_large_image' }],
    ['meta', { name: 'twitter:image', content: OG_IMAGE }],
    ['meta', { name: 'twitter:image:alt', content: 'Servlo, local PHP development for Linux and macOS' }],

    // Structured data (rich results / knowledge graph)
    [
      'script',
      { type: 'application/ld+json' },
      JSON.stringify({
        '@context': 'https://schema.org',
        '@graph': [
          {
            '@type': 'SoftwareApplication',
            name: 'Servlo',
            applicationCategory: 'DeveloperApplication',
            operatingSystem: 'Linux, macOS',
            description:
              'Open-source, Herd-like local PHP development environment for Linux and macOS: automatic .test domains and HTTPS, per-project PHP 7.4–8.5 and Node, rootless Podman and a built-in Web UI. No Docker daemon, no sudo.',
            keywords:
              'local PHP development, Laravel Herd for Linux, Laragon for Linux, Laragon alternative Linux, .test domains, rootless Podman, PHP-FPM, local development environment',
            url: SITE_URL,
            downloadUrl: 'https://raw.githubusercontent.com/realrashid/servlo/main/install.sh',
            softwareVersion: SOFTWARE_VERSION,
            license: 'https://opensource.org/licenses/MIT',
            image: OG_IMAGE,
            author: { '@type': 'Person', name: 'George Dumitrescu' },
            offers: { '@type': 'Offer', price: '0', priceCurrency: 'USD' },
          },
          {
            '@type': 'WebSite',
            name: 'Servlo',
            url: SITE_URL,
            description:
              'Documentation and downloads for Servlo, the open-source local PHP development environment for Linux and macOS.',
          },
        ],
      }),
    ],
  ],

  transformPageData(pageData, { siteConfig }) {
    const canonicalUrl = `${SITE_URL}/${pageData.relativePath.replace(/\.md$/, '').replace(/index$/, '')}`
    const description = pageData.frontmatter.description ?? pageData.description ?? siteConfig.site.description
    const title = pageData.frontmatter.title ?? pageData.title ?? siteConfig.site.title
    pageData.frontmatter.head ??= []
    pageData.frontmatter.head.push(
      ['link', { rel: 'canonical', href: canonicalUrl }],
      ['meta', { name: 'description', content: description }],
      ['meta', { property: 'og:title', content: title }],
      ['meta', { property: 'og:description', content: description }],
      ['meta', { property: 'og:url', content: canonicalUrl }],
      ['meta', { name: 'twitter:title', content: title }],
      ['meta', { name: 'twitter:description', content: description }],
    )
  },

  themeConfig: {
    logo: '/assets/logo.svg',
    siteTitle: 'Servlo',

    nav: [
      { text: 'Getting Started', link: '/getting-started/requirements' },
      { text: 'Usage', link: '/usage/sites' },
      { text: 'Features', link: '/features/web-ui' },
      { text: 'Configuration', link: '/configuration' },
      { text: 'Reference', link: '/reference/commands' },
      { text: 'Contributing', link: '/contributing/building' },
    ],

    sidebar: {
      '/getting-started/': [
        {
          text: 'Getting Started',
          items: [
            { text: 'Requirements', link: '/getting-started/requirements' },
            { text: 'Installation', link: '/getting-started/installation' },
            { text: 'Quick Start', link: '/getting-started/quick-start' },
          ],
        },
        {
          text: 'Framework walkthroughs',
          items: [
            { text: 'Laravel', link: '/getting-started/laravel' },
            { text: 'Symfony', link: '/getting-started/symfony' },
            { text: 'WordPress', link: '/getting-started/wordpress' },
            { text: 'Containers (Node, Python, Go, …)', link: '/getting-started/containers' },
          ],
        },
        {
          text: 'Add-ons',
          items: [
            { text: 'Services (MongoDB, phpMyAdmin, …)', link: '/getting-started/services' },
          ],
        },
      ],
      '/usage/': [
        {
          text: 'Lifecycle',
          items: [
            { text: 'Start, Stop & Autostart', link: '/usage/lifecycle' },
          ],
        },
        {
          text: 'Sites & Runtimes',
          items: [
            { text: 'Site Management', link: '/usage/sites' },
            { text: 'Site Groups', link: '/usage/site-groups' },
            { text: 'PHP', link: '/usage/php' },
            { text: 'Node', link: '/usage/node' },
            { text: 'Nginx Overrides', link: '/usage/nginx-overrides' },
          ],
        },
        {
          text: 'Services & Data',
          items: [
            { text: 'Services', link: '/usage/services' },
            { text: 'Service updates', link: '/usage/service-updates' },
            { text: 'Service presets', link: '/usage/service-presets' },
            { text: 'Database', link: '/usage/database' },
            { text: 'Disk cleanup', link: '/usage/cleanup' },
          ],
        },
        {
          text: 'Frameworks & Workers',
          items: [
            { text: 'Frameworks', link: '/usage/frameworks' },
            { text: 'Framework Workers', link: '/usage/framework-workers' },
            { text: 'Framework Commands', link: '/features/commands' },
            { text: 'Framework Definitions', link: '/usage/framework-definitions' },
            { text: 'Queue Workers', link: '/usage/queue-workers' },
            { text: 'Healing Failed Workers', link: '/usage/worker-heal' },
          ],
        },
        {
          text: 'Integrations & Migration',
          items: [
          ],
        },
      ],
      '/features/': [
        {
          text: 'UI & AI',
          items: [
            { text: 'Web UI', link: '/features/web-ui' },
            { text: 'Terminal Dashboard', link: '/features/tui' },
          ],
        },
        {
          text: 'Project lifecycle',
          items: [
            { text: 'Project Setup', link: '/features/project-setup' },
            { text: 'Environment Setup', link: '/features/env-setup' },
            { text: 'FrankenPHP runtime', link: '/features/frankenphp' },
          ],
        },
        {
          text: 'Running in production',
          items: [
            { text: 'Production mode', link: '/features/production-mode' },
          ],
        },
        {
          text: 'Networking',
          items: [
            { text: 'HTTPS / TLS', link: '/features/https' },
          ],
        },
      ],
      '/configuration': [
        {
          text: 'Configuration',
          items: [
            { text: 'Overview', link: '/configuration' },
            { text: 'Per-project (.servlo.yaml)', link: '/configuration#per-project-config-servloyaml' },
          ],
        },
      ],
      '/reference/': [
        {
          text: 'Reference',
          items: [
            { text: 'Command Reference', link: '/reference/commands' },
            { text: 'Configuration', link: '/configuration' },
          ],
        },
        {
          text: 'Internals',
          items: [
            { text: 'Directory Layout', link: '/reference/directory-layout' },
            { text: 'Architecture', link: '/reference/architecture' },
          ],
        },
        {
          text: 'Help',
          items: [
            { text: 'Troubleshooting', link: '/troubleshooting' },
          ],
        },
      ],
      '/troubleshooting': [
        {
          text: 'Reference',
          items: [
            { text: 'Command Reference', link: '/reference/commands' },
            { text: 'Configuration', link: '/configuration' },
          ],
        },
        {
          text: 'Internals',
          items: [
            { text: 'Directory Layout', link: '/reference/directory-layout' },
            { text: 'Architecture', link: '/reference/architecture' },
          ],
        },
        {
          text: 'Help',
          items: [
            { text: 'Troubleshooting', link: '/troubleshooting' },
          ],
        },
      ],
      '/contributing/': [
        {
          text: 'Contributing',
          items: [
            { text: 'Building from Source', link: '/contributing/building' },
            { text: 'The Stores', link: '/contributing/stores' },
            { text: 'Pull Requests', link: '/contributing/pull-requests' },
          ],
        },
      ],
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/realrashid/servlo' },
      { icon: 'discord', link: 'https://discord.gg/5JK54s7xCC' },
      {
        icon: {
          svg: '<svg role="img" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><title>Reddit</title><path d="M12 0A12 12 0 0 0 0 12a12 12 0 0 0 12 12 12 12 0 0 0 12-12A12 12 0 0 0 12 0zm5.01 4.744c.688 0 1.25.561 1.25 1.249a1.25 1.25 0 0 1-2.498.056l-2.597-.547-.8 3.747c1.824.07 3.48.632 4.674 1.488.308-.309.73-.491 1.207-.491.968 0 1.754.786 1.754 1.754 0 .716-.435 1.333-1.01 1.614a3.111 3.111 0 0 1 .042.52c0 2.694-3.13 4.87-7.004 4.87-3.874 0-7.004-2.176-7.004-4.87 0-.183.015-.366.043-.534A1.748 1.748 0 0 1 4.028 12c0-.968.786-1.754 1.754-1.754.463 0 .898.196 1.207.49 1.207-.883 2.878-1.43 4.744-1.487l.885-4.182a.342.342 0 0 1 .14-.197.35.35 0 0 1 .238-.042l2.906.617a1.214 1.214 0 0 1 1.108-.701zM9.25 12C8.561 12 8 12.562 8 13.25c0 .687.561 1.248 1.25 1.248.687 0 1.248-.561 1.248-1.249 0-.688-.561-1.249-1.249-1.249zm5.5 0c-.687 0-1.248.561-1.248 1.25 0 .687.561 1.248 1.249 1.248.688 0 1.249-.561 1.249-1.249 0-.687-.562-1.249-1.25-1.249zm-5.466 3.99a.327.327 0 0 0-.231.094.33.33 0 0 0 0 .463c.842.842 2.484.913 2.961.913.477 0 2.105-.056 2.961-.913a.361.361 0 0 0 .029-.463.33.33 0 0 0-.464 0c-.547.533-1.684.73-2.512.73-.828 0-1.979-.196-2.512-.73a.326.326 0 0 0-.232-.095z"></path></svg>',
        },
        link: 'https://reddit.com/r/servlo',
      },
    ],

    footer: {
      message: 'Released under the MIT License.',
      copyright: 'Servlo',
    },

    search: {
      provider: 'local',
    },

    editLink: {
      pattern: 'https://github.com/realrashid/servlo/edit/main/docs/:path',
      text: 'Edit this page on GitHub',
    },
  },
})
