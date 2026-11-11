{
  description = "Cope and Drag (CnD), a spatial-layout fork of the Sterling visualizer";

  inputs = {
    copeanddrag = {
      url = "github:sidprasad/copeanddrag/";
      flake = false;
    };

    forge = {
      url = "github:tnelson/Forge";
      flake = false;
    };
  };

  outputs =
    { self, nixpkgs, ... }@inputs:
    let
      eachSystem =
        f:
        nixpkgs.lib.genAttrs nixpkgs.lib.systems.flakeExposed (
          system: f system nixpkgs.legacyPackages.${system}
        );
    in
    {
      packages = eachSystem (
        system: pkgs:
        let
          lib = pkgs.lib;
          nodejs = pkgs.nodejs_22;
          version = (lib.importJSON (inputs.copeanddrag + "/package.json")).version;

          forge = pkgs.callPackage ./forge {
            version = "5.2";
            src = inputs.forge;
            racketDeps = pkgs.callPackage ./forge/racket-deps.nix { };
          };

          # Offline Yarn cache. If yarn.lock changes, update this hash using
          # lib.fakeHash and the mismatch message from `nix build`.
          yarnDeps = pkgs.fetchYarnDeps {
            yarnLock = inputs.copeanddrag + "/yarn.lock";
            hash = "sha256-E/u82ebYNUqONjM0iwxaSM9nONm4dqGVtCNosG8d9+Q=";
          };

          mkWeb =
            flavor: buildScript:
            pkgs.callPackage ./sterling/web.nix {
              nodejs = pkgs.nodejs_22;
              src = inputs.copeanddrag;
              inherit
                version
                yarnDeps
                flavor
                buildScript
                forge
                ;
            };

          mkPackage =
            {
              name,
              web,
              description,
              extraArgs ? "",
              alloy ? null,
              flavor ? null,
            }:
            pkgs.callPackage ./sterling/package.nix {
              alloy = pkgs.alloy6;
              inherit
                name
                web
                description
                extraArgs
                flavor
                version
                ;
            };

          copeanddrag-forge = mkWeb "forge" "build:forge";
          copeanddrag-alloy = mkWeb "alloy" "build:alloy";
        in
        {
          inherit forge copeanddrag-forge copeanddrag-alloy;

          sterling-forge = mkPackage {
            name = "sterling-forge";
            web = copeanddrag-forge;
            description = "CLI for running Forge models with the locally-built Cope and Drag UI";
          };
          sterling-alloy = mkPackage {
            name = "sterling-alloy";
            web = copeanddrag-alloy;
            description = "CLI for running Alloy models with the locally-built Cope and Drag UI";
            alloy = pkgs.alloy6;
          };
        }
      );

      checks = eachSystem (system: _: inputs.self.packages.${system});
    };
}
