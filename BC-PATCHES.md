# BigCommerce Consul patches

`main-bc` is the latest upstream Consul release with the BigCommerce patches on top. It is rebased (not merged) onto each new upstream release, so every patch below is one commit and can be re-evaluated, reworded or dropped at each rebase.

- Current base: `v2.1.0`, rebased 2026-10-27.
- Older bases live on `<major>.<minor>.x-bc` branches (`2.0.x-bc`, `1.22.x-bc`, ...), which are kept for rollback and patch releases.
- Releases are tagged `vX.Y.Z-bc.N`. Pushing the tag makes `bc-build` publish the linux/amd64 zip and `SHA256SUMS` as a GitHub release, which fpm-cookery packages into the consul deb.

Commit SHAs change on every rebase, so refer to patches by subject.

## Current patches

In branch order.

| # | Patch | Kind | Drop when |
|---|-------|------|-----------|
| 1 | connect(bigcommerce): force all egress routes to 0s request timeouts | BC policy | Never, unless timeouts move out of ingress |
| 2 | hardcode maxConnections to 4096 in all upstream configs | BC policy | Every service sets its own limits |
| 3 | set pre-release to easily identify bigcommerce builds | BC build | Never |
| 4 | Add extra logging for requests without ACL tokens | BC feature | Nothing sets `log_acl_info` |
| 5 | Stop RPC materializer retries when ACL token is not found | Bug fix, not upstreamed | Upstream fixes it |
| 6 | chore: Add BC CI workflows | BC build | Never |
| 7 | test: Add BC maxConnections and egress timeouts to new upstream goldens | Test fixup | Regenerated at each rebase |
| 8 | feat: Add a proxy config key for the upstream connection idle timeout | Feature, upstream PR open | [hashicorp/consul#24053](https://github.com/hashicorp/consul/pull/24053) ships |
| 9 | fix: Stop the previous check monitor when a check changes type | Backport, upstream PR open | [hashicorp/consul#24008](https://github.com/hashicorp/consul/pull/24008) ships |

### 1. Egress routes get 0s request timeouts

Chris Boulton, 2021-01. Sets the Envoy route timeout to `0s` (disabled; Envoy's default is 15s) on the upstream routes Consul generates for sidecars and gateways, because BigCommerce enforces request timeouts at ingress.

- Code: `agent/xds/listeners.go`, `listeners_ingress.go`, `routes.go`.
- No global upstream equivalent: `local_request_timeout_ms` only covers the local app, and `RequestTimeout` is per service (`service-resolver`) or per route (`service-router`).
- Rebase: adds `"timeout": "0s"` to many route goldens. Upstream test cases added since the last rebase lack it; see patch 7.

### 2. Upstream clusters default to maxConnections 4096

Chris Boulton, 2021-04. When an upstream has no `Limits`, its discovery-chain clusters get a circuit breaker with `maxConnections: 4096`. `local_app` is excluded. This was added as a temporary step off an earlier custom `UpstreamLimits` implementation, so config entries carrying those limits could be removed.

- Code: `makeUpstreamClustersForDiscoveryChain` in `agent/xds/clusters.go`.
- Upstream equivalent, per service: `service-defaults` `UpstreamConfig.Defaults.Limits.MaxConnections`. Dropping the patch without that drops every upstream without limits to Envoy's default of 1024 connections.
- Rebase: adds a `circuitBreakers.thresholds` block to many cluster goldens; see patch 7.

### 3. `VersionPrerelease = "bcN"`

Chris Boulton, 2021-06. `consul version` reports `vX.Y.Z-bcN`, where N matches the `vX.Y.Z-bc.N` release tag.

- Code: `version/version.go`.
- Release: reset to `bc1` when rebasing onto a new upstream version, and bump N for each further release of the same upstream version. `bc-build` refuses to release a tag whose N doesn't match.

### 4. `log_acl_info` logs requests without an ACL token

Matt Clement, 2024-09. With `log_acl_info = true` in agent config, every HTTP API request without a token is logged at INFO with its URL, user agent and remote address, to find clients that call the agent without a token.

- Code: `agent/http.go`, and `agent/config/{builder,config,runtime}.go`.
- Rebase: the `TestRuntimeConfig_Sanitize` golden gains `"LogACLInfo": false`.

### 5. RPC materializer stops on ACL not found

Daniil Guzanov, 2026-05. On client agents, a streaming health subscription whose token was deleted retried until idle eviction. ACL not found is now terminal for `RPCMaterializer`, as it already was for `LocalMaterializer`.

- Code: `agent/submatview/{materializer,local_materializer,rpc_materializer}.go`, with tests.
- Upstream: no PR or issue found as of 2026-10-27; v2.1.0 still only stops the local materializer. A candidate to send upstream.

### 6. BC CI workflows

Joe Cai, 2026-10. `bc-build` builds the binary and publishes releases on `v*-bc*` tags. `bc-test` runs `go vet ./...` and the unit tests for the packages the patches touch, on pushes and PRs to `main-bc`.

- Code: `.github/workflows/bc-build.yml`, `bc-test.yml`.
- Upstream workflows need HashiCorp runners and secrets, so they are disabled in the repository's Actions settings; see [Upstream workflows](#upstream-workflows).
- When a patch touches a new package, add it to the `--packages` list in `bc-test.yml`. The whole `./agent/` package is too slow for one runner, so its step runs only the tests matching a `-run` pattern (patch 9's check registration and patch 4's HTTP handler wrapper); extend the pattern when a patch changes other code there.
- On a `v*-bc*` tag, `bc-build` fails unless the binary reports the tag's version (`vX.Y.Z-bc.N` → `Consul vX.Y.Z-bcN`).

### 7. Golden fixup for new upstream test cases

Regenerated at each rebase. Patches 1 and 2 change the xds goldens of every test case that exists when they are written; upstream test cases added later lack the BC lines. Since v2.1.0 these are the api-gateway and `transparent-proxy-http-upstream-with-recursors` cases.

At each rebase, if this commit conflicts, discard it and regenerate (see [Rebasing](#rebasing-onto-a-new-upstream-release)). If the regeneration changes nothing, drop the commit.

### 8. `upstream_connection_idle_timeout_ms`

Joe Cai, 2026-10, [bigcommerce/consul PR #900](https://github.com/bigcommerce/consul/pull/900). A proxy-defaults config key that sets `common_http_protocol_options.idle_timeout` on every http, http2 and grpc upstream cluster. Envoy's default is 1h. It closes idle upstream connections before GCP's VPC firewall forgets them (10 minutes), which otherwise turns a draining destination's FIN into a reset and a 503 on the caller's next request. Unset by default.

- Code: `agent/xds/clusters.go`, `agent/xds/config/config.go`, with tests and goldens.
- Upstream: [hashicorp/consul#24053](https://github.com/hashicorp/consul/pull/24053), open. When it ships, check that the key name and semantics match before dropping the patch; if they don't, the proxy-defaults config has to change with the upgrade.

### 9. Stop the previous check monitor when a check changes type

Backport of [hashicorp/consul#24008](https://github.com/hashicorp/consul/pull/24008), open upstream. Re-registering a check ID as a different type left the old monitor running, so two monitors flapped the check and synced each change as a catalog write. Nomad sidecars hit this on reload. v2.1.0 includes [hashicorp/consul#23088](https://github.com/hashicorp/consul/pull/23088), which removes that trigger, but not the leak.

- Code: `agent/agent.go`, `agent/agent_test.go`, `.changelog/24008.txt`.

## Rebasing onto a new upstream release

1. Save the current state as the old version branch, if it isn't already there:

   ```sh
   git fetch origin --tags && git fetch bc
   git push bc main-bc:refs/heads/<old-major>.<old-minor>.x-bc
   ```

2. Re-evaluate each patch against the new release before rebasing. For each one, check whether the release includes the upstream PR, or changes the same code another way:

   ```sh
   git log --oneline <old-tag>..<new-tag> --grep=<upstream PR number>
   git log --oneline <old-tag>..<new-tag> -- <files the patch touches>
   ```

   Note that the old and new tags are usually on different release branches, so `<old-tag>` is not an ancestor of `<new-tag>`.

3. Rebase, dropping the patches that are no longer needed:

   ```sh
   GIT_SEQUENCE_EDITOR="sed -i '' -e 's/^pick <sha>/drop <sha>/'" \
     git rebase -i --onto <new-tag> <old-tag> main-bc
   ```

4. Resolve golden conflicts by taking the new base's version (`git checkout HEAD -- agent/xds/testdata`), then regenerate after the conflicting commit:

   ```sh
   go test ./agent/xds/ -update -count=1
   # Only the BC lines should change:
   git diff -U0 agent/xds/testdata | grep '^[+-] ' | sed 's/^\([+-]\) */\1 /' | sort | uniq -c | sort -rn
   ```

   Expect only `"timeout": "0s"`, `"maxConnections": 4096` with its `circuitBreakers`/`thresholds` wrapper, and trailing commas on the line before them. Commit the result as patch 7.

5. Check that `version/version.go` has `VersionPrerelease = "bc1"`.

6. Test, as CI does:

   ```sh
   go vet -lostcancel=false ./...
   go test -count=1 ./acl/... ./agent/config/... ./agent/proxycfg/... ./agent/structs/... \
     ./agent/submatview/... ./agent/xds/... ./command/connect/envoy/... ./version/...
   go test -count=1 ./agent/ \
     -run 'TestAgent_(AddCheck|AddService|RemoveCheck|RemoveService|UpdateCheck)|TestHTTPAPI|TestHTTP_wrap'
   (cd envoyextensions && go test ./...)
   ```

   On Apple Silicon macOS, packages that import `github.com/shoenig/go-m1cpu` (e.g. `command/connect/envoy`) can crash with a SIGSEGV in its cgo init before any test runs. Run them with `CGO_ENABLED=0`. Don't run the whole `./agent/` package locally; it exhausts RAM.

7. Compare the series with the old one, then force-push with an explicit lease:

   ```sh
   git range-diff <old-tag>..<old-major>.<old-minor>.x-bc <new-tag>..main-bc
   git push --force-with-lease=main-bc:<old main-bc sha> bc main-bc
   ```

8. Check the Actions settings for upstream workflows the new release added (see below), and update this file: the base and the patch list.

## Upstream workflows

Upstream workflows that would run on this fork are disabled in the repository's Actions settings. A new upstream release can add workflows that are enabled by default, so check after each rebase:

```sh
gh workflow list -R bigcommerce/consul --all --json path,state \
  --jq '.[] | select(.state == "active") | .path'
```

Disable anything that isn't a `bc-*` workflow, a `reusable-*` workflow (these only run when called), or deliberately kept:

```sh
gh workflow disable <file>.yml -R bigcommerce/consul
```

- `sha-ancestry-check.yml` (new in v2.1.0): disabled 2026-10-27. It fails on every force-push, which every rebase is.
- `ent-protection.yml` (new in v2.1.0): still active. It runs on every PR and checks for a HashiCorp Jira ID.
