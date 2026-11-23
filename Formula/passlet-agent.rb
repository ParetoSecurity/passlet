class PassletAgent < Formula
  desc "Short-lived credentials for your agents"
  homepage "https://passlet.dev"
  version "0.12.1"
  license "MIT"

  depends_on :linux

  on_arm do
    url "https://github.com/ParetoSecurity/passlet/releases/download/v#{version}/passlet_#{version}_linux_arm64.tar.gz"
    sha256 "a149b5ce66dca8453fa939b6734fce0119af690837f54bdd56a3251575931c28"
  end
  on_intel do
    url "https://github.com/ParetoSecurity/passlet/releases/download/v#{version}/passlet_#{version}_linux_amd64.tar.gz"
    sha256 "ceabccee45aec8bb3728163adccbcdb782da6993580a4037ac3edb92ef613020"
  end

  def install
    bin.install "passlet"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/passlet --version")
  end
end
