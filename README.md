# Freedom Names

Decentralized DNS (Domain Name System) using DHT, written in Golang.

## Usage

Just run:

```sh
go run .
```


Increase kernel limit receive buffers to avoid warnings:

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

Install air and run:

```sh
air
```