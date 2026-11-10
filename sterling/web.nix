# Build one flavour of the web app with the project's own webpack
# configuration (no release zip is downloaded).
{
  lib,
  stdenv,
  nodejs,
  yarn,
  yarnConfigHook,
  yarnBuildHook,
  src,
  version,
  yarnDeps,
  flavor,
  buildScript,
  forge,
}:
stdenv.mkDerivation {
  pname = "copeanddrag-${flavor}";
  inherit src version;

  nativeBuildInputs = [
    nodejs
    yarn
    yarnConfigHook
    yarnBuildHook
  ];

  yarnOfflineCache = yarnDeps;
  yarnBuildScript = buildScript;

  installPhase = ''
    runHook preInstall

    mkdir -p "$out/share/sterling-${flavor}"
    cp -r dist/. "$out/share/sterling-${flavor}/"

    # Bundle the Forge Racket runtime (including its package scope and Java
    # dependencies) with the web app, so no external Racket installation is
    # needed at run time.
    mkdir -p "$out/bin"
    ln -s ${forge}/bin/racket "$out/bin/racket"

    cat > "$out/share/sterling-${flavor}/version.json" <<EOF
    {
      "name": "Sterling Layout",
      "version": "${version}",
      "build": "${flavor}"
    }
    EOF

    runHook postInstall
  '';

  meta = with lib; {
    description = "${flavor} build of the Cope and Drag visualizer";
    homepage = "https://sidprasad.github.io/copeanddrag/";
    license = licenses.mit;
    platforms = platforms.all;
  };
}
