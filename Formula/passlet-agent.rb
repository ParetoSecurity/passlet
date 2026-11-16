class PassletAgent < Formula
  desc "Short-lived credentials for your agents"
  homepage "https://passlet.dev"
  version "0.10.0"
  license "MIT"

  depends_on :linux

  on_arm do
    url "https://github.com/ParetoSecurity/passlet/releases/download/v#{version}/passlet_#{version}_linux_arm64.tar.gz"
    sha256 "41e13d0c80ffb8076ab25a84429126045f11e2bf80ce27bd5824ff544818a4d9"
  end
  on_intel do
    url "https://github.com/ParetoSecurity/passlet/releases/download/v#{version}/passlet_#{version}_linux_amd64.tar.gz"
    sha256 "c56afcdc21f53c1caa2dedb8918fffa9c77386bb21dc159fd4f69c8f3e9099fc"
  end

  def install
    bin.install "passlet"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/passlet --version")
  end
end
