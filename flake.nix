{
  description = "KubeAid Bootstrap Script development environment";

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

            nixfmt-rfc-style
            direnv
          ];

          buildInputs = [
            # Required for building KubePrometheus.
            gojsontoyaml
            jsonnet
            jsonnet-bundler
            jq

            k3d
            kubectl
            kubeseal
            clusterctl
            (import ./build/nix/pkgs/clusterawsadm.nix { inherit pkgs; })
            (import ./build/nix/pkgs/azwi.nix { inherit pkgs; })
            azure-cli
            kubeone

            yq
          ];

          # Hitting this issue : https://github.com/Azure/azure-cli/issues/31419.
          # Got the solution from here : https://github.com/dotnet/orleans/pull/9486/files
          AZURE_CORE_USE_MSAL_HTTP_CACHE = "false";
        };
      }
    );
}
