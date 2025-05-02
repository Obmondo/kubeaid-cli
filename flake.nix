{
  description = "KubeAid Bootstrap Script";

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
          nativeBuildInputs = [
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
            (import ./build/nix/pkgs/clusterawsadm.nix { inherit pkgs; })
            (import ./build/nix/pkgs/azwi.nix { inherit pkgs; })
            azure-cli

            k9s
          ];
        };
      }
    );
}
