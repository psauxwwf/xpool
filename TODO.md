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

## Stability

- [x] 28. Keep status API always enabled on the default local address.
- [x] 29. Validate generated Xray config before starting Xray.
- [x] 30. Batch full-download checks with bounded concurrency and jitter.
- [x] 31. Limit downloaded health-check body size.
- [x] 32. Retire proxies permanently from the check pool after a failed full-download check.
- [x] 33. Skip invalid proxy URLs while tracking invalid source statistics for future subscriptions.
- [x] 34. Expose richer runtime status fields for Xray, uptime, switch timestamps, and check settings.
- [x] 35. Add smoke tests for lenient proxy parsing and retired health-check routes.

## YAML config

- [x] 36. Add `internal/config/config.go` with YAML schema, defaults, validation, and save/load helpers.
- [x] 37. Move operational Cobra flags into YAML config and keep only config/logger flags.
- [x] 38. Add safe default `xpool.yaml` and `--save-config` support.
- [x] 39. Wire app startup through `xpool.RunConfig` so runtime settings come from YAML.
- [x] 40. Convert logical batteries to struct-based entrypoints where useful: config, source file, config generator, and Xray runtime.
