package assets

import _ "embed"

//go:embed reference-models.json
var Models []byte

//go:embed demo-video.webm
var DemoVideo []byte
