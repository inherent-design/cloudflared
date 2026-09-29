# Cloudflare Tunnel client

Contains the command-line client for Cloudflare Tunnel, a tunneling daemon that proxies traffic from the Cloudflare network to your origins.
This daemon sits between Cloudflare network and your origin (e.g. a webserver). Cloudflare attracts client requests and sends them to you
via this daemon, without requiring you to poke holes on your firewall --- your origin can remain as closed as possible.
Extensive documentation can be found in the [Cloudflare Tunnel section](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel) of the Cloudflare Docs.
All usages related with proxying to your origins are available under `cloudflared tunnel help`.

You can also use `cloudflared` to access Tunnel origins (that are protected with `cloudflared tunnel`) for TCP traffic
at Layer 4 (i.e., not HTTP/websocket), which is relevant for use cases such as SSH, RDP, etc.
Such usages are available under `cloudflared access help`.

You can instead use [WARP client](https://developers.cloudflare.com/warp-client/)
to access private origins behind Tunnels for Layer 4 traffic without requiring `cloudflared access` commands on the client side.


## Fork h2c support

This fork adds `originRequest.h2cOrigin` for prior-knowledge HTTP/2 to cleartext
origins. It is separate from `http2Origin`, which retains its TLS behavior.
Enabling both options, or enabling h2c for a TLS origin, is rejected.

```yaml
protocol: http2
ingress:
  - hostname: grpc.example.com
    service: http://grpc.default.svc.cluster.local:50051
    originRequest:
      h2cOrigin: true
      keepAliveTimeout: 90s
  - service: http_status:404
```

The origin must support cleartext HTTP/2; this mode does not fall back to HTTP/1.1
or support HTTP/1.1 WebSocket Upgrade requests. Plain Unix socket origins also
use the h2c transport. `keepAliveTimeout` closes idle origin connections; it does
not limit an active stream. The h2c transport dials the origin directly and does
not use the HTTP proxy environment variables supported by the ordinary transport.

The edge transport and origin transport are separate. Use `protocol: http2` when
response trailers are required: the QUIC tunnel adapter still drops trailers,
including gRPC status trailers. This fork does not change that wire protocol.

Upstream release `2026.9.3` still lacks this origin mode. Upstream
[PR #1698](https://github.com/cloudflare/cloudflared/pull/1698) proposes different
configuration semantics and was unmerged when reviewed on 2026-09-27. Recheck
upstream support and configuration compatibility before removing the fork.

## Before you get started

Before you use Cloudflare Tunnel, you'll need to complete a few steps in the Cloudflare dashboard: you need to add a
website to your Cloudflare account. Note that today it is possible to use Tunnel without a website (e.g. for private
routing), but for legacy reasons this requirement is still necessary:
1. [Add a website to Cloudflare](https://developers.cloudflare.com/fundamentals/manage-domains/add-site/)
2. [Change your domain nameservers to Cloudflare](https://developers.cloudflare.com/dns/zone-setups/full-setup/setup/)


## Installing `cloudflared`

Downloads are available as standalone binaries, a Docker image, and Debian, RPM, and Homebrew packages. You can also find releases [here](https://github.com/cloudflare/cloudflared/releases) on the `cloudflared` GitHub repository.

* You can [install on macOS](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/downloads/#macos) via Homebrew or by downloading the [latest Darwin amd64 release](https://github.com/cloudflare/cloudflared/releases)
* Binaries, Debian, and RPM packages for Linux [can be found here](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/downloads/#linux)
* A Docker image of `cloudflared` is [available on DockerHub](https://hub.docker.com/r/cloudflare/cloudflared)
* You can install on Windows machines with the [steps here](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/downloads/#windows)
* To build from source, install the required version of go, mentioned in the [Development](#development) section below. Then you can run `make cloudflared`.

User documentation for Cloudflare Tunnel can be found at https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/


## Creating Tunnels and routing traffic

Once installed, you can authenticate `cloudflared` into your Cloudflare account and begin creating Tunnels to serve traffic to your origins.

* Create a Tunnel with [these instructions](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/get-started/create-remote-tunnel/)
* Route traffic to that Tunnel:
  * Via public [DNS records in Cloudflare](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/routing-to-tunnel/dns/)
  * Or via a public hostname guided by a [Cloudflare Load Balancer](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/routing-to-tunnel/public-load-balancers/)
  * Or from [WARP client private traffic](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/private-net/)


## TryCloudflare

Want to test Cloudflare Tunnel before adding a website to Cloudflare? You can do so with TryCloudflare using the documentation [available here](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/do-more-with-tunnels/trycloudflare/).

## Breaking Changes

Removal of CLI flags, environment variables, configuration keys, or commands is a breaking
change. Such removals must be announced in the [Cloudflare Tunnel changelog](https://developers.cloudflare.com/changelog/product/tunnel/)
before the release that removes them.

## Deprecated versions

Cloudflare currently supports versions of cloudflared that are **within one year** of the most recent release. Breaking changes unrelated to feature availability may be introduced that will impact versions released more than one year ago. You can read more about upgrading cloudflared in our [developer documentation](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/downloads/update-cloudflared/).

For example, as of January 2023 Cloudflare will support cloudflared version 2023.1.1 to cloudflared 2022.1.1.

## Development

### Requirements
- [GNU Make](https://www.gnu.org/software/make/)
- [capnp](https://capnproto.org/install.html)
- [go >= 1.26](https://go.dev/doc/install)
- Optional tools:
  - [capnpc-go](https://pkg.go.dev/zombiezen.com/go/capnproto2/capnpc-go)
  - [goimports](https://pkg.go.dev/golang.org/x/tools/cmd/goimports)
  - [golangci-lint](https://github.com/golangci/golangci-lint)
  - [gomocks](https://pkg.go.dev/go.uber.org/mock)

### Build
To build cloudflared locally run `make cloudflared`. Fork container builds use
Go 1.26.8 and download the locked module graph from the public Go proxy; dependencies
are not vendored. To build a Linux container with an explicit version and architecture:

```bash
make VERSION=2026.9.3-h2c.1-dev TARGET_ARCH=arm64 container
make VERSION=2026.9.3-h2c.1-dev TARGET_ARCH=amd64 container
```

When building in a Git worktree nested inside another repository, explicitly select
its Git metadata so Go does not stamp the enclosing repository's revision:

```bash
GIT_DIR="$(git rev-parse --absolute-git-dir)" GIT_WORK_TREE="$PWD" make cloudflared
```

### Test
To locally run the tests run `make test`

### Linting
To format the code and keep a good code quality use `make fmt` and `make lint`.
The fork is validated with golangci-lint 2.11.4; run `make test lint` before committing.

### Fork releases

After reviewing and merging the source, push an existing release tag in the form
`vYYYY.M.P-h2c.N` (for example, `v2026.9.3-h2c.1`). The `H2C Release` workflow also
supports manual dispatch with that tag as both `release_tag` and the workflow
ref (`gh workflow run docker-publish.yml --ref <tag> -f release_tag=<tag>`).
It rejects a dispatch from a different commit, resolves the tag to one commit and
runs Linux race tests, vet and pinned lint against that exact source.

Both Linux amd64 and arm64 image archives must pass Trivy's fixable HIGH/CRITICAL
OS and library vulnerability checks before any image or release asset is
published. Publication loads the checked archives without rebuilding, verifies
their checksums and registry identities, then publishes the versioned
multi-platform image and `latest-h2c`, Linux binaries and `SHA256SUMS`. Individual
architecture tags use `<version>-amd64` and `<version>-arm64`. Release runs share
one concurrency group and are not cancelled once running.

If a pre-publication check fails, fix it and rerun the workflow before updating
consumers. A failure during publication can leave partial registry artifacts;
rerun the same tag to finish publication, then verify both published platforms,
the embedded version, release checksums and immutable image digest. Never move a
released tag. Downstream cfgate references must use a verified published release.

### Mocks
After changes on interfaces you might need to regenerate the mocks, so run `make mocks`

### Git Hooks
To avoid CI errors, you can install pre-push hooks that run linting and tests before each push:
```bash
make install-hooks
```
This will configure git to use the hooks in `.githooks/` that run `make fmt-check lint test` before each push.
