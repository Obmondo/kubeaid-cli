{ pkgs }:
with pkgs;
stdenv.mkDerivation rec {
  pname = "clusterawsadm";
  version = "v2.7.1";

  src = fetchurl {
    url =
      "https://github.com/kubernetes-sigs/cluster-api-provider-aws/releases/download/${version}/"
      + (if stdenv.isDarwin then "clusterawsadm-darwin-arm64" else "clusterawsadm-linux-amd64");

    sha256 = "sha256-J4MJ8NZwJVqJJSes6pP+1Zro+v0Kc+1p89N6r74i+oI=";
  };

  dontUnpack = true;
  installPhase = ''
    mkdir -p $out/bin
    cp $src $out/bin/clusterawsadm
    chmod +x $out/bin/clusterawsadm
  '';
}
