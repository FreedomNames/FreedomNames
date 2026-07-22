# Freedom Names website

The marketing + documentation site for [Freedom Names](https://gitlab.melroy.org/freedom-names/freedom-names),
built with [VitePress](https://vitepress.dev).

## Develop

```sh
npm install
npm run docs:dev
```

Open the printed local URL (default `http://localhost:5173`).

## Build

```sh
npm run docs:build     # outputs static site to docs/.vitepress/dist
npm run docs:preview   # serve the built site locally
```

## Structure

```
docs/
├── index.md                    # home / landing page
├── guide/                      # introduction, get-started, reference, going-further
├── examples/                   # end-to-end recipes
├── public/                     # static assets (logo)
└── .vitepress/
    ├── config.mts              # site config: nav, sidebar, theme
    └── theme/                  # brand colors (teal) on the default theme
```

Content is plain Markdown. Edit the files under `docs/` and the dev server
hot-reloads.

## Release documentation

The Getting Started pages use one concrete Linux amd64 archive name to keep the
download and extraction example easy to copy. When publishing a new release,
update that version in:

- `README.md` at the repository root,
- `docs/guide/what-is-freedom-names.md`, and
- `docs/guide/running-a-node.md`.

Then download that published archive, extract it, and run
`./freedom-names --version` before deploying the documentation.
