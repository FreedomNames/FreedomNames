# LibreWeb

**[LibreWeb](https://libreweb.org)** is a sister project to Freedom Names: a
user-friendly decentralized-web browser that puts a graphical interface on
everything Freedom Names does under the hood.

Freedom Names is the engine, one small binary that provides self-sovereign names
and peer-to-peer content. LibreWeb is a friendly app built on top of it, for
people who would rather browse and publish than run a command line.

## What LibreWeb adds

- **A real browser UI.** Open `.fn` names and read their pages like any website,
  with no terminal required.
- **A built-in node.** LibreWeb spawns and drives a `freedom-names` node for you
  (see [Embedding a node](/guide/embedding)), so there is nothing separate to
  install or keep running.
- **Publish from the app.** Author a page and point a name at it from inside
  LibreWeb, which uploads the content and publishes the signed record for you.

## How it fits together

LibreWeb talks to its embedded node over the local HTTP API. For each page load it
asks the node to resolve a `.fn` name and stream the content in one request, and
the node handles naming, discovery, and transfer.

The names and content are fully interoperable with the Freedom Names CLI and every
other node on the network: a name published from the CLI opens in LibreWeb, and a
name published from LibreWeb resolves through any node.

## Get LibreWeb

Visit **[libreweb.org](https://libreweb.org)**. For the integration details any
host app uses, see [Embedding a node](/guide/embedding); for more of what people
build on Freedom Names, see the [use cases](/guide/use-cases).
