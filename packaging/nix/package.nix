{ lib, stdenvNoCC, fetchurl, unzip, release ? (if builtins.pathExists ../release.json then builtins.fromJSON (builtins.readFile ../release.json) else throw "Passlet package metadata is not published yet; use a reviewed package release revision.") }:
let
  platform = if stdenvNoCC.hostPlatform.isDarwin then "macos_universal"
    else if stdenvNoCC.hostPlatform.isAarch64 then "linux_arm64" else "linux_amd64";
  artifact = release.artifacts.${platform};
in stdenvNoCC.mkDerivation {
  pname = "passlet";
  inherit (release) version;
  src = fetchurl { inherit (artifact) url sha256; };
  nativeBuildInputs = lib.optional stdenvNoCC.hostPlatform.isDarwin unzip;
  sourceRoot = ".";
  # ditto archives contain AppleDouble metadata; it must not become app resources.
  unpackPhase = lib.optionalString stdenvNoCC.hostPlatform.isDarwin ''
    unzip -q "$src" -x '__MACOSX/*' '*/._*'
  '';
  dontConfigure = true;
  dontBuild = true;
  dontFixup = true; # Rewriting Mach-O files invalidates the Developer ID signature.
  installPhase = if stdenvNoCC.hostPlatform.isDarwin then ''
    mkdir -p "$out/Applications" "$out/bin"
    cp -R Passlet.app "$out/Applications/"
    ln -s "$out/Applications/Passlet.app/Contents/MacOS/passlet" "$out/bin/passlet"
  '' else ''
    install -Dm755 passlet "$out/bin/passlet"
  '';
  meta = {
    description = "Short-lived credentials for your agents";
    homepage = "https://passlet.dev";
    license = lib.licenses.gpl3Only;
    mainProgram = "passlet";
    platforms = [ "aarch64-darwin" "x86_64-darwin" "aarch64-linux" "x86_64-linux" ];
  };
}
