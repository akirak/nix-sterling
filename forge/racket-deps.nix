{ pkgs }:

let
  git =
    url: rev: hash:
    pkgs.fetchgit {
      inherit url rev hash;
    };

  zip =
    url: hash:
    pkgs.fetchurl {
      inherit url hash;
    };
in
[
  # Packages from the Racket package catalog that are not part of the
  # main Racket distribution.  Keep these pinned to the revisions that
  # `raco pkg install ./forge` would select so the build never needs
  # network access.
  {
    name = "actor-lib";
    src = zip "https://download.rhombus-lang.org/releases/current/pkgs/actor-lib.zip" "sha256-XBEJPxetp7cQtg28jENV+92aGq7euN2tR88sgpNsU4A=";
    unpack = true;
  }
  {
    name = "asn1-lib";
    src = "${
      git "https://github.com/rmculpepper/asn1.git" "3cd32b61a68b40ec03bed98cd0c4d4d4f72cacf2"
        "sha256-aNYtnW/usIbqebWXIsWZKylfo83oXUGGJCbLhoaXoJ8="
    }/asn1-lib";
  }
  {
    name = "base64-lib";
    src = "${
      git "https://github.com/rmculpepper/racket-base64.git" "f783f42743b158173c5775b90b5cadcc41f700b3"
        "sha256-SKoi5GD3bHL0ktxEtC7wpzu6epjIEnc7zMtJ2rGe5zU="
    }/base64-lib";
  }
  {
    name = "basedir";
    src = "${git "https://github.com/willghatch/racket-basedir.git"
      "487dfc49ff3268b76dea9aa2011ddfe585da2672"
      "sha256-Ia+wFnUSKMfhot0f8I8TFCPtGEdwy9gnnrnBEgNvSt0="
    }";
  }
  {
    name = "beautiful-racket";
    src = "${
      git "https://gitlab.com/mbutterick/beautiful-racket.git" "7d41a489c6ec6e1f35fdd5b7ee1861cc11ccde19"
        "sha256-aVbv49pLV53gj67a1kWOW41k19QBRwOjP5eMv3Levlg="
    }/beautiful-racket";
  }
  {
    name = "beautiful-racket-demo";
    src = "${
      git "https://gitlab.com/mbutterick/beautiful-racket.git" "7d41a489c6ec6e1f35fdd5b7ee1861cc11ccde19"
        "sha256-aVbv49pLV53gj67a1kWOW41k19QBRwOjP5eMv3Levlg="
    }/beautiful-racket-demo";
  }
  {
    name = "beautiful-racket-lib";
    src = "${
      git "https://gitlab.com/mbutterick/beautiful-racket.git" "7d41a489c6ec6e1f35fdd5b7ee1861cc11ccde19"
        "sha256-aVbv49pLV53gj67a1kWOW41k19QBRwOjP5eMv3Levlg="
    }/beautiful-racket-lib";
  }
  {
    name = "beautiful-racket-macro";
    src = "${
      git "https://gitlab.com/mbutterick/beautiful-racket.git" "7d41a489c6ec6e1f35fdd5b7ee1861cc11ccde19"
        "sha256-aVbv49pLV53gj67a1kWOW41k19QBRwOjP5eMv3Levlg="
    }/beautiful-racket-macro";
  }
  {
    name = "binaryio-lib";
    src = "${
      git "https://github.com/rmculpepper/binaryio.git" "e949401e3acd7aa51ffe044cb75288128ce64c61"
        "sha256-wxEqfbAcnZNhB2YXiMTadpilHWcaGBfXtDjYYQ/VtbQ="
    }/binaryio-lib";
  }
  {
    name = "br-parser-tools-doc";
    src = "${
      git "https://gitlab.com/mbutterick/br-parser-tools.git" "95b7c69cf9d660a51abf4742378b9adb7100d25a"
        "sha256-and0y3rBjXwmgaEwwXzJOTgX/wCSY0uUfB3+U4JLTrk="
    }/br-parser-tools-doc";
  }
  {
    name = "br-parser-tools-lib";
    src = "${
      git "https://gitlab.com/mbutterick/br-parser-tools.git" "95b7c69cf9d660a51abf4742378b9adb7100d25a"
        "sha256-and0y3rBjXwmgaEwwXzJOTgX/wCSY0uUfB3+U4JLTrk="
    }/br-parser-tools-lib";
  }
  {
    name = "brag";
    src = "${
      git "https://gitlab.com/mbutterick/brag.git" "30cbf95e6a717e71fb8bda6b15a7253aed36115a"
        "sha256-NJctskWDoBNRdBMDklALkMAPKT4A7on8pu6X3Q6NheE="
    }/brag";
  }
  {
    name = "brag-lib";
    src = "${
      git "https://gitlab.com/mbutterick/brag.git" "30cbf95e6a717e71fb8bda6b15a7253aed36115a"
        "sha256-NJctskWDoBNRdBMDklALkMAPKT4A7on8pu6X3Q6NheE="
    }/brag-lib";
  }
  {
    name = "crypto-lib";
    src = "${
      git "https://github.com/rmculpepper/crypto.git" "713eaaf45a5e6c55bd3e97a32b2a90f61ef13c4f"
        "sha256-6Y4WZ494vNxY3/WOq7Dmi7SpfYqMsqVvG8SyqXUTU7g="
    }/crypto-lib";
  }
  {
    name = "debug";
    src = "${git "https://github.com/AlexKnauth/debug.git" "2d089fcfb76c4e0524669ef7ce8efeef489d1243"
      "sha256-p6XFkvzwgrUDOXHz9rcmFvu9mfi/rIYYNXiwW4ysr64="
    }";
  }
  {
    name = "fancy-app";
    src = "${git "https://github.com/samth/fancy-app.git" "f451852164ee67e3e122f25b4bce45001a557045"
      "sha256-2DdngIyocn+CrLf4A4yO9+XJQjIxzKVpmvGiNuM7mTQ="
    }";
  }
  {
    name = "gmp-lib";
    src = "${
      git "https://github.com/rmculpepper/racket-gmp.git" "768c33615a1c2414ccaf1a1e4ea1064bd5dd46af"
        "sha256-Z09YdkD6nzwF51CjF7405cf4ZGR/OGAnYJrVvbXidEU="
    }/gmp-lib";
  }
  {
    name = "hash-view-lib";
    src = "${
      git "https://github.com/rmculpepper/racket-hash-view.git" "7a9b31d1715c40c205a065d666fcc74d840a8a5e"
        "sha256-pKp/sVd/xNOIbZsRwg1yRi5Xr+7V5DJqr3QxsM6MLhI="
    }/hash-view-lib";
  }
  {
    name = "http-easy";
    src = zip "https://download.rhombus-lang.org/releases/current/pkgs/http-easy.zip" "sha256-PkrrUzYsMG7cK4VP8T/LWWxnYPdzD+71+cyFBZ1Mw0M=";
    unpack = true;
  }
  {
    name = "http-easy-lib";
    src = zip "https://download.rhombus-lang.org/releases/current/pkgs/http-easy-lib.zip" "sha256-F3+Yvxqd8VHMvoRjJvLrWw6m3KzcWrPtROUHNCLf3ms=";
    unpack = true;
  }
  {
    name = "mischief";
    src = "${git "https://github.com/carl-eastlund/mischief.git"
      "c6f95a774b60950cabd7238e639f7e5f0d8737cd"
      "sha256-L8TP8BnA9LhqFia2mIrIk3TPAfVRwDv+HeKgLWaTe5Y="
    }";
  }
  {
    name = "predicates";
    src = "${git "https://github.com/jackfirth/predicates.git"
      "0c0baa3c6871ae39c9b94af1591331d30fced968"
      "sha256-8bGjhNs8LQOmZysv/GvNKAahKfgnP9V9HECg0IYEGpM="
    }";
  }
  {
    name = "pretty-format";
    src = "${git "https://github.com/AlexKnauth/pretty-format.git"
      "59c03bb6123fcd898e5143b451e6c9a8b89f7f8b"
      "sha256-0LENO8OBd4cwAzSLf4KnWaGRSB/F3sMDXoAnpR7xf1Y="
    }";
  }
  {
    name = "request";
    src = "${git "https://github.com/jackfirth/racket-request.git"
      "d010a60ad227a540f40b4707cd570d101d5530b3"
      "sha256-E5FjZ8amw1MfFNBIq1Hkm3emnlF5mgmWSzt6bfhgq+U="
    }";
  }
  {
    name = "resource-pool-lib";
    src = zip "https://download.rhombus-lang.org/releases/current/pkgs/resource-pool-lib.zip" "sha256-cGIJqYATPFpifZDHyxQafPgx57j/7bVbHMMRg8d9HPU=";
    unpack = true;
  }
  {
    name = "scramble-lib";
    src = "${
      git "https://github.com/rmculpepper/racket-scramble.git" "a2d1dfd8d249c63059bed2958e71dbb6e9098319"
        "sha256-GbniMHYktaaXLbc/CHNRxQvzisc2u5NAsUH2E4ZLYeY="
    }/scramble-lib";
  }
  {
    name = "sha";
    src = "${git "https://github.com/greghendershott/sha.git" "f9472bea67e05e8c11dc915e943038d3a7276a9b"
      "sha256-FgQbcKnG67DZeU0Owa4mP7pwGmnW4J8fozd5etZbsxQ="
    }";
  }
  {
    name = "sugar";
    src = "${git "https://gitlab.com/mbutterick/sugar.git" "c90834b77afad07f9b02986fc8e157ccf30b753c"
      "sha256-hXcKYCxOBpj5TMU2h25UI9QDBB9MvLuODwUvirRlQro="
    }";
  }
  {
    name = "syntax-classes";
    src = "${
      git "https://github.com/lexi-lambda/syntax-classes.git" "4d64d35259d69ee8d5c90f475fe777c66d54c8fd"
        "sha256-00A4fPrg/Mv8OljC8JvghB/scd7xFAP0d/tvBMUcDgo="
    }/syntax-classes";
  }
  {
    name = "syntax-classes-doc";
    src = "${
      git "https://github.com/lexi-lambda/syntax-classes.git" "4d64d35259d69ee8d5c90f475fe777c66d54c8fd"
        "sha256-00A4fPrg/Mv8OljC8JvghB/scd7xFAP0d/tvBMUcDgo="
    }/syntax-classes-doc";
  }
  {
    name = "syntax-classes-lib";
    src = "${
      git "https://github.com/lexi-lambda/syntax-classes.git" "4d64d35259d69ee8d5c90f475fe777c66d54c8fd"
        "sha256-00A4fPrg/Mv8OljC8JvghB/scd7xFAP0d/tvBMUcDgo="
    }/syntax-classes-lib";
  }
]
