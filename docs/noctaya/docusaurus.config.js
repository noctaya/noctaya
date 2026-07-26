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
      id: 'v040_alpha1_status',
      content:
        '<strong>v0.4.0-alpha.1</strong> · Production hardening is underway. <a href="/ROADMAP">See status and limitations →</a>',
      backgroundColor: '#ffedf5',
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
          to: '/docs/getting-started',
          label: 'Getting started',
          position: 'left',
        },
        {
          to: '/docs/architecture',
          label: 'Architecture',
          position: 'left',
        },
        {
          to: '/examples',
          label: 'Examples',
          position: 'left',
        },
        {
          to: '/ROADMAP',
          label: 'Roadmap',
          position: 'right',
        },
        {
          href: 'https://github.com/noctaya/noctaya',
          position: 'right',
          className: 'header-github-link',
          'aria-label': 'GitHub repository',
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
            {label: 'Getting started', to: '/docs/getting-started'},
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
