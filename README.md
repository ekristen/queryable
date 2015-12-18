# queryable

Inspired by the guys over at GliderLabs (even the name) and the [resolvable](https://github.com/gliderlabs/resolvable) project, the main resolver code is from that project, a few minor changes were made for default upstream recursors and additional logging.

The purpose of this project is to provide local DNS for docker containers. Containers are discoverable via `.docker` domain by default. 

Like `resolvable` this honors the `DNS_RESOLVES` and `DNS_PORT` env vars that containers can set, like consul. If a container has `DNS_RESOLVES=consul` an upstream nameserver will be registered for all queries that end in `consul`.

I also instrumented the code using codegansta/cli for CLI options.

# Options

* `--recursor`
* `--host-ip`
* `--port`
* `--domain`

# Example

You just want to be able to discover local docker containers by their friendly name or container id.

```
docker run -d \
  --name queryable \
  --restart always \
  -p 0.0.0.0:53:53/udp \
  -v /var/run/docker.sock:/tmp/docker.sock \
  ekristen/queryable \
  --recursor 8.8.8.8
```

You can then update the `/etc/resolv.conf` and point to `eth0` or `docker0` IP address for DNS, all containers will get this by default then. OR you can modify the docker defaults to set `--dns` to either option.

# DNS Entry Examples

All the sub items can be used via DNS to get the IP.

* id: 5df839803dc0250747baa81fb192ca301d91c5c9b5e18420c53956896c710358, name: focused_poincare, ip: 172.17.0.10
  * 5df839803dc0
  * focused_poincare
  * focused_poincare.DOMAIN


# Credits

The main credit goes to [Matt Good](https://github.com/mgood) and [GliderLabs](https://github.com/gliderlabs) and their [resolvable](https://github.com/gliderlabs/resolvable) project. 

The [resolvable](https://github.com/gliderlabs/resolvable) project is licensed under MIT, as is this project.
