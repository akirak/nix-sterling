# Contributing Guide

## Updating Dependencies

1. Update flake inputs:
   ```sh
   nix flake update
   ```
2. If `yarn.lock` in `copeanddrag` changed, replace the `yarnDeps` hash in `flake.nix` with `lib.fakeHash`, run `nix build .#sterling-forge`, and update `hash` with the expected SHA-256 reported by Nix.
3. If Forge requires updated or new Racket packages, update the pinned sources in `forge/racket-deps.nix`.
4. If Go dependencies in `sterling/go.mod` change, update `vendorHash` in `sterling/launcher.nix`. Set it to `lib.fakeHash`, run `nix build .#sterling-forge`, and copy the expected SHA-256 from the error message.
