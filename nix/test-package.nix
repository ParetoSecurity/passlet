{ pkgs, source }:
(pkgs.buildGoModule.override { go = pkgs.go_1_27; }) {
  pname = "passlet-integration-fixture";
  version = "0.2.0-test";
  src = pkgs.lib.fileset.toSource {
    root = source;
    fileset = pkgs.lib.fileset.unions [
      (source + "/go.mod") (source + "/go.sum")
      (source + "/cmd") (source + "/internal")
    ];
  };
  vendorHash = "sha256-AQCVyh83+7eFuhQmSqCyUjp684DdgOSIapfVwbUFnbY=";
  subPackages = [ "cmd/passlet" ];
  doCheck = false; # Tests run separately; the VM driver runs the fixture binary.
  postBuild = ''
    go test -c -o passlet-vm-fixture ./internal/passlet
  '';
  postInstall = ''
    mkdir -p $out/libexec
    cp passlet-vm-fixture $out/libexec/
  '';
}
