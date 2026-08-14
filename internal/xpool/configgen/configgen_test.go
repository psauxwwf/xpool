package configgen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadProxiesSkipsInvalidLines(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "proxies.txt")
	content := "\n# comment\nsocks5h://user:pass@example.com:1080\nhttp://bad.example.com:8080\nnot a url\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	proxies, invalid, err := ReadProxies(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(proxies) != 1 {
		t.Fatalf("expected 1 valid proxy, got %d", len(proxies))
	}
	if len(invalid) != 2 {
		t.Fatalf("expected 2 invalid lines, got %d", len(invalid))
	}
	if invalid[0].Line != 4 || invalid[1].Line != 5 {
		t.Fatalf("unexpected invalid line numbers: %+v", invalid)
	}
}
