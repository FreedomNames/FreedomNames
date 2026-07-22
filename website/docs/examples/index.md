# Examples

Practical, end-to-end recipes. Each one assumes you have
[Freedom Names running](/guide/running-a-node) on `http://localhost:8420` and the
downloaded `freedom-names` binary available in your current directory. DHT
examples also require the running instance to be connected to at least one peer.

::: info Windows
The command examples use Linux/macOS syntax. In PowerShell, replace
`./freedom-names` with `.\freedom-names.exe`.
:::

<div class="fn-examples">

- [**Host a website on `.fn`**](/examples/host-a-website): point a name at a web
  server and open it in a browser.
- [**Publish a TXT record**](/examples/txt-record): store arbitrary text (SPF,
  verification tokens, a message) under a name.
- [**Rotate your records**](/examples/rotate-records): change what a name points
  at, and understand how the newest signed record wins.
- [**Run a bootstrap node**](/examples/bootstrap-node): stand up a peer others
  join the network through.

</div>

::: tip New to the CLI?
Walk through [**your first name**](/guide/your-first-name) first, since these
examples build on the same `keygen → set → publish → lookup` flow.
:::

<style>
.fn-examples ul { line-height: 2; }
</style>
