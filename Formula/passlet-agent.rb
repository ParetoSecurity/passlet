class PassletAgent < Formula
  desc "Short-lived credentials for your agents"
  homepage "https://passlet.dev"
  version "0.4.1"
  license "MIT"

  depends_on :linux

  on_arm do
    url "https://github.com/ParetoSecurity/passlet/releases/download/v#{version}/passlet_#{version}_linux_arm64.tar.gz"
    sha256 "3ec7bae118d8b020640a5332f9aa78ed26a9e8ce86d6e88f4ec982001ac50dbb"
  end
  on_intel do
    url "https://github.com/ParetoSecurity/passlet/releases/download/v#{version}/passlet_#{version}_linux_amd64.tar.gz"
    sha256 "4e3b74b78a55976f1a852208c86795b62124784672f6d5d6d9280a73c11df583"
  end

  def install
    bin.install "passlet"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/passlet --version")
  end
end
