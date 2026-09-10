# unscribd

`unscribd` downloads Scribd document pages as PDFs. It includes a command-line tool and a small web interface.

## Requirements

- Go 1.22 or later for local builds
- `woff2` when producing PDFs with embedded Scribd font subsets; without it, the PDF renderer uses its built-in fallback font

## Build

```bash
make build
```

## Web app

```bash
PORT=10000 ./bin/unscribd-web
```

Open [http://localhost:10000](http://localhost:10000), paste a Scribd document URL, and wait for the download. The app processes one job at a time and shows its queue position.

## Container

```bash
podman build -t unscribd .
podman run --rm -p 10000:10000 -e PORT=10000 unscribd
```

For a resource-constrained local test:

```bash
podman run --rm -p 10000:10000 \
  --cpus=0.1 --memory=512m --memory-swap=512m \
  --tmpfs /tmp:rw,size=256m,mode=1777 \
  -e PORT=10000 unscribd
```

The container stores jobs and its cache in `/tmp`, so all downloaded files are temporary and disappear when the container restarts.

## CLI

```bash
./bin/unscribd 'https://www.scribd.com/document/1001716572/Wai-Crochets-Flower-Bunny-Eng-compressed'
```

Examples:

```bash
# Choose an output location.
./bin/unscribd -o ./downloads '<Scribd URL>'

# Create text, image, or page-source output instead of a searchable PDF.
./bin/unscribd -format text '<Scribd URL>'
./bin/unscribd -format images '<Scribd URL>'
./bin/unscribd -format jsonp '<Scribd URL>'
```

Run `./bin/unscribd -help` for all options.

## Configuration

The CLI accepts `-token`, `-no-token`, `-cache-dir`, `-cache-bytes`, and `-concurrency`. The following environment variables provide defaults or limits:

| Variable | Purpose |
| --- | --- |
| `UNSCRIBD_CACHE_DIR` | Cache location when `-cache-dir` is not set. |
| `UNSCRIBD_CACHE_BYTES` | Maximum cache size in bytes when `-cache-bytes` is not set. |
| `UNSCRIBD_MAX_CONCURRENCY` | Limit for page and asset requests. |
| `UNSCRIBD_PROFILE` | Enable timing and memory logging. |
| `UNSCRIBD_WEB_OUTPUT_DIR` | Directory used by the web app for jobs and its cache. |
| `PORT` | Web server port; defaults to `8000` locally. |

## Development

```bash
make test
make build
```

`default.pgo` is optional. When present, builds use it for profile-guided optimization; builds work normally without it.
