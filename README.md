# Freedom Names

Decentralized DNS (Domain Name System) using DHT, written in Golang.

## Usage

To run a node, just run:

```sh
go run .
```

## Troubleshooting

To avoid warnings for the Quic protocol, increase kernel limit receive buffers:

```sh
sudo sysctl -w net.core.rmem_max=7500000
sudo sysctl -w net.core.wmem_max=7500000
```

Make it paermanent by adding to `/etc/sysctl.conf`:

```conf
net.core.rmem_max=7500000
net.core.wmem_max=7500000
```

## Development

Install [air](https://github.com/air-verse/air) and run (`air` will automatically recompile code on file changes):

```sh
air
```

----

Run a **bootstrap** node:

```sh
go run . bootstrap
```