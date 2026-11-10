{
  lib,
  stdenv,
  go,
  makeWrapper,
  name,
  web,
  version,
  extraArgs ? "",
}:
stdenv.mkDerivation {
  pname = "${name}-launcher";
  inherit version;
  src = ./.;

  nativeBuildInputs = [
    go
    makeWrapper
  ];

  dontConfigure = true;

  buildPhase = ''
    runHook preBuild

    export GOCACHE="$TMPDIR/go-cache"
    export CGO_ENABLED=0
    go build -trimpath -buildvcs=false -o sterling-forge .

    runHook postBuild
  '';

  installPhase = ''
    runHook preInstall

    mkdir -p "$out/bin"
    cp sterling-forge "$out/bin/${name}"
    wrapProgram "$out/bin/${name}" \
      --set-default STERLING_DIST "${web}/share/${name}" \
      --set-default STERLING_VERSION "${version}" \
      --set-default STERLING_APP_NAME "${name}" \
      --set-default STERLING_RACKET "${web}/bin/racket" ${extraArgs}

    runHook postInstall
  '';

  meta = {
    description = "Launcher for the locally-built Cope and Drag visualizer";
    mainProgram = name;
    platforms = lib.platforms.unix;
  };
}
