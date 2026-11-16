package http

import "net/http"

// contentSecurityPolicy allows the interface what it loads and nothing
// else: its own scripts and styles, the Inter webfont from Google Fonts
// (web/index.html), inline style attributes (React's style prop, used where
// a value is data: a meter's width), data: and blob: images, and requests to
// this origin. No inline script, no framing, no plugins, no foreign forms.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"font-src 'self' https://fonts.gstatic.com; " +
	"img-src 'self' data: blob:; " +
	"connect-src 'self'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"object-src 'none'"

// securityHeaders sets the response headers that make a browser hold the
// interface to what it is: no sniffing a JSON body into a script, no
// framing it into a clickjacking page, no leaking a project path in a
// Referer, and the policy above. Strict-Transport-Security only when this
// process terminated TLS itself; behind a proxy, the proxy owns that header.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}
