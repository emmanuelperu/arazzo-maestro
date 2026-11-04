# Recipes

Ready-to-copy integrations for `arazzo-maestro`.

## CI workflow (GitHub Actions)

The `lint` job is all you need to fail a pull request on a broken
workflow. The `publish` and `deploy` jobs are optional: they render the
workflow with `view` and put the result on GitHub Pages on every push to
`main`. Each job runs on a fresh runner, which is why `publish` installs
the binary again. One-time setup for the deploy: repository Settings,
Pages, "Build and deployment", Source: "GitHub Actions".

```yaml
# .github/workflows/arazzo.yml
name: arazzo
on:
  pull_request:
  push:
    branches: [main]

permissions:
  contents: read

concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with: { go-version: '1.26' }
      - run: |
          go install github.com/emmanuelperu/arazzo-maestro/cmd/arazzo-maestro@latest
          arazzo-maestro lint workflows/checkout.arazzo.yaml

  publish:
    needs: lint
    if: github.ref == 'refs/heads/main'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with: { go-version: '1.26' }
      - run: |
          go install github.com/emmanuelperu/arazzo-maestro/cmd/arazzo-maestro@latest
          arazzo-maestro view workflows/checkout.arazzo.yaml -o public/
      - uses: actions/upload-pages-artifact@v5
        with: { path: public/ }

  deploy:
    needs: publish
    runs-on: ubuntu-latest
    permissions:
      pages: write
      id-token: write
    environment:
      name: github-pages
      url: ${{ steps.deploy.outputs.page_url }}
    steps:
      - id: deploy
        uses: actions/deploy-pages@v5
```

`view` writes one page per workflow plus an `index.html` listing them,
so `public/` is a complete static site.

## Pre-commit hook

pre-commit passes every staged file matching `files` in a single call;
`lint` accepts several files and prefixes each finding with its file.
Keep the pattern on `.arazzo.yaml` names: an OpenAPI contract stored
next to the workflows would otherwise be linted as an Arazzo file and
fail the hook.

```yaml
# .pre-commit-config.yaml
repos:
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
docker build --build-arg VERSION=0.6.0 \
  -t ghcr.io/emmanuelperu/arazzo-maestro:0.6.0 .

# Lint a file from the current directory
docker run --rm -v "$PWD":/work -w /work \
  ghcr.io/emmanuelperu/arazzo-maestro:0.6.0 \
  lint workflows/checkout.arazzo.yaml

# Render to ./dist/ on the host, owned by you rather than root
docker run --rm -v "$PWD":/work -w /work --user "$(id -u):$(id -g)" \
  ghcr.io/emmanuelperu/arazzo-maestro:0.6.0 \
  view workflows/checkout.arazzo.yaml
```

`-v "$PWD":/work` exposes your current directory inside the container,
and `-w /work` makes it the working directory, so relative paths
(input file, `-o dist/` default) resolve where you expect on the host.
Without `-w`, the container's cwd is `/` and `view`'s default output
(`./dist/`) is written to `/dist/` inside the container, then discarded
by `--rm`. On Linux the container runs as root unless `--user` is set,
so files it writes on the host would otherwise be owned by root.

The image is `FROM scratch` (~20 MB): no shell, no libc, no package
manager. The binary is the entire userland.
