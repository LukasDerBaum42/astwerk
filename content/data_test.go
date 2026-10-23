package content

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type dataConfig struct {
	Title string `json:"title" toml:"title"`
	Count int    `json:"count" toml:"count"`
}

func writeDataFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDataJSON(t *testing.T) {
	path := writeDataFile(t, "site.json", `{"title": "Site", "count": 3}`)
	got, err := LoadData[dataConfig](path)
	if err != nil {
		t.Fatal(err)
	}
	if want := (dataConfig{Title: "Site", Count: 3}); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestLoadDataTOML(t *testing.T) {
	path := writeDataFile(t, "site.toml", "title = \"Site\"\ncount = 3\n")
	got, err := LoadData[dataConfig](path)
	if err != nil {
		t.Fatal(err)
	}
	if want := (dataConfig{Title: "Site", Count: 3}); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestLoadDataMissing(t *testing.T) {
	if _, err := LoadData[dataConfig](filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestParseDataFormats(t *testing.T) {
	for _, format := range []string{".json", "json", ".JSON"} {
		got, err := ParseData[dataConfig]([]byte(`{"title": "Site", "count": 3}`), format)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if got.Title != "Site" {
			t.Errorf("%s: got %+v", format, got)
		}
	}
}

func TestParseDataUnsupported(t *testing.T) {
	if _, err := ParseData[dataConfig]([]byte("title: Site"), ".yaml"); err == nil {
		t.Fatal("expected an error for an unsupported format")
	}
}

func TestParseDataArray(t *testing.T) {
	got, err := ParseData[[]int]([]byte(`[1, 2, 3]`), ".json")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Errorf("got %v", got)
	}
}

func TestMustLoadDataPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected MustLoadData to panic")
		}
	}()
	MustLoadData[dataConfig](filepath.Join(t.TempDir(), "nope.json"))
}
