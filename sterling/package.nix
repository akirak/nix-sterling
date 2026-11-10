{
  symlinkJoin,
  callPackage,
  name,
  version,
  web,
  description,
  extraArgs ? "",
}:
symlinkJoin {
  name = "${name}-${version}";
  paths = [
    web
    (callPackage ./launcher.nix {
      inherit
        name
        version
        extraArgs
        web
        ;
    })
  ];
  meta = web.meta // {
    inherit description;
    mainProgram = name;
  };
}
