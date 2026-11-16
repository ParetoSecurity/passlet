cask "passlet" do
  version "0.11.0"
  sha256 "19293424173799f64911222f27fde3c0b3b06ead0a98ad080b47c8a04511a96c"

  url "https://github.com/ParetoSecurity/passlet/releases/download/v#{version}/passlet_#{version}_macos.zip"
  name "Passlet"
  desc "Short-lived credentials for your agents"
  homepage "https://passlet.dev"

  depends_on arch: :arm64
  depends_on macos: :tahoe

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
