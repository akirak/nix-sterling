# nix-sterling

Nix flake for [Cope and Drag (CnD)](https://github.com/sidprasad/copeanddrag) (a spatial-layout fork of the [Sterling](https://github.com/tnelson/Sterling) web visualizer) and [Forge](https://github.com/tnelson/Forge).

All components are built from source in a hermetic Nix sandbox without runtime network access:

- **Forge** is compiled with a pinned set of Racket catalog packages and wrapped with OpenJDK for its bundled Kodkod/Pardinus solvers.
- **Cope and Drag** is built from source for both the Forge and Alloy targets using an offline Yarn cache.
- **Launchers** (`sterling-forge` and `sterling-alloy`) are self-contained Go binaries that serve the compiled web UI and, for Forge models, supervise the Forge Sterling provider process.

## Packages

| Package | Main Program | Description |
| --- | --- | --- |
| `sterling-forge` | `sterling-forge` | CLI that serves the Forge-flavored Cope and Drag UI and runs `.frg` models in Sterling provider mode |
| `sterling-alloy` | `sterling-alloy` | CLI that serves the Alloy-flavored Cope and Drag UI |
| `forge` | `forge` | Self-contained Forge installation (`forge`, `racket`, and `raco` wrapped with Forge and OpenJDK) |
| `copeanddrag-forge` | — | Static web build of Cope and Drag (`build:forge`) |
| `copeanddrag-alloy` | — | Static web build of Cope and Drag (`build:alloy`) |

## Usage

### Run a Forge model in Cope and Drag

```sh
nix run .#sterling-forge -- path/to/model.frg
```

`sterling-forge` starts a local HTTP server for the web UI, launches Forge in Sterling provider mode on a free port, and prints the visualizer URL (e.g. `http://127.0.0.1:<static-port>/?<provider-port>`) to `stdout`.

Additional arguments after `--` are forwarded directly to the underlying `racket` invocation:

```sh
nix run .#sterling-forge -- path/to/model.frg -- -O verbose 2
```

To serve only the Forge UI without starting a Forge process:

```sh
nix run .#sterling-forge -- --serve-only
```

## Launcher CLI Reference

Run one of the packages with `--help`.
