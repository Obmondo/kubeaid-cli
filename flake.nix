{
  description = "KubeAid CLI development environment";

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

            bun
            addlicense
            pre-commit
          ];

          buildInputs = [
            # Required for building KubePrometheus.
            gojsontoyaml
            jsonnet
            jsonnet-bundler
            jq

            kubectl
            kubeone
            clusterctl
            cilium-cli
          ];
        };

        CGO_ENABLED = 0;

        packages.default = buildGoModule {
          pname = "kubeaid-cli";
          version = "v" + builtins.readFile ./cmd/kubeaid-core/root/version/version.txt;

          meta = {
            description = "KubeAid CLI helps you operate KubeAid managed Kubernetes cluster lifecycle in a GitOps native way";
            homepage = "https://github.com/Obmondo/kubeaid-cli";
            license = lib.licenses.gpl3;
            maintainers = with lib.maintainers; [
              archisman-mridha
              ashish1099
            ];
            mainProgram = "kubeaid-cli";
          };

          vendorHash = "sha256-HndNtKWxYWp81r1AWcOmlGToQ+udglmqkE3Md6zfpSY=";

          src = self;
          subPackages = [ "cmd/kubeaid-cli" ];
          goSum = ./go.sum;
          ldflags = [
            # Disable symbol table generation.
            # You will not be able to use go tool nm to list the symbols in the binary.
            "-s"

            # Disable DWARF debugging information generation.
            # You will not be able to use gdb on the binary to look at specific functions or set
            # breakpoints or get stack traces, because all the metadata gdb needs will not be
            # there. You will also not be able to use other tools that depend on the information,
            # like pprof profiling.
            "-w"
          ];
        };
      }
    );
}
