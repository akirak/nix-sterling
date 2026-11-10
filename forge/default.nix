{
  lib,
  stdenv,
  racket,
  jdk,
  makeWrapper,
  unzip,
  racketDeps,
  src,
  version,
}:

stdenv.mkDerivation {
  pname = "forge";
  inherit version;

  inherit src;

  nativeBuildInputs = [
    racket
    makeWrapper
    unzip
  ];

  dontConfigure = true;
  dontPatch = true;

  buildPhase = ''
    runHook preBuild

    export HOME="$TMPDIR/home"
    export PLTUSERHOME="$out/share/racket-user"
    mkdir -p "$HOME" "$out/share/racket/pkgs" "$PLTUSERHOME"

    # Put the pinned Racket dependencies in the user package scope.  The
    # packages are installed from local directories, so raco never needs
    # network access while building.
    ${lib.concatMapStringsSep "\n" (
      dep:
      if dep.unpack or false then
        ''
          mkdir -p "$out/share/racket/pkgs/${dep.name}"
          unzip -q ${dep.src} -d "$out/share/racket/pkgs/${dep.name}"
        ''
      else
        ''
          cp -a ${dep.src} "$out/share/racket/pkgs/${dep.name}"
        ''
    ) racketDeps}

    # Archives from the Racket catalog may contain precompiled files that
    # were built with a different Racket installation.  Recompile everything.
    find "$out/share/racket/pkgs" -type d -name compiled -prune -exec rm -rf {} +

    cp -a forge "$out/share/racket/pkgs/forge"

    # Sources copied out of the Nix store are read-only, but raco setup needs
    # to create compiled/ directories next to them.
    chmod -R u+w "$out/share/racket/pkgs"

    # Install the Forge package (and the packages it depends on) into the
    # user scope.  raco compiles the Racket sources as part of installation.
    raco pkg install --batch --no-docs "$out/share/racket/pkgs/"*

    runHook postBuild
  '';

  installPhase = ''
    runHook preInstall

    mkdir -p "$out/bin"

    # Forge shells out to Java for its bundled Kodkod/Pardinus solvers.
    # Make the wrapped Racket binaries self-contained: PLTUSERHOME points at
    # the package installation above, and Java is available on PATH.
    for exe in racket raco; do
      makeWrapper ${racket}/bin/"$exe" "$out/bin/$exe" \
        --set PLTUSERHOME "$out/share/racket-user" \
        --prefix PATH : ${lib.makeBinPath [ jdk ]}
    done

    # Convenience entry point: `forge file.frg` is equivalent to
    # `racket file.frg` with the Forge package available.
    makeWrapper ${racket}/bin/racket "$out/bin/forge" \
      --set PLTUSERHOME "$out/share/racket-user" \
      --prefix PATH : ${lib.makeBinPath [ jdk ]}

    runHook postInstall
  '';

  doCheck = false;
  doInstallCheck = false;

  meta = {
    description = "Forge: a tool and language for teaching formal methods";
    homepage = "https://forge-fm.org";
    license = lib.licenses.mit;
    mainProgram = "forge";
    platforms = lib.platforms.unix;
  };
}
