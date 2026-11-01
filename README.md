# ecosyste.ms project search

An experimental search service for ecosyste.ms, built in Go. [search.ecosyste.ms](https://search.ecosyste.ms/) provides a web interface and JSON API for selected projects from [packages.ecosyste.ms](https://packages.ecosyste.ms/).

## How it works

[Bleve](https://blevesearch.com/) provides full-text indexing and ecosystem filtering through its Scorch backend. The HTTP server and HTML templates use Go's standard library. CSS and JavaScript are served as embedded assets, without a frontend build step. Dependency versions are recorded in [go.mod](go.mod).

The server opens a prebuilt index read-only and uses a separate catalogue for descriptions, categories and public identifiers. Package details are read from the catalogue for returned results. Downloads and indexing run through a separate build command; serving requires no database service or search cluster.

Search uses BM25, with exact-name matches placed first among the top 100 candidates. Descriptions use English stemming; names and identifiers are not stemmed. Popularity does not contribute to ranking. The web interface displays up to 100 results, with category suggestions drawn from those candidates. There is no pagination, blank-query browsing or exhaustive facet counting.

## Data

The packages service's corpus exporter supplies selected package records and project assignments. The `snapshot` command requires those assignments; it validates them but does not generate or revise the grouping. Coverage, metadata and assignments can change between exports.

| Archive | Contents | Use |
| --- | --- | --- |
| Publisher corpus | Package records, projects, assignments and checksummed manifests | Input to the `snapshot` command |
| Search bundle (`search.tar.gz`) | Bleve index, serving catalogue and integrity manifests | Extract and run the server |

The server verifies the index/catalogue pairing before serving. `/api/v1/health` reports the snapshot version, creation date and source checksums. Project IDs are snapshot-local and may change between daily builds when upstream assignments change. Use PURLs or registry/package identifiers for references across snapshots.

The public snapshot download URL is pending. Local setup requires a search bundle or a publisher archive URL and its SHA256. There is no automatic `latest` lookup. Tests use synthetic publisher fixtures and run without a production snapshot.

## Run locally

Install Go at the version required by [go.mod](go.mod) or newer, and Make. Extracting a bundle requires `tar` with gzip support. JavaScript tests require Node.js with `node --test`. Local builds and tests do not require Docker, a packages checkout or a database.

From the repository root, build the binary and extract a prebuilt search bundle into a new directory. Replace `/path/to/search.tar.gz` with the bundle you obtained:

```sh
make build
mkdir -p data
mkdir data/local
tar -xzf /path/to/search.tar.gz -C data/local
make serve SNAPSHOT=data/local
```

Open <http://localhost:8100>. Stop the server with Ctrl+C. The extracted directory should contain `index/`, `serving-catalog.ndjson`, `manifest.json` and `index-checksums.json`. Keep these files together and extract new snapshots into separate directories.

To use the compiled binary or choose another address:

```sh
./bin/search explore -index data/local/index -catalog data/local -addr 127.0.0.1:8100
```

If you have a publisher archive rather than a prebuilt bundle, set `CORPUS_URL` to its HTTP(S) URL and `CORPUS_SHA256` to the checksum supplied by the publisher, then run:

```sh
make build
./bin/search snapshot -url "$CORPUS_URL" -sha256 "$CORPUS_SHA256" -output data/release
make serve SNAPSHOT=data/release/snapshot
```

The URL can be a public or signed S3-compatible storage URL. The output directory must not already exist. Preparation validates checksums, memberships and public identifiers, then writes `snapshot/`, a downloadable `search.tar.gz` and `receipt.json`. A failed build removes its staging directory and leaves existing releases unchanged. Building needs more memory than serving.

HTML, CSS, JavaScript and the OpenAPI specification are embedded in the executable. Restart `make serve`, or rebuild and restart the binary, after changing them.

## API

The API base URL is `https://search.ecosyste.ms/api/v1`. The [OpenAPI specification](https://search.ecosyste.ms/openapi.yml) is also [included in the repository](internal/web/openapi.yml).

```sh
curl 'https://search.ecosyste.ms/api/v1/search?q=numpy&ecosystem=pypi&limit=100'
```

Search requires `q`, with these optional parameters:

| Parameter | Behaviour |
| --- | --- |
| `limit` | `10` (default) or `100` |
| `ecosystem` | One case-insensitive value, such as `npm`, `pypi` or `rubygems`; omitted or empty means unfiltered |
| `suggestions` | `true` (default) or `false` |

Ecosystem filtering happens before selecting candidates and does not add to relevance scores. Unknown ecosystems return no results. A project spanning several ecosystems retains all its public identities, so its category suggestions can include other ecosystems. Registry filtering is not supported.

Each search hit contains its name, description, scores and public identity data. The `public` object includes every package membership, PURL, package/repository link and bulk lookup request body. No separate detail lookup is required. `/api/v1/health` is retained for deployment checks.

An abbreviated hit from a search for `@types/node`:

```json
{
  "id": "package:1333965",
  "name": "@types/node",
  "description": "TypeScript definitions for node",
  "identity": {
    "label": "npm · @types/node · npmjs.org",
    "purl": "pkg:npm/%40types/node",
    "url": "https://repos.ecosyste.ms/hosts/GitHub/repositories/definitelytyped%2Fdefinitelytyped",
    "package_count": 1
  }
}
```

The example omits scores and the `public` object. Multi-package projects list their PURLs in `public.packages` rather than selecting one PURL for the project. The application does not enforce request-rate limits; limits for the hosted service have not yet been specified.

## Development

```sh
make test
```

This runs the Go race tests and JavaScript tests. HTTP tests need loopback listener access; the updater tests run only on Linux. The saved publisher fixtures require no Ruby installation, packages checkout or database. [Fixture provenance](internal/cli/testdata/publisher/provenance.json) and the regeneration script are recorded beside the fixtures.

To compare two builds from identical publisher input:

```sh
SEARCH_REBUILD_ARCHIVE=/absolute/path/to/corpus.tar.gz \
  go test ./internal/cli -run '^TestSnapshotRebuildRanking$' -count=1 -timeout=15m -v
```

This builds two temporary snapshots through the CLI and compares 66 queries through the HTTP handler. It rejects score changes and reordered unequal-score results, but allows equal-score ties to reorder or cross the top-100 cutoff. Temporary snapshots are removed after the test. The regular suite uses a small fixture when the environment variable is unset.

For a code change, compare against a running server using the same snapshot:

```sh
SEARCH_SNAPSHOT=/absolute/path/to/snapshot SEARCH_BASELINE_URL=http://127.0.0.1:8100 \
  go test ./internal/cli -run TestExistingSnapshotRegression -v
```

That check compares API responses and HTML as well as rankings. The code is split between `cmd/search` (entry point) and `internal/cli`, `internal/project`, `internal/snapshot`, `internal/search`, `internal/web` and `internal/storage`. Web templates and assets are in `internal/web/assets`.

## Deployment

The Docker image contains the application and web assets; snapshots are mounted read-only. For data updates, the host downloads and validates the publisher archive, builds a snapshot, switches the active directory and restarts the server. The previous snapshot is retained for rollback. Application deployments are separate from data updates. See the [Docker and Dokku deployment guide](docs/deployment.md).

## License

Licensed under the [GNU Affero General Public License v3.0](LICENSE).
