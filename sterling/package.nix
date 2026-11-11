{
  symlinkJoin,
  callPackage,
  name,
  version,
  web,
  description,
  extraArgs ? "",
  alloy ? null,
  alloyProgram ? "alloy6",
  flavor ? null,
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
        alloy
        alloyProgram
        flavor
        ;
    })
  ];
  meta = web.meta // {
    inherit description;
    mainProgram = name;
  };
}
