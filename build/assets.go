// Package build embeds shared build-time application assets for Tisane.
package build

import _ "embed"

// AppIcon is the master PNG application icon used by the desktop runtime.
//
//go:embed appicon.png
var AppIcon []byte
