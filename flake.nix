{
  description = "lastfm-golang: dump Last.fm scrobbles locally (JSONL + SQLite)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in {
        packages.default = pkgs.buildGoModule {
          pname = "lastfm-golang";
          version = "0.1.0";
          src = ./.;
          subPackages = [ "cmd/lastfm-golang" ];
          vendorHash = "sha256-h6cHghxBPGqLh80r5q8zipjBOUZdtbPpGlVEH/AYvhI=";
        };

        apps.default = flake-utils.lib.mkApp {
          drv = self.packages.${system}.default;
        };
      }
    );
}
