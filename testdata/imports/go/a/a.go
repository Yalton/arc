package a

import (
	"fmt"

	bee "example.com/app/b"
	_ "example.com/app/c"

	"github.com/example/external"
)

var _ = fmt.Sprint(bee.B, external.X)
