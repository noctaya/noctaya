const {themes: prismThemes} = require('prism-react-renderer');

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'Noctaya',
  tagline: 'Scale-to-zero LLM serving for private Kubernetes clusters',

  url: 'https://noctaya.io',
  baseUrl: '/',
  organizationName: 'noctaya',
  projectName: 'noctaya',
  trailingSlash: false,

  future: {
    v4: true,
  },

  onBrokenLinks: 'throw',
  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  markdown: {
    mermaid: true,
    hooks: {
      onBrokenMarkdownLinks: 'throw',
    },
  },

  themes: [
    '@docusaurus/theme-mermaid',
    [
      require.resolve('@easyops-cn/docusaurus-search-local'),
      {
        hashed: true,
        language: ['en'],
        docsDir: '../..',
        docsRouteBasePath: '/',
        indexBlog: false,
        indexDocs: true,
        indexPages: true,
        highlightSearchTermsOnTargetPage: true,
      },
    ],
  ],

  presets: [
    [
      'classic',
      {
        docs: {
          path: '../..',
          include: [
            'docs/**/*.md',
            'examples/README.md',
            'CHANGELOG.md',
            'CODE_OF_CONDUCT.md',
            'CONTRIBUTING.md',
            'ROADMAP.md',
            'SECURITY.md',
          ],
          exclude: ['docs/noctaya/**'],
          routeBasePath: '/',
          sidebarPath: require.resolve('./sidebars.js'),
          editUrl: ({docPath}) =>
            `https://github.com/noctaya/noctaya/edit/main/${docPath}`,
          showLastUpdateAuthor: true,
          showLastUpdateTime: true,
        },
        blog: false,
        theme: {
          customCss: require.resolve('./src/css/custom.css'),
        },
        sitemap: {
          changefreq: 'weekly',
          priority: 0.5,
        },
      },
    ],
  ],

  themeConfig: {
    metadata: [
      {
        name: 'keywords',
        content:
          'Kubernetes, LLM serving, scale to zero, KEDA, vLLM, NVIDIA, Ascend',
      },
    ],
    colorMode: {
      defaultMode: 'dark',
      disableSwitch: false,
      respectPrefersColorScheme: true,
    },
    announcementBar: {
      id: 'alpha_status',
      content:
        'Noctaya v0.3.0 is alpha software. See the <a href="/ROADMAP">roadmap and current limitations</a>.',
      backgroundColor: '#fff0f7',
      textColor: '#701a4b',
      isCloseable: true,
    },
    navbar: {
      title: 'Noctaya',
      hideOnScroll: true,
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docsSidebar',
          position: 'left',
          label: 'Docs',
        },
        {
          to: '/examples',
          label: 'Examples',
          position: 'left',
        },
        {
          to: '/docs/architecture',
          label: 'Architecture',
          position: 'left',
        },
        {
          to: '/ROADMAP',
          label: 'Roadmap',
          position: 'left',
        },
        {
          to: '/CONTRIBUTING',
          label: 'Contribute',
          position: 'right',
        },
        {
          href: 'https://github.com/noctaya/noctaya/issues/new/choose',
          label: 'Report issue',
          position: 'right',
        },
        {
          href: 'https://github.com/noctaya/noctaya',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    docs: {
      sidebar: {
        hideable: true,
        autoCollapseCategories: true,
      },
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Learn',
          items: [
            {label: 'Getting started', to: '/docs/started'},
            {label: 'Architecture', to: '/docs/architecture'},
            {label: 'API reference', to: '/docs/crd'},
            {label: 'Examples', to: '/examples'},
          ],
        },
        {
          title: 'Project',
          items: [
            {label: 'Roadmap', to: '/ROADMAP'},
            {label: 'Changelog', to: '/CHANGELOG'},
            {label: 'Security', to: '/SECURITY'},
            {label: 'Contributing', to: '/CONTRIBUTING'},
          ],
        },
        {
          title: 'Community',
          items: [
            {
              label: 'GitHub',
              href: 'https://github.com/noctaya/noctaya',
            },
            {
              label: 'Report an issue',
              href: 'https://github.com/noctaya/noctaya/issues/new/choose',
            },
            {
              label: 'Discussions',
              href: 'https://github.com/noctaya/noctaya/discussions',
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Noctaya contributors. Apache-2.0 licensed.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'go', 'json', 'yaml'],
    },
  },
};

module.exports = config;
