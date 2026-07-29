/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
  docsSidebar: [
    {
      type: 'html',
      value: '<span class="sidebar-section-label">Start</span>',
      defaultStyle: true,
    },
    {
      type: 'doc',
      id: 'docs/index',
      label: 'Overview',
    },
    {
      type: 'doc',
      id: 'docs/getting-started',
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
      id: 'docs/troubleshooting',
      label: 'Troubleshooting',
    },
    {
      type: 'doc',
      id: 'docs/no-gpu',
      label: 'Test without an accelerator',
    },
    {
      type: 'category',
      label: 'Hardware validation',
      collapsed: true,
      items: [
        {
          type: 'doc',
          id: 'docs/validation/requirements',
          label: 'Requirements',
        },
        {
          type: 'doc',
          id: 'docs/validation/nvidia/a10',
          label: 'NVIDIA A10',
        },
        {
          type: 'doc',
          id: 'docs/validation/ascend/310p',
          label: 'Ascend 310P',
        },
        {
          type: 'doc',
          id: 'docs/validation/ascend/910b3',
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
