/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
  docsSidebar: [
    {
      type: 'html',
      value: '<span class="sidebar-section-label">Learn</span>',
      defaultStyle: true,
    },
    {
      type: 'doc',
      id: 'docs/index',
      label: 'Overview',
    },
    {
      type: 'doc',
      id: 'docs/started',
      label: 'Getting started',
    },
    {
      type: 'doc',
      id: 'examples/README',
      label: 'Examples and model changes',
    },
    {
      type: 'doc',
      id: 'docs/architecture',
      label: 'Architecture',
    },
    {
      type: 'doc',
      id: 'docs/crd',
      label: 'CRD reference',
    },
    {
      type: 'html',
      value: '<span class="sidebar-section-label">Operate</span>',
      defaultStyle: true,
    },
    {
      type: 'doc',
      id: 'docs/observability',
      label: 'Observability',
    },
    {
      type: 'doc',
      id: 'docs/no-gpu',
      label: 'Develop without an accelerator',
    },
    {
      type: 'doc',
      id: 'docs/demo',
      label: 'Operational walkthrough',
    },
    {
      type: 'category',
      label: 'Hardware validation',
      collapsed: true,
      items: [
        {
          type: 'doc',
          id: 'docs/ascend/ascend-validation',
          label: 'Validation guide',
        },
        {
          type: 'doc',
          id: 'docs/nvidia/a10-validation',
          label: 'NVIDIA A10',
        },
        {
          type: 'doc',
          id: 'docs/ascend/ascend-310p-validation',
          label: 'Ascend 310P',
        },
        {
          type: 'doc',
          id: 'docs/ascend/ascend-910b-validation',
          label: 'Ascend 910B3',
        },
      ],
    },
    {
      type: 'html',
      value: '<span class="sidebar-section-label">Project</span>',
      defaultStyle: true,
    },
    {
      type: 'doc',
      id: 'ROADMAP',
      label: 'Roadmap',
    },
    {
      type: 'doc',
      id: 'CONTRIBUTING',
      label: 'Contributing',
    },
    {
      type: 'doc',
      id: 'SECURITY',
      label: 'Security',
    },
    {
      type: 'doc',
      id: 'CHANGELOG',
      label: 'Changelog',
    },
  ],
};

module.exports = sidebars;
