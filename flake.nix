{
  description = "Read SMS from an Android phone over adb";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      forAll = f: nixpkgs.lib.genAttrs [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ]
        (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAll (pkgs: {
        default = pkgs.buildGoModule {
          pname = "smsadb";
          version = "0.1.0";
          src = ./.;
          vendorHash = "sha256-gDbKYQmWr9hJ6QUfm4jocCCiNDlMu8CDH/ZX0V9euwE=";
          nativeBuildInputs = [ pkgs.makeWrapper ];
          # suffix, so an adb already on PATH wins and its server version matches
          postInstall = ''
            wrapProgram $out/bin/smsadb --suffix PATH : ${pkgs.lib.makeBinPath [ pkgs.android-tools ]}
          '';
        };
      });

      devShells = forAll (pkgs: {
        default = pkgs.mkShell { packages = [ pkgs.go pkgs.gopls pkgs.golangci-lint ]; };
      });
    };
}
