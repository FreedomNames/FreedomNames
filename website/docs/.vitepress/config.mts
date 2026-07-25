import { defineConfig } from 'vitepress'

// Freedom Names: https://gitlab.melroy.org/freedom-names/freedom-names
export default defineConfig({
  lang: 'en-US',
  title: 'Freedom Names',
  description:
    'Decentralized DNS on a libp2p Kademlia DHT. Self-certifying names need no registry or consensus; optional bare names use Bitcoin Cash for uniqueness.',

  cleanUrls: true,
  lastUpdated: true,

  head: [
    ['link', { rel: 'icon', type: 'image/png', href: '/logo.png' }],
    ['meta', { name: 'theme-color', content: '#1abc9c' }],
    ['meta', { property: 'og:type', content: 'website' }],
    ['meta', { property: 'og:title', content: 'Freedom Names: decentralized DNS' }],
    [
      'meta',
      {
        property: 'og:description',
        content:
          'Own a self-certifying name with no registry or consensus, or use a Bitcoin Cash-backed bare name. Records are signed so nobody can overwrite a name they don’t own.',
      },
    ],
    ['meta', { property: 'og:image', content: '/logo.png' }],
  ],

  themeConfig: {
    logo: '/logo.svg',
    siteTitle: 'Freedom Names',

    nav: [
      { text: 'Home', link: '/' },
      { text: 'Guide', link: '/guide/what-is-freedom-names' },
      { text: 'Examples', link: '/examples/' },
      {
        text: 'Reference',
        items: [
          { text: 'CLI', link: '/guide/cli' },
          { text: 'HTTP API', link: '/guide/http-api' },
          { text: 'Configuration', link: '/guide/configuration' },
        ],
      },
    ],

    sidebar: {
      '/guide/': [
        {
          text: 'Introduction',
          collapsed: false,
          items: [
            { text: 'What is Freedom Names?', link: '/guide/what-is-freedom-names' },
            { text: 'Use cases', link: '/guide/use-cases' },
            { text: 'How names work', link: '/guide/how-names-work' },
            { text: 'Architecture', link: '/guide/architecture' },
          ],
        },
        {
          text: 'Get started',
          collapsed: false,
          items: [
            { text: 'Quickstart', link: '/guide/quickstart' },
            { text: 'Run Freedom Names', link: '/guide/running-a-node' },
            { text: 'Your first name', link: '/guide/your-first-name' },
            { text: 'Resolving from your system', link: '/guide/resolving' },
          ],
        },
        {
          text: 'Reference',
          collapsed: false,
          items: [
            { text: 'CLI', link: '/guide/cli' },
            { text: 'HTTP API', link: '/guide/http-api' },
            { text: 'Configuration', link: '/guide/configuration' },
          ],
        },
        {
          text: 'Going further',
          collapsed: false,
          items: [
            { text: 'The content network', link: '/guide/content' },
            { text: 'Embedding a node', link: '/guide/embedding' },
            { text: 'LibreWeb', link: '/guide/libreweb' },
            { text: 'Bare names', link: '/guide/bare-names' },
            { text: 'Testing on a real network', link: '/guide/testing-a-network' },
            { text: 'Known limitations', link: '/guide/limitations' },
            { text: 'FAQ', link: '/guide/faq' },
          ],
        },
      ],
      '/examples/': [
        {
          text: 'Examples',
          items: [
            { text: 'Overview', link: '/examples/' },
            { text: 'Host a website on .fn', link: '/examples/host-a-website' },
            { text: 'Publish a TXT record', link: '/examples/txt-record' },
            { text: 'Rotate your records', link: '/examples/rotate-records' },
            { text: 'Run a bootstrap node', link: '/examples/bootstrap-node' },
            { text: 'Claim a bare name (chipnet)', link: '/examples/claim-a-bare-name' },
          ],
        },
      ],
    },

    socialLinks: [
      { icon: 'gitlab', link: 'https://gitlab.melroy.org/freedom-names/freedom-names' },
      { icon: 'github', link: 'https://github.com/FreedomNames/FreedomNames' },
      { icon: 'telegram', link: 'https://t.me/freedomnames', ariaLabel: 'Telegram' },
      { icon: 'matrix', link: 'https://matrix.to/#/#freedomnames:melroy.org', ariaLabel: 'Matrix chat' },
    ],

    editLink: {
      pattern:
        'https://gitlab.melroy.org/freedom-names/freedom-names/-/edit/main/website/docs/:path',
      text: 'Edit this page on GitLab',
    },

    search: {
      provider: 'local',
    },

    footer: {
      message: 'Released under the AGPL-3.0 License.',
      copyright: 'Copyright © Melroy van den Berg',
    },
  },
})
