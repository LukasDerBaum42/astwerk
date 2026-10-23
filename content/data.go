package content

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// LoadData reads one JSON or TOML file into T. The format follows the file
// extension: .json or .toml.
//
// For a single object or array that is not content, this is the whole API:
//
//	site, err := content.LoadData[SiteConfig]("site.json")
//
// For build-time data that should be loaded once at start-up, [MustLoadData]
// does the same thing as a package-level value:
//
//	var Site = content.MustLoadData[SiteConfig]("site.json")
func LoadData[T any](path string) (T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("content: %s: %w", path, err)
	}
	value, err := ParseData[T](data, filepath.Ext(path))
	if err != nil {
		var zero T
		return zero, fmt.Errorf("content: %s: %w", path, err)
	}
	return value, nil
}

// ParseData decodes data as JSON (".json") or TOML (".toml"). The leading dot
// is optional. Use it for embedded files or tests.
func ParseData[T any](data []byte, format string) (T, error) {
	var value T
	switch strings.TrimPrefix(strings.ToLower(format), ".") {
	case "json":
		if err := json.Unmarshal(data, &value); err != nil {
			return value, fmt.Errorf("data (json): %w", err)
		}
	case "toml":
		if err := toml.Unmarshal(data, &value); err != nil {
			return value, fmt.Errorf("data (toml): %w", err)
		}
	default:
		return value, fmt.Errorf("data: unsupported format %q (want .json or .toml)", format)
	}
	return value, nil
}

// MustLoadData is [LoadData] for build-time values. It panics on error, which
// is the right failure mode for a package-level config:
//
//	var Site = content.MustLoadData[SiteConfig]("site.json")
func MustLoadData[T any](path string) T {
	value, err := LoadData[T](path)
	if err != nil {
		panic(err)
	}
	return value
}
