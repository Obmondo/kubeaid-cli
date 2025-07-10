{ pkgs }:
with pkgs;
buildGoModule rec {
  pname = "clusterawsadm";
  version = "v2.7.1";

  src = fetchFromGitHub {
    owner = "kubernetes-sigs";
    repo = "cluster-api-provider-aws";
    rev = version;
    hash = "sha256-l2ZCylr47vRYw/HyYaeKfSvH1Kt9YQPwLoHLU2h+AE4=";
  };

  vendorHash = "sha256-iAheoh9VMSdTVvJzhXZBFpGDoDsGO8OV/sYjDEsf8qw=";

  subPackages = [ "cmd/clusterawsadm" ];
}
