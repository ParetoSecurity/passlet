class PassletAgent < Formula
  desc "Short-lived credentials for your agents"
  homepage "https://passlet.dev"
  version "0.3.1"
  license "MIT"

  depends_on :linux

  on_arm do
    url "https://github.com/ParetoSecurity/passlet/releases/download/v#{version}/passlet_#{version}_linux_arm64.tar.gz"
    sha256 "86bd4fcd9f9ead9ec729203a5380e8ff3c1bb5469319578e8b2a6b7b8d0875a6"
  end
  on_intel do
    url "https://github.com/ParetoSecurity/passlet/releases/download/v#{version}/passlet_#{version}_linux_amd64.tar.gz"
    sha256 "cd2277ac374b5008bd44b11f6f62dfbd986cdded92940616a060e56b5ebfec4d"
  end

  def install
    bin.install "passlet"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/passlet version")
  end
end
