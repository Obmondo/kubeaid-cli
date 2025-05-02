{ pkgs }:
with pkgs;
stdenv.mkDerivation rec {
  pname = "azwi";
  version = "v1.4.1";

  src = fetchurl {
    url =
      "https://github.com/Azure/azure-workload-identity/releases/download/${version}/azwi-${version}-"
      + (if stdenv.isDarwin then "darwin-arm64.tar.gz" else "linux-arm64.tar.gz");

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
}
