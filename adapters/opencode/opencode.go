// Package opencode embeds arc's opencode plugin shim (arc.js) so that
// 'arc init --opencode' can install it. The contract the shim speaks is
// pinned in internal/hook/opencode.go.
package opencode

import _ "embed"

// Shim is the plugin source installed as .opencode/plugins/arc.js.
//
//go:embed arc.js
var Shim []byte

// Marker starts the first line of the shim; a file carrying it is arc's
// own and may be overwritten.
const Marker = "// arc-managed: opencode plugin shim."
