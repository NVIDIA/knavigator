# Contribute to the NVIDIA `knavigator` Project

Want to contribute to the NVIDIA `knavigator` project? Awesome!
This guide covers the development setup, tests, commit messages and pull
requests. Every commit needs a sign-off, as described in
[Sign your work](#sign-your-work). Everyone taking part follows the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Development setup

You need:

- Go 1.27.4 or later, the version in `go.mod`.
- golangci-lint v2.15.0 for `make lint`, the version the Lint step in
  `.github/workflows/go.yml` pins.
- Helm v3.19.0, the version CI pins, if you change the charts under `charts/`.
- Docker, only to build the container image.

The `Makefile` targets:

| Target | What it does |
| --- | --- |
| `make build` | Builds `knavigator`, `klient` and `node-resource-exporter` into `bin/`. |
| `make test` | Runs the unit tests and writes coverage to `coverage.out`. |
| `make coverage` | Runs `make test`, then prints coverage per function. |
| `make lint` | Runs golangci-lint with `.golangci.yml` on the whole tree, as CI does. |
| `make vet` | Runs `go vet ./...`. |
| `make fmt` | Runs `go fmt ./...`. CI fails when this would change a file. |
| `make mod` | Runs `go mod tidy`. |
| `make image-build` | Runs `make build`, then builds a container image named by `IMAGE_REPO` and `IMAGE_TAG`. |
| `make image-push` | Runs `make image-build`, then pushes the image. |
| `make clean` | Removes the binaries from `bin/`. |

## Tests

Run the unit tests before you open a pull request:

    make test

They need no Kubernetes cluster. Add or update tests with each change in
behavior.

The end-to-end tests need a cluster, so they run in CI. The `test-k8s` job in
`.github/workflows/go.yml` creates a kind cluster, deploys KWOK and runs
`tests/ci/test_k8s.sh`. The Nightly workflow (`.github/workflows/nightly.yml`)
runs the scheduler tests in `tests/ci/` for Volcano, JobSet, Kueue and
YuniKorn; maintainers start it, pull requests do not. Chart changes are
checked by `.github/workflows/helm.yml` with `ct lint`, kubeconform and
`helm unittest`.

Most checks do not run on the pull request itself: copy-pr-bot copies it to a
`pull-request/<number>` branch in this repository, and the workflows run on
that branch. The bot copies new commits on its own when they are signed
(`git commit -S`) and come from a vetted contributor; otherwise a maintainer
reviews the change and comments `/ok to test <sha>` to run CI on that commit.

## Commit messages

Commit messages follow
[Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):

    type(scope): description

`type` is one of:

- `feat`: a new feature
- `fix`: a bug fix
- `docs`: documentation only
- `chore`: maintenance that changes no behavior, such as license headers or `OWNERS`
- `refactor`: a code change that neither fixes a bug nor adds a feature
- `style`: formatting only
- `perf`: a performance improvement
- `test`: new or corrected tests
- `build`: the build or its dependencies, such as the `Makefile`, `Dockerfile` or `go.mod`
- `ci`: GitHub Actions workflows and other CI configuration

The scope is optional and names the part of the project the change touches,
such as `engine`, `results` or `charts`. Put `!` right before the colon to mark
a breaking change. The description says what the change does; the body
explains why.

When a pull request is squash merged, its title becomes the subject of the
commit on `main`, so the title follows the same form. The PR title check
(`.github/workflows/pr-title.yaml`) enforces it, and editing the title runs the
check again.

## Sign your work

The sign-off is a simple signature at the end of the description for the patch.
Your signature certifies that you wrote the patch or otherwise have the right
to pass it on as an open-source patch.

The rules are pretty simple, and sign-off means that you certify the DCO below
(from [developercertificate.org](http://developercertificate.org/)):

```
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.
1 Letterman Drive
Suite D4700
San Francisco, CA, 94129

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.

Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

To sign off, you just add the following line to every git commit message:

    Signed-off-by: Joe Smith <joe.smith@email.com>

You must use your real name (sorry, no pseudonyms or anonymous contributions).

If you set your `user.name` and `user.email` using git config, you can sign
your commit automatically with `git commit -s`.

## Pull requests

- Keep each pull request to one concern, and split unrelated changes into
  separate pull requests.
- Open the pull request ready for review, not as a draft. copy-pr-bot does not
  copy drafts (`auto_sync_draft: false` in `.github/copy-pr-bot.yaml`), so a
  draft gets no CI results.
- Fill in the pull request template.
- GitHub requests a review from the code owners listed in `.github/CODEOWNERS`.
