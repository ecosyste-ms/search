# Docker and Dokku deployment

The application image contains code and web assets. Snapshot files are supplied through a read-only mount, and the server runs as UID/GID 65532. It validates the selected snapshot at startup and opens the index read-only, without downloading or indexing data.

Build and deploy the image when the application changes. Data updates run on the host and restart the application with a newly built snapshot. The packages publisher needs storage write credentials; it does not need access to Dokku. Search needs a public HTTP archive URL or a signed download URL.

## Image and initial snapshot

Build the application image from the repository root:

```sh
docker build -t ecosystems-search:local .
IMAGE=$(docker image inspect --format '{{.Id}}' ecosystems-search:local)
```

The Dockerfile pins its base images and excludes datasets from the build context. Its default command serves `/snapshots/current/snapshot` on port 8080. `GO_IMAGE` and `RUNTIME_IMAGE` build arguments can select different digest-pinned base images.

The commands below run on a Linux Dokku host and assume an existing Dokku app named `search`. The account running updates must be able to run Docker and Dokku and write to the snapshot root. Set `CORPUS_URL` and `CORPUS_SHA256` to the publisher archive URL and its supplied SHA256. The [README](../README.md) explains the distinction between publisher archives and ready-to-serve bundles.

```sh
ROOT=/var/lib/dokku/data/storage/search-snapshots
mkdir -p "$ROOT/releases"
docker run --rm --read-only --user "$(id -u):$(id -g)" --env GOMEMLIMIT=2GiB \
  --mount "type=bind,src=$ROOT/releases,dst=/output" \
  "$IMAGE" snapshot -url "$CORPUS_URL" -sha256 "$CORPUS_SHA256" -output /output/initial
ln -s releases/initial "$ROOT/current"
dokku storage:mount search "$ROOT:/snapshots:ro"
dokku docker-options:add search deploy,run "--read-only"
```

Use a new release directory for each build. The output contains `snapshot/`, `search.tar.gz` and `receipt.json`; the `current` symlink points to the release directory, not its `snapshot/` child. Ensure UID 65532 can traverse the host directories and read the snapshot files. The build process needs more memory than serving; `GOMEMLIMIT=2GiB` is a Go garbage-collector target, not a hard container limit.

Deploy the image through your normal Dokku process. Mount the parent directory rather than the `current` symlink itself, since Docker resolves a bind-mounted symlink when creating the container. Dokku documents [storage mounts](https://dokku.com/docs/advanced-usage/persistent-storage/) and [container options](https://dokku.com/docs/advanced-usage/docker-options/).

Check `/api/v1/health` after deployment. It returns the project count, export creation date, package source checksum and source manifest checksum. `snapshot.version` is the publisher archive SHA256.

## Updates and rollback

Run the updater on the host with the new publisher URL and checksum. It selects the image of the running Dokku application. Set `HEALTH_URL` to the app's externally reachable `/api/v1/health` URL:

```sh
scripts/update-snapshot search "$ROOT" \
  "$CORPUS_URL" "$CORPUS_SHA256" "$HEALTH_URL"
```

For a ready-to-serve bundle, pass `--bundle` and the bundle's SHA256:

```sh
scripts/update-snapshot --bundle search "$ROOT" \
  "$BUNDLE_URL" "$BUNDLE_SHA256" "$HEALTH_URL"
```

Bundle activation verifies the archive checksum, rejects unexpected paths and links, and opens the snapshot in a separate read-only container before switching. This startup check validates the index/catalogue pairing as the non-root application user. It avoids rebuilding a prebuilt index. The bundle checksum identifies the release directory; the health endpoint reports the source snapshot version recorded inside it.

The script requires Bash, Docker, Dokku, curl, jq and Linux `flock`, plus permission to manage Docker and write the snapshot root. Run it with `sudo` when needed. It serializes updates, prepares a separate directory, switches `current`, and runs `dokku ps:restart`. It then checks that health reports the expected snapshot. A failed restart or health check restores the old pointer and attempts another restart; rollback errors are reported. The previous release remains available through `previous`. `SEARCH_IMAGE` can override image selection with an image ID or digest, but it must match the running application at activation time.

Schedule this command on the host once snapshot publication is available. The repository installs no scheduler or automatic production deployment. Keep application deployments and snapshot updates from running concurrently. A host crash or forced kill can interrupt rollback, so inspect `current` and `/api/v1/health` before retrying. Failed activation directories are retained; retrying the same checksum requires inspecting and moving the failed directory aside first.

For manual rollback:

```sh
ln -s "$(readlink "$ROOT/previous")" "$ROOT/.rollback-next"
mv -Tf "$ROOT/.rollback-next" "$ROOT/current"
dokku ps:restart search
```

Check `/api/v1/health` again. Old releases are not removed automatically; retain the active and rollback snapshots and never remove files while a server is using them.

## Publisher validation

The importer requires `manifest.json`, `packages.ndjson.gz`, `projects.ndjson.gz`, `assignments.ndjson.gz`, `assignment-manifest.json` and `excluded.json`. It also accepts `corpus.json`, `identity.json`, `selection.json` and `selection.ndjson.gz` when declared in the manifest. Every archive file except the manifest must have a declared size and SHA256. Unknown entries, paths, links and duplicate entries are rejected.

Preparation checks archive and file checksums, project counts, unique package memberships, registry/package names and explicit PURLs. Null PURLs remain null. Required fields and supported grouping reasons are checked; the exporter has no numbered schema. Obtain the archive checksum through a trusted channel. Archive URLs and credentials are not stored in the output.

Project assignments are used as supplied. The assignment manifest's `as_of` supplies the activity date when present; otherwise the export finish date is used. Historical `previous_project_id` values do not determine current grouping and cannot be checked against an absent historical corpus. Refreshing metadata does not verify upstream identity.

Snapshot preparation reads `signals.dependent_repos_count` from package records. It calculates each positive count's percentile within its ecosystem in the selected corpus, using `(number of smaller positive counts + 1) / number of positive counts`. Tied counts share a percentile; missing and zero counts stay zero. Negative counts are rejected. Each project's highest member-package percentile is stored as the optional `dependent_repos_percentile` catalogue field, outside the Bleve index. Search uses it for RRF within the retrieved candidates; tied positive values share a rank and zero values receive no popularity contribution. Older catalogues without the field remain readable and retain BM25 scores and ordering, with exact-name matches first.

Read-only serving prevents segment merges during use. Rebuilding identical source data can still change Bleve's segment layout, scores and equal-score ordering. The repeated-build check documented in the README can detect ranking changes before selecting a new release.
