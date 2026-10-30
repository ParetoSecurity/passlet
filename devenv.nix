{ pkgs, ... }: {
  packages = with pkgs; [
    bash
    go_1_27
    openssl
    python3
    git
    gh
    gnumake
    actionlint
    shellcheck
    gitleaks
  ];

  env.PASSLET_DEVENV = "1";

  scripts.check.exec = "make check";
  scripts.lint.exec = ''
    set -eu
    actionlint .github/workflows/*.yml
    shellcheck scripts/build-macos.sh scripts/build.sh scripts/check.sh scripts/go.sh scripts/icon.sh scripts/release.sh
    gitleaks git --redact
  '';

  enterTest = ''
    make check
    make build
  '';
}
