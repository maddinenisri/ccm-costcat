# Harness CCM Cost Categories

Go client and CLI for the Harness CCM business-mapping API. Set
`HARNESS_API_KEY` and `HARNESS_ACCOUNT_ID` in your environment. See
`.env.example` for the optional base URL setting.

List only cost category UUIDs and names:

```sh
go run ./cmd/ccm-costcat list -format names -stdout
```

This fetches all pages of `GET /ccm/api/business-mapping` and writes
`out/cost_categories_names.json`. With `-stdout`, the same JSON array is
also printed to stdout; progress messages go to stderr.

```json
[
  {"uuid": "category-uuid-1", "name": "Engineering"},
  {"uuid": "category-uuid-2", "name": "Business Units"}
]
```

Use `-search-key`, `-sort-type`, `-sort-order`, and `-limit` to control
search, sorting, and page size. UUID/name selection happens locally because
the API has no documented field-selection parameter. Empty results produce
`[]`.

The `names` format also works with `get -id <uuid>`. The default `all`
format includes this file along with the other outputs. Run
`go run ./cmd/ccm-costcat --help` for all commands and formats.

Build and check:

```sh
make build
make test
make vet
```
