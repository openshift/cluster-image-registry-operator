package assets

import (
	"embed"
)

//go:embed *.yaml
var f embed.FS

// MustAsset reads and returns the content of the named file or panics
// if something went wrong.
func MustAsset(name string) []byte {
	data, err := f.ReadFile(name)
	if err != nil {
		panic(err)
	}

	return data
}
