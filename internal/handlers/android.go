package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
)

// DeleteAccountInfoPage is the public "how to delete your account" page that Google Play asks for.
func (a *App) DeleteAccountInfoPage(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, "delete_account.html", map[string]any{"SupportEmail": a.SupportEmail})
}

// AssetLinks serves the Digital Asset Links file that ties the Android (TWA) app to this domain.
// It stays 404 until ANDROID_PACKAGE and ANDROID_SHA256 are set.
func (a *App) AssetLinks(w http.ResponseWriter, r *http.Request) {
	var prints []string
	for _, f := range strings.Split(a.AndroidSHA256, ",") {
		if f = strings.ToUpper(strings.TrimSpace(f)); f != "" {
			prints = append(prints, f)
		}
	}
	if a.AndroidPackage == "" || len(prints) == 0 {
		http.NotFound(w, r)
		return
	}
	out := []map[string]any{{
		"relation": []string{"delegate_permission/common.handle_all_urls"},
		"target": map[string]any{
			"namespace":                "android_app",
			"package_name":             a.AndroidPackage,
			"sha256_cert_fingerprints": prints,
		},
	}}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_ = json.NewEncoder(w).Encode(out)
}
