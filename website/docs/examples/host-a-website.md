# Host a website on `.fn`

Goal: make `blog.<pubKeyID>.fn` open your web server in a browser.

## 1. Have a web server somewhere

Anything that serves HTTP works — a box at `203.0.113.20`, a home server, a VPS.
Note its IP address.

## 2. Create a name for it

```sh
freedom keygen blog
freedom name blog
# blog.<pubKeyID>.fn
```

## 3. Point the name at the server

```sh
freedom set blog A 203.0.113.20 300
```

If your server also has IPv6:

```sh
freedom set blog AAAA 2001:db8::20 300
```

## 4. Publish

```sh
freedom publish blog
# Published blog.<pubKeyID>.fn (seq …, 1 record(s))
```

## 5. Make your system resolve `.fn`

For a browser to open the name, your machine has to send `.fn` queries to a
Freedom Names node. Follow [Resolving from your
system](/guide/resolving) — for a quick check without changing system settings:

```sh
dig @127.0.0.1 -p 53 blog.<pubKeyID>.fn A
# should return 203.0.113.20
```

## 6. Open it

Once your system resolver is pointed at the node, visit:

```
http://blog.<pubKeyID>.fn
```

Your web server sees a normal HTTP request — Freedom Names only handled the
name→IP resolution.

::: warning HTTPS
Publicly-trusted TLS certificates are issued for names in the conventional DNS
hierarchy, so a CA won't issue a cert for a `.fn` name. For HTTPS today you'd use a
self-signed certificate (and accept the browser warning) or terminate TLS via a
conventional hostname. Plain HTTP works out of the box.
:::

## Next

- [Rotate the record](/examples/rotate-records) when your server's IP changes.
- Add a [TXT record](/examples/txt-record) for verification or metadata.
