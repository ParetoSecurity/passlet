class PassletAgent < Formula
  desc "Short-lived credentials for your agents"
  homepage "https://passlet.dev"
  version "0.4.2"
  license "MIT"

  depends_on :linux

  on_arm do
    url "https://github.com/ParetoSecurity/passlet/releases/download/v#{version}/passlet_#{version}_linux_arm64.tar.gz"
    sha256 "9190d729dfb21d13eca40930e2c1536e9e8484a5652c0ba1468d0969a2ed61fd"
  end
  on_intel do
    url "https://github.com/ParetoSecurity/passlet/releases/download/v#{version}/passlet_#{version}_linux_amd64.tar.gz"
    sha256 "607254ecaa0441bdc8892ed1d1c8618912df1e5b78bbbfdf65c37643293b7698"
  end

  def install
    bin.install "passlet"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/passlet --version")
  end
end
