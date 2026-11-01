package passlet

import (
	"crypto/sha256"
	"encoding/base64"
	"html/template"
)

// Self-contained pages: only the manifest form uses an inline, CSP-hashed script.
const setupCSS = `
:root{color-scheme:light;--paper:#f8f7f3;--ink:#193b30;--muted:#606e66;--line:#dce3da;--green:#20563f}
*{box-sizing:border-box}body{margin:0;background:var(--paper);color:var(--ink);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;font-size:16px;line-height:1.6}
.shell{width:min(100% - 40px,620px);margin:48px auto}.brand{font-size:23px;font-weight:750;letter-spacing:-.8px;display:flex;align-items:center;gap:11px;margin-bottom:34px}.mark{display:grid;place-items:center;width:34px;height:34px;background:var(--green);color:#fff8dc;border-radius:9px;font-size:22px}
.card{background:#fff;border:1px solid var(--line);border-radius:20px;padding:36px;box-shadow:0 12px 40px #173c3010}.eyebrow{font-size:12px;letter-spacing:1.5px;text-transform:uppercase;font-weight:700;color:var(--muted);margin:0 0 12px}h1{font-size:clamp(28px,5vw,36px);line-height:1.15;letter-spacing:-1.2px;margin:0 0 18px}h2{font-size:18px;line-height:1.4;margin:28px 0 10px}p{margin:0 0 18px}.intro{color:var(--muted)}.name{font-family:ui-monospace,SFMono-Regular,monospace;font-size:15px;overflow-wrap:anywhere}.panel{padding:16px 20px;background:var(--paper);border-radius:12px;margin:22px 0}.label{font-size:12px;color:var(--muted);display:block;margin-bottom:4px}
.permissions{margin:0 0 12px}.permissions div{display:flex;align-items:baseline;justify-content:space-between;gap:18px;padding:10px 0;border-bottom:1px solid var(--line)}dt{overflow-wrap:anywhere}dd{margin:0;flex-shrink:0;font-size:12px;font-weight:650;color:var(--green);background:#edf3ed;padding:2px 9px;border-radius:6px}.note{font-size:13px;color:var(--muted)}a{color:var(--green);text-underline-offset:3px}a:hover{color:#102c20}button,.button{display:block;width:100%;text-align:center;border:1px solid var(--green);border-radius:10px;background:var(--green);color:white;padding:13px 18px;font:inherit;font-weight:650;text-decoration:none;cursor:pointer}button:hover,.button:hover{background:#153e2c;color:white}.secondary{background:white;color:var(--green)}:focus-visible{outline:3px solid #bb8647;outline-offset:4px}form{margin-top:26px}.footer{font-size:12px;color:var(--muted);text-align:center;margin-top:20px}.steps{padding-left:23px;margin:24px 0}.steps li{padding-left:5px;margin-bottom:24px}.steps p{margin:8px 0 12px}.steps .button{margin-top:12px}.success{color:var(--green)}.optional{border-top:1px solid var(--line);margin-top:26px;padding-top:4px}
@media(max-width:480px){.shell{width:calc(100% - 24px);margin:24px auto}.card{padding:24px}.brand{margin-bottom:22px}.permissions div{gap:10px}}
`

var setupContentSecurityPolicy = func() string {
	sum := sha256.Sum256([]byte(setupCSS))
	scriptSum := sha256.Sum256([]byte(setupSubmitScript))
	return "default-src 'none'; img-src 'self'; style-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'; script-src 'sha256-" + base64.StdEncoding.EncodeToString(scriptSum[:]) + "'; form-action https://github.com; frame-ancestors 'none'; base-uri 'none'"
}()

const setupHead = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Passlet · GitHub setup</title><link rel="icon" type="image/png" href="/favicon.png"><style>` + setupCSS + `</style></head><body><div class="shell"><header class="brand"><span class="mark" aria-hidden="true">P</span>Passlet</header><main class="card">`
const setupFoot = `</main><footer class="footer">Passlet · GitHub setup</footer></div></body></html>`

func setupTemplate(name, body string) *template.Template {
	return template.Must(template.New(name).Parse(setupHead + body + setupFoot))
}

const setupSubmitScript = `document.querySelector('form').submit();`

var setupPage = setupTemplate("setup", `<h1>Opening GitHub…</h1><form method="post" action="{{.Action}}"><input type="hidden" name="manifest" value="{{.Manifest}}"><noscript><button type="submit">Create my GitHub App</button></noscript></form><script>`+setupSubmitScript+`</script>`)

var setupCompletePage = setupTemplate("complete", `<h1>GitHub setup complete.</h1><p>You can close this tab and return to your terminal.</p>`)

var setupFailurePage = template.Must(template.New("failure").Parse(setupHead + `{{range .}}<p>{{.}}</p>{{end}}` + setupFoot))
