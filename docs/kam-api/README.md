# Pinned KAM contract

[FeatureKamApi.md](FeatureKamApi.md) is an identical copy of the document in the KAM repository (`docs/FeatureKamApi.md`, the former `FeatureMonitorApi.md`). KAM owns it. `KAM_COMMIT` records the KAM commit it was copied from.

Links inside the copy point into the KAM repository and resolve there, not here. In particular its supersession notes about the unified listener point at KAM's `FeatureUnifiedApi.md`, and the shipped endpoints at `APIdetails.md`; both live in KAM and are not pinned here.

## Check for drift

```sh
diff ../kam/docs/FeatureKamApi.md docs/kam-api/FeatureKamApi.md
```

## Update

1. Change the contract in the KAM repository first.
2. Copy the file here, update `KAM_COMMIT`, and read the diff.
3. Update `internal/kam` types and `testdata/kam/*.json`, then run `go test ./...`. The fixtures decode with unknown fields rejected, so a new field in the contract fails the tests until the types know it.

## Fixtures

`testdata/kam/*.json` are example responses written by hand from the contract. Replace them with recorded responses from a real node when you can capture its traffic.
