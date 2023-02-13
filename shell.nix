let
  pkgs = import <nixpkgs> { };
in
  pkgs.mkShell {
    # fix go debugging, see https://github.com/NixOS/nixpkgs/issues/18995
    hardeningDisable = [ "fortify" ];

    buildInputs = with pkgs; [ go ];
  }
