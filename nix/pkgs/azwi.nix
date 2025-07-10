{ pkgs }:
with pkgs;
buildGoModule rec {
  pname = "azwi";
  version = "v1.4.1";

  src = fetchFromGitHub {
    owner = "Azure";
    repo = "azure-workload-identity";
    rev = version;
    hash = "sha256-Ru/8K67hq8qeeyMbjdZjcVxFGBVKJZdkj0J3rNAUs8E=";
  };

  vendorHash = "sha256-XM02obL0cfolf8DuUwcYlMNRx/nyrQof65coGmiLB3s=";

  subPackages = [ "cmd/azwi" ];
}
