{
  description = "KubeAid CLI development environment";

  inputs = {
    nixpkgs.url = "github:cachix/devenv-nixpkgs/rolling";
    flake-utils.url = "github:numtide/flake-utils";

    devenv.url = "github:cachix/devenv";
  };

  nixConfig = {
    extra-trusted-public-keys = "devenv.cachix.org-1:w1cLUi8dv3hnoSPGAuibQv+f9TZLr6cv/Hm9XgU50cw=";
    extra-substituters = "https://devenv.cachix.org";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      devenv,
    }@inputs:
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
        devShells.default = devenv.lib.mkShell {
          inherit inputs pkgs;

          modules = [
            (
              { pkgs, config, ... }:
              {
                # Most packages come pre-built with binaries provided by the official Nix binary
                # cache. If you're modifying a package or using a package that's not built
                # upstream, Nix will build it from source instead of downloading a binary.
                # To prevent packages from being built more than once, devenv provides seamless
                # integration with binary caches hosted by Cachix.
                #
                # Devenv will automatically configure Cachix caches for you, or guide you how to
                # add the caches to Nix manually. Any caches set up by devenv are used in addition
                # to the caches configured in Nix, for example, in /etc/nix/nix.conf.
                cachix = {
                  enable = true;

                  # devenv.cachix.org is added to the list of pull caches by default. It mirrors
                  # the official NixOS cache and is designed to provide caching for the
                  # devenv-nixpkgs/rolling nixpkgs input.
                  #
                  # Some languages and integrations may automatically add caches when enabled.
                  pull = [ ];
                };

                packages =
                  with pkgs;
                  [
                    go
                    golangci-lint
                    richgo

                    clusterctl

                    bun
                    addlicense
                    pre-commit
                    nix-update
                    goreleaser
                  ]
                  # Runtime dependencies.
                  ++ [
                    # Required for building KubePrometheus.
                    gojsontoyaml
                    jsonnet
                    jsonnet-bundler
                    jq

                    cilium-cli
                  ];

                env = {
                  CGO_ENABLED = 0;
                };
              }
            )
          ];
        };

        packages.default = buildGoModule {
          pname = "kubeaid-cli";
          version = "v" + builtins.readFile ./cmd/kubeaid-core/root/version/version.txt;

          meta = {
            mainProgram = "kubeaid-cli";

            description = "KubeAid CLI helps you operate KubeAid managed Kubernetes cluster lifecycle in a GitOps native way";
            homepage = "https://github.com/Obmondo/kubeaid-cli";
            license = lib.licenses.gpl3;

            maintainers =
              with lib.maintainers
              // {
                archisman-mridha = {
                  name = "Archisman Mridha";
                  email = "archisman@obmondo.com";
                };
                ashish1099 = {
                  name = "Ashish Jaiswal";
                  email = "ashish@obmondo.com";
                };
              }; [
                archisman-mridha
                ashish1099
              ];
          };

          vendorHash = "sha256-0iQRiApQeso96gloZNpuxxEnk6AyknTU48UNxg25YcY=";

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
