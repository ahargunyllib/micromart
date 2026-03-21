{
  description = "micromart's nix flake";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs?ref=nixos-unstable";
  };

  outputs = { self, nixpkgs }:
  let
    pkgs = nixpkgs.legacyPackages."aarch64-darwin";
  in
  {
    devShells."aarch64-darwin".default = pkgs.mkShell {
      packages = [
        pkgs.go
        pkgs.buf
        pkgs.k6
      ];

      shellHook = ''
        export GOPATH="$HOME/go"
        export GOBIN="$GOPATH/bin"
        export PATH="$GOBIN:$PATH"
      '';
    };
  };
}
