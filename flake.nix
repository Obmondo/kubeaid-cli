{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
          config.allowUnfree = true;
        };
      in
      with pkgs;
      {
        devShells.default = mkShell {
          buildInputs =
            let
              clusterawsadm = pkgs.stdenv.mkDerivation rec {
                pname = "clusterawsadm";
                version = "v2.7.1";

                src = pkgs.fetchurl {
                  url =
                    "https://github.com/kubernetes-sigs/cluster-api-provider-aws/releases/download/${version}/"
                    + (if pkgs.stdenv.isDarwin then "clusterawsadm-darwin-arm64" else "clusterawsadm-linux-amd64");

                  sha256 = "sha256-J4MJ8NZwJVqJJSes6pP+1Zro+v0Kc+1p89N6r74i+oI=";
                };

                dontUnpack = true;
                installPhase = ''
                  mkdir -p $out/bin
                  cp $src $out/bin/clusterawsadm
                  chmod +x $out/bin/clusterawsadm
                '';
              };
              azwi = pkgs.stdenv.mkDerivation rec {
                pname = "azwi";
                version = "v1.4.1";

                src = pkgs.fetchurl {
                  url =
                    "https://github.com/Azure/azure-workload-identity/releases/download/${version}/azwi-${version}-"
                    + (if pkgs.stdenv.isDarwin then "darwin-arm64.tar.gz" else "linux-arm64.tar.gz");

                  sha256 = "sha256-Cejrlh4CDtDpv7k93DDwbS4/mSA+AfhjvhMVKHItaHw=";
                };

                unpackPhase = ''
                  tar -xzf $src
                '';
                installPhase = ''
                  mkdir -p $out/bin
                  cp azwi $out/bin/azwi
                  chmod +x $out/bin/azwi
                '';
              };
            in
            [
              go
              golangci-lint
              golines

              gojsontoyaml
              jsonnet
              jq

              yq

              k3d
              kubectl
              kubeseal
              clusterctl

              clusterawsadm
              azwi
              azure-cli

              k9s
              gnumake
            ];
        };
      }
    );
}
