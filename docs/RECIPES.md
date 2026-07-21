# Recipes

Ready-to-copy integrations for `arazzo-maestro`.

## CI workflow (GitHub Actions)

```yaml
# .github/workflows/arazzo.yml
name: arazzo
on: [pull_request, push]

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.25' }
      - run: |
          go install github.com/emmanuelperu/arazzo-maestro/cmd/arazzo-maestro@latest
          arazzo-maestro lint workflows/checkout.yaml

  publish:
    needs: lint
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: arazzo-maestro view workflows/checkout.yaml -o public/
      - uses: actions/upload-pages-artifact@v3
        with: { path: public/ }
```

## Pre-commit hook

```yaml
# .pre-commit-config.yaml
- repo: local
  hooks:
    - id: arazzo-lint
      name: arazzo-maestro lint
      entry: arazzo-maestro lint
      language: system
      files: '\.arazzo\.ya?ml$'
```

## Docker

Pull the published image `ghcr.io/emmanuelperu/arazzo-maestro:latest`
(tagged per release), or build it locally from the in-repo
[`Dockerfile`](../Dockerfile):

```bash
# Build locally
docker build --build-arg VERSION=0.4.0 \
  -t ghcr.io/emmanuelperu/arazzo-maestro:0.4.0 .

# Lint a file from the current directory
docker run --rm -v "$PWD":/work -w /work \
  ghcr.io/emmanuelperu/arazzo-maestro:0.4.0 \
  lint workflows/checkout.yaml

# Render to ./dist/ on the host
docker run --rm -v "$PWD":/work -w /work \
  ghcr.io/emmanuelperu/arazzo-maestro:0.4.0 \
  view workflows/checkout.yaml
```

`-v "$PWD":/work` exposes your current directory inside the container,
and `-w /work` makes it the working directory, so relative paths
(input file, `-o dist/` default) resolve where you expect on the host.
Without `-w`, the container's cwd is `/` and `view`'s default output
(`./dist/`) is written to `/dist/` inside the container, then discarded
by `--rm`.

The image is `FROM scratch` (~19 MB): no shell, no libc, no package
manager. The binary is the entire userland.
