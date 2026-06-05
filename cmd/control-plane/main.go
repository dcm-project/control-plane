package main

import (
	"os"

	"github.com/dcm-project/control-plane/internal/app"
)

func main() {
	os.Exit(app.Run())
}
