# Codex WebSocket response accounting

The `zakstak/CLIProxyAPI` fork owns the producer fix for response accounting and
idle connection retirement. Forge produced branch `fix/codex-websocket-outcomes`
from upstream
`c2f402b45643f0740d70b9010fd7d8d0aab7ef65` (8.0.57).

## Reproduction and intended behavior

A native Codex WebSocket can complete several responses before closing. The
conductor previously recorded one result at connection closure. A subsequent
idle timeout discarded all those successes and recorded one failed request.

The initial regression reproduced `success=0 failed=1` after one completed
response and an injected network read timeout. The patch changed that result to
`success=1 failed=0`. The expanded
`TestCodexDuplexResponseAccountingAfterConnectionLoss` verifies two completions
before idle timeout or EOF, an interrupted second response, and a business
rejection followed by plain or wrapped EOF. It checks results while the
connection is open. Fake upstreams and deterministic error injection avoid
waiting for the five-minute production deadline.

The executor reports response outcomes separately from connection errors. The
conductor counts each terminal response once and preserves failures during
active responses. Idle read timeouts use the existing recoverable connection-loss
path. The five-minute liveness deadline remains in place, and no in-flight
request is replayed. Single-response streams retain their existing accounting.

A business rejection followed by plain EOF also exposed an existing panic:
unconditionally unwrapping EOF discarded its cause. The fix unwraps only the
recoverable turn-loss marker and preserves a plain transport error.

## Validation and delivery

The server builds with Go 1.27.3 on Linux. Targeted race checks pass for response
accounting, idle and active upstream loss, steering error recovery, duplicate
terminal events, ordinary streams, and reused Gin contexts. The complete
executor, auth, and OpenAI handler packages pass in the final full-suite run.

The final full suite fails three tests. Those failures also reproduce on
untouched upstream `c2f402b45643f0740d70b9010fd7d8d0aab7ef65` in the same sandbox:

| Test | Observed failure |
| --- | --- |
| `TestPionMediaRelayBridgesAudioAndDataChannel` | WebRTC DataChannel is not created in the isolated network namespace. |
| `TestInPlaceByteWritesAreReviewed` | Existing `copy` in `internal/kvstore/bolt_store.go` is absent from the review allowlist. |
| `TestSnapshotAuthFileRelativeAuthDir` | The test tries to create a fixture in the read-only source tree. |

An earlier full run also failed
`TestServerResponsesWebsocketReauthorizesEachTurn` during request-log cleanup.
That failure reproduced on untouched upstream and passed in the final run.

Go checks run in a Bubblewrap sandbox with read-only source, a separate writable
module/build cache, private process and network namespaces, and no operator home
or gateway credentials. Dependency download is a separate network-enabled step.
The checkout contains no runtime config, `.env`, or credential files.

The local sandbox runner is
`/home/zak/.cache/cliproxy-fork-20261105/sandbox-go`. For example:

```sh
/home/zak/.cache/cliproxy-fork-20261105/sandbox-go test -p 2 -count=1 \
  -run 'TestCodexDuplexResponseAccountingAfterConnectionLoss|TestWrapStreamResultResponseOutcomes' \
  ./internal/runtime/executor ./sdk/cliproxy/auth
```

The runner and local test logs are Forge artifacts, not portable release files.
GitHub Actions is disabled on the newly created fork; validation above is local.

## Upstream updates

Run `./scripts/sync-upstream.sh` from a clean fork branch to merge the latest
upstream `main` while preserving this patch. [UPSTREAM.md](../UPSTREAM.md)
documents update branches, conflict recovery, and publishing an update PR.
Disposable local Git fixtures pass for retained fork history, repeated updates,
dirty and detached checkouts, unfinished operations, conflict recovery, ignored
file/directory/symlink collisions, fresh-clone remote setup, and fetch failures.
Bash syntax and ShellCheck pass. The server build also passes after adding the
helper; the Go source is unchanged from the validation above.

## Deployment boundary

Pyre remains responsible for gateway packaging and any later rollout. This
producer change does not authorize gateway deployment or a shared-service
restart. Local tests do not establish production recovery or resolve the
separate active-output transport losses observed on November 3.
