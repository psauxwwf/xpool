# TODO

- [x] 1. Create and maintain this TODO list.
- [x] 2. Add Go module and Cobra/Fang dependencies.
- [x] 3. Move config generation logic into `internal/xpool/configgen`, reusable helpers into `pkg`, and binary startup into `cmd/xpool`.
- [x] 4. Move rotation/controller logic into the package structure.
- [x] 5. Add `configureLogger()` following the `pmgbot` style.
- [x] 6. Add `Taskfile.yml` for build and test commands following `pmgbot`.
- [x] 7. Remove old entrypoints and verify build/tests.

## Fixes

- [x] 8. Make proxy health checks verify full page downloads through dedicated HTTP check routes.
- [x] 9. Verify build and tests after full proxy check changes.

## Xray migration

- [x] 10. Add Xray config structs in `pkg/xray` and remove sing-box structs.
- [x] 11. Rewrite config generation from `proxy.txt` to Xray `config.json`.
- [x] 12. Replace sing-box rotator with Xray runner/controller.
- [x] 13. Add dedicated background check inbounds and ready pool.
- [x] 14. Update Cobra/Fang CLI to the Xray `run` command.
- [x] 15. Update Taskfile for Xray build/test/run tasks.
- [x] 16. Verify gofmt, tests, build, help output, and Xray config validation.
- [x] 17. Remove public `generate` subcommand and keep generation internal to `run`.

## Rename and package split

- [x] 18. Rename module, CLI, and build output to `xpool`.
- [x] 19. Keep `pkg` as small reusable batteries and move app orchestration back into `internal/xpool`.
- [x] 20. Split reusable packages into `pkg/fs`, `pkg/xray`, and `pkg/health`.
- [x] 21. Make `cmd/xpool` import only `internal/xpool`.
- [x] 22. Keep Cobra/Fang/configureLogger in `cmd/xpool/main.go` and expose app logic through `internal/xpool/xpool.go`.

## Operational API

- [x] 23. Add status snapshots for the health pool and controller.
- [x] 24. Add local HTTP `/healthz` and `/status` endpoints.
- [x] 25. Track `healthy` separately from `serving`.
- [x] 26. Add activation gating before balancer override.
- [x] 27. Add a file proxy source abstraction ready for future dynamic subscription refresh.
