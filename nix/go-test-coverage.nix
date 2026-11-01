{ pkgs }:
(pkgs.buildGoModule.override { go = pkgs.go_1_27; }) {
  pname = "go-test-coverage";
  version = "2.21.0";
  src = pkgs.fetchFromGitHub {
    owner = "vladopajic";
    repo = "go-test-coverage";
    rev = "v2.21.0";
    hash = "sha256-Z/9BGs2Y8SNJPfiQ/eUAjgmz7UCAaX8DEKgLVqkzExw=";
  };
  vendorHash = "sha256-bhRprQCWk69p+PknwsKRHTAKnKoLO7nnaWLfmFu/0ZA=";
  subPackages = [ "." ];
  meta.mainProgram = "go-test-coverage";
}
