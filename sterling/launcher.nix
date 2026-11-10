let
  launcherScript = ./sterling-forge.mjs;
in
{
  lib,
  runCommand,
  version,
  makeWrapper,
  nodejs,
  name,
  web,
  extraArgs ? "",
}:
runCommand "${name}-launcher-${version}"
  {
    nativeBuildInputs = [ makeWrapper ];
  }
  ''
    mkdir -p "$out/bin"
    makeWrapper ${lib.getExe nodejs} "$out/bin/${name}" \
      --add-flags ${launcherScript} \
      --set-default STERLING_DIST "${web}/share/${name}" \
      --set-default STERLING_VERSION "${version}" \
      --set-default STERLING_APP_NAME "${name}" \
      --set-default STERLING_RACKET "${web}/bin/racket" ${extraArgs}
  ''
