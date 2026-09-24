# objpool — notes for Claude

## Design decisions

- The element contract lives once, in the unexported `factory` (factory.go):
  a nil `newFn` panics at construction, `resetFn` runs on reuse only — never on
  a fresh build — and a reset error drops the element for a fresh build that
  never surfaces the error. Change the contract there, not per type. The panic
  messages name the constructor and are pinned by tests.
- `factory` is a named field of `Pool` and `FreeList`, not an embedded one:
  embedded, `go doc` prints `factory[T]` in both public struct signatures.
- Reset runs in `Get`, not `Put`: a failed reset can then be answered with a
  fresh element. `Pool` leaves `sync.Pool.New` unset so a miss reaches `Get` as
  nil and is built without being reset.
- Both `Get`s keep the hit path free of calls into objpool: `factory.rewind`
  (the reset check) and `factory.build` (the miss path) each fit the inlining
  budget and inline into `Get`. Keep `rewind` that small — it is the hit path —
  and do not fold the two into one helper: together they exceed the budget, and
  every `Get` pays a call. Test `rewind` in its own `if`, nested under the nil
  check: joined to it with `&&`, the compiler materializes the result as a bool
  (`SETE`/`TEST`) on the hit path. Generic methods report inlining only where
  they are instantiated, so check it with `go build -gcflags=-m` in a consuming
  package.
- `FreeList` never evicts. Bounding retained memory is the caller's job at
  release (`RatchetTrim.Due`, then drop instead of `Put`); do not add eviction
  or a trim hook to the list itself.
- `RatchetTrim.wasteful` divides before it multiplies: `used*Factor` overflows
  `int` past 512 MiB where `int` is 32 bits and would wrap into a false strike.
  Do not replace it with the plain product; `Test_unit_RatchetTrim_Wasteful`
  pins it.

## Go version floor

go.mod declares `go 1.18` — generics are the newest language feature the
library uses — and CI tests 1.18. Tests and examples must compile there too:
no range-over-int, `wg.Go`, `min`/`max`, `clear`, `atomic.Int32`-style types or
`slices`/`maps` packages. Use classic `for` loops, and pass loop variables into
goroutines explicitly (pre-1.22 loop-variable semantics apply). A 1.18
toolchain: `go install golang.org/dl/go1.18.10@latest && go1.18.10 download`.

## Testing notes

- Pool tests must not assume a `Put` comes back: under `-race`, `sync.Pool`
  drops a random quarter of Puts on purpose. Cycle until a hit, bounded (see
  `Test_unit_Pool_ResetRewindsRecycledOnly`). FreeList retention is
  deterministic.
- The concurrency tests share `hammer`, which fails if an element is ever held
  by two goroutines at once.

## Verification

- Tests must keep 100% statement coverage; run
  `go test -race -shuffle=on -cover ./...`.
- CI runs `golangci-lint` in a separate job, at the version pinned in
  `.github/workflows/ci.yml`; run `golangci-lint run ./...` locally too (it
  lives in `~/go/bin`, which may not be on `PATH`), and move the pin when the
  local binary moves so the two agree.
- CI auto-tags every green main commit as the next patch version — pushing to
  main is releasing.
