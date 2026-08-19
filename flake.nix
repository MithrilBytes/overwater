{
  description = "Flag LLM call sites that use more model than the task needs";

  # A release branch, not a rolling one. flake.lock pins the exact
  # revision; run "nix flake lock" after changing this line.
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.05";

  outputs = { self, nixpkgs }:
    let
      version = "2.5.0";
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems f;
    in
    {
      packages = forAllSystems (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          overwater = pkgs.buildGoModule {
            pname = "overwater";
            inherit version;
            src = ./.;

            # The hash of the vendored module cache. Only nix can
            # compute it: run "nix build .#overwater" and paste the
            # hash it reports as the expected one. It changes only
            # when go.mod or go.sum changes.
            vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";

            subPackages = [ "cmd/overwater" ];

            ldflags = [
              "-s"
              "-w"
              "-X github.com/MithrilBytes/overwater/internal/cli.buildVersion=v${version}"
            ];

            # The suite shells out to git and reads the repository's
            # fixtures and goldens; CI runs it, this build does not.
            doCheck = false;

            meta = {
              description = "Flag LLM call sites that use more model than the task needs";
              homepage = "https://github.com/MithrilBytes/overwater";
              license = nixpkgs.lib.licenses.mit;
              mainProgram = "overwater";
            };
          };
        in
        {
          inherit overwater;
          default = overwater;
        });

      apps = forAllSystems (system: rec {
        overwater = {
          type = "app";
          program = "${self.packages.${system}.overwater}/bin/overwater";
        };
        default = overwater;
      });

      checks = forAllSystems (system: {
        inherit (self.packages.${system}) overwater;
      });
    };
}
