cask "passlet" do
  version "0.5.0"
  sha256 "71843b54f948b6d011b5800f551a35391eabe640193cb18b1ebb7e6193383b91"

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
