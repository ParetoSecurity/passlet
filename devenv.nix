{ pkgs, inputs, config, ... }: {
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

  enterShell = ''
    python3 "${config.devenv.root}/scripts/sync-agent-conventions.py" "${inputs.niteo-agents}" "${config.devenv.root}"
  '';

  scripts.check.exec = "make check";
  scripts.lint.exec = ''
    set -eu
    actionlint .github/workflows/*.yml
    shellcheck scripts/*.sh
    gitleaks git --redact
  '';

  enterTest = ''
    make check
    make build
  '';
}
