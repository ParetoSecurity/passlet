cask "passlet" do
  version "0.4.1"
  sha256 "1ee490fe75d36d8c49608ec14d7563edfea3a26b4cd97edbc073a4b3a5e75a13"

  url "https://github.com/ParetoSecurity/passlet/releases/download/v#{version}/passlet_#{version}_macos_universal.zip"
  name "Passlet"
  desc "Short-lived credentials for your agents"
  homepage "https://passlet.dev"

  depends_on macos: :sonoma

  # launchd registration requires an installer script outside the cask sandbox.
  installer script: {
    executable: "/bin/sh",
    args: ["#{staged_path}/Passlet.app/Contents/Resources/homebrew-service.sh", "install", "#{staged_path}/Passlet.app", HOMEBREW_PREFIX.to_s],
    sudo: false,
    must_succeed: true,
  }
  binary "#{staged_path}/Passlet.app/Contents/MacOS/passlet"

  uninstall script: {
    executable: "/bin/sh",
    args: ["#{staged_path}/Passlet.app/Contents/Resources/homebrew-service.sh", "uninstall", "#{staged_path}/Passlet.app", HOMEBREW_PREFIX.to_s],
    sudo: false,
    must_succeed: true,
  }

  caveats "Run passlet approver github setup to configure your GitHub account."
end
