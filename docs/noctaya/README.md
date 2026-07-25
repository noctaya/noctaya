# Noctaya documentation website

This directory contains the Docusaurus presentation layer for
[noctaya.io](https://noctaya.io). Documentation content remains in the repository
root:

- `docs/**/*.md`;
- `examples/README.md`; and
- `CHANGELOG.md`, `CODE_OF_CONDUCT.md`, `CONTRIBUTING.md`, `ROADMAP.md`, and `SECURITY.md`.

Do not copy those files into `docs/noctaya/`. The Docusaurus docs plugin reads them directly so
code, examples, and documentation can change together.

## Develop locally

Use Node.js 20 or newer:

```bash
cd docs/noctaya
npm ci
npm start
```

The development server watches the canonical Markdown sources as well as the website components
and styles.

## Validate

```bash
cd docs/noctaya
npm run build
npm run serve
```

The production build fails on broken internal links. Local search is generated at build time.
From the repository root, `make test-docs` performs the clean installation and production build
used in CI.

## Publish

`.github/workflows/docs.yml` builds and publishes the site through GitHub Pages after changes land
on `main`. The artifact includes `static/CNAME` for `noctaya.io`; the repository's Pages
settings and DNS records must point the domain to GitHub Pages.
