{
  description = "git-third-party";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    systems.url = "github:nix-systems/default";
    flake-parts.url = "github:hercules-ci/flake-parts";

    treefmt-nix.url = "github:numtide/treefmt-nix";
    treefmt-nix.inputs.nixpkgs.follows = "nixpkgs";

    git-hooks.url = "github:cachix/git-hooks.nix";
    git-hooks.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = inputs @ {flake-parts, ...}:
    flake-parts.lib.mkFlake {inherit inputs;} {
      systems = import inputs.systems;

      imports = [
        inputs.treefmt-nix.flakeModule
        inputs.git-hooks.flakeModule
      ];

      perSystem = {
        self',
        config,
        pkgs,
        lib,
        ...
      }: let
        gitThirdParty = pkgs.buildGoModule {
          pname = "git-third-party";
          version = "1.0.0";
          src = lib.cleanSource ./.;
          vendorHash = "sha256-n58Qmiv3gik1qkuXQFbQ+soeOQtUz1dUocEAJepqp/E=";

          nativeCheckInputs = [
            pkgs.git
            pkgs.python3
          ];

          meta = {
            description = "Vendor third-party git content into a host git repo";
            homepage = "https://github.com/khwstolle/git-third-party";
            license = lib.licenses.mit;
            mainProgram = "git-third-party";
          };
        };
      in {
        treefmt = {
          config = {
            projectRootFile = "flake.nix";
            programs = {
              deadnix.enable = true;
              alejandra.enable = true;
              statix.enable = true;
              gofmt.enable = true;
              goimports.enable = true;
            };
          };
        };

        pre-commit = {
          check.enable = false;

          settings = {
            tools.pre-commit = pkgs.prek;
            hooks = {
              treefmt.enable = true;

              staticcheck.enable = true;
              golangci-lint.enable = true;
              gotest.enable = true;
            };
          };
        };

        packages = {
          default = self'.packages.git-third-party;
          git-third-party = gitThirdParty;
        };

        checks.default = self'.packages.git-third-party;

        devShells.default = pkgs.mkShell {
          inputsFrom = [
            config.treefmt.build.devShell
            config.pre-commit.devShell
          ];

          packages = with pkgs; [
            go
            gopls
            gotools
            golangci-lint
            git
            prek
            (python3.withPackages (ps: with ps; [hatch]))
            nodejs_20
          ];
        };
      };
    };
}
