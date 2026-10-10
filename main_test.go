package main

import (
	"encoding/json/v2"
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
	"github.com/oschwald/maxminddb-golang/v2"
)

func TestParseCIDRs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   string
		version int
		want    []string
	}{
		{"IPv4", "# comment\n\n 1.2.3.4/24 \n1.2.3.0/24\n8.8.8.8/32\n", 4, []string{"1.2.3.0/24", "8.8.8.8/32"}},
		{"IPv6", "2400:3200:0000::1/32\n2400:3200::/32\n240e::1/20\n", 6, []string{"2400:3200::/32", "240e::/20"}},
		{"empty", "", 4, nil},
		{"comments only", "# comment\n \n", 6, nil},
		{"invalid CIDR", "1.2.3.0/24\ninvalid\n", 4, nil},
		{"IPv6 in IPv4 source", "240e::/20\n", 4, nil},
		{"IPv4 in IPv6 source", "1.2.3.0/24\n", 6, nil},
		{"mapped IPv4", "::ffff:1.2.3.4/120\n", 6, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCIDRs(strings.NewReader(tc.input), tc.version)
			if tc.want == nil {
				if err == nil {
					t.Fatal("expected invalid source to fail")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestParseCIDRsReadFailure(t *testing.T) {
	readErr := errors.New("interrupted download")
	_, err := parseCIDRs(io.MultiReader(strings.NewReader("1.2.3.0/24\n"), failingReader{readErr}), 4)
	if !errors.Is(err, readErr) {
		t.Fatalf("got %v, want read error", err)
	}
}

type failingWriteCloser struct {
	writeErr error
	closeErr error
	closed   bool
}

func (w *failingWriteCloser) Write(p []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return len(p), nil
}
func (w *failingWriteCloser) Close() error { w.closed = true; return w.closeErr }

func TestWriteAndClose(t *testing.T) {
	writeErr, closeErr := errors.New("write failed"), errors.New("close failed")
	for _, tc := range []struct {
		name               string
		writeErr, closeErr error
	}{
		{"success", nil, nil},
		{"write failure", writeErr, nil},
		{"close failure", nil, closeErr},
		{"both fail", writeErr, closeErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &failingWriteCloser{writeErr: tc.writeErr, closeErr: tc.closeErr}
			err := writeAndClose(w, func(out io.Writer) error { _, err := io.WriteString(out, "data"); return err })
			if !w.closed {
				t.Fatal("writer was not closed")
			}
			if tc.writeErr == nil && tc.closeErr == nil && err != nil {
				t.Fatal(err)
			}
			for _, want := range []error{tc.writeErr, tc.closeErr} {
				if want != nil && !errors.Is(err, want) {
					t.Fatalf("got %v, want %v", err, want)
				}
			}
		})
	}
}

func testTree(t *testing.T, cidrs []string) *mmdbwriter.Tree {
	t.Helper()
	writer, err := mmdbwriter.New(mmdbwriter.Options{IncludeReservedNetworks: true, DisableIPv4Aliasing: true})
	if err != nil {
		t.Fatal(err)
	}
	value := mmdbtype.Map{"country": mmdbtype.Map{"iso_code": mmdbtype.String("CN")}}
	if _, err := insertStaticCIDRs(writer, cidrs, value); err != nil {
		t.Fatal(err)
	}
	return writer
}

func seedOldOutput(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("previous output"), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertOldOutput(t *testing.T, dir string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "old.txt"))
	if err != nil || string(data) != "previous output" {
		t.Fatalf("old output changed: %q, %v", data, err)
	}
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "dist" {
		t.Fatalf("unexpected leftovers: %v", entries)
	}
}

func TestWriteOutputs(t *testing.T) {
	cidrs := []string{"1.2.3.0/24", "240e::/20"}
	for _, existing := range []bool{false, true} {
		name := "initial output"
		if existing {
			name = "replace existing output"
		}
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "dist")
			if existing {
				seedOldOutput(t, dir)
			}
			if err := writeOutputs(dir, testTree(t, cidrs), cidrs); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "chnroutes.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != strings.Join(cidrs, "\n")+"\n" {
				t.Fatalf("unexpected TXT: %q", data)
			}
			data, err = os.ReadFile(filepath.Join(dir, "chnroutes.json"))
			if err != nil {
				t.Fatal(err)
			}
			var rules singBoxRuleSet
			if err := json.Unmarshal(data, &rules); err != nil {
				t.Fatal(err)
			}
			if rules.Version != 2 || len(rules.Rules) != 1 || !reflect.DeepEqual(rules.Rules[0].IPCIDR, cidrs) {
				t.Fatalf("unexpected rules: %+v", rules)
			}
			reader, err := maxminddb.Open(filepath.Join(dir, "chnroutes.mmdb"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := reader.Close(); err != nil {
					t.Errorf("关闭 MMDB reader 失败: %v", err)
				}
			})
			for _, ip := range []string{"1.2.3.4", "240e::1"} {
				var code string
				if err := reader.Lookup(netip.MustParseAddr(ip)).DecodePath(&code, "country", "iso_code"); err != nil {
					t.Fatal(err)
				}
				if code != "CN" {
					t.Fatalf("%s: got %q, want CN", ip, code)
				}
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 3 {
				t.Fatalf("unexpected output files: %v", entries)
			}
			entries, err = os.ReadDir(filepath.Dir(dir))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatalf("unexpected leftovers: %v", entries)
			}
		})
	}
}

func TestWriteOutputsFailurePreservesOldOutput(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dist")
	seedOldOutput(t, dir)
	// Invalid UTF-8 makes JSON serialization fail after MMDB and TXT were staged.
	if err := writeOutputs(dir, testTree(t, []string{"1.2.3.0/24"}), []string{string([]byte{0xff})}); err == nil {
		t.Fatal("expected serialization failure")
	}
	assertOldOutput(t, dir)
}

func TestReplaceOutputDirFailureRestoresOldOutput(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dist")
	seedOldOutput(t, dir)
	if err := replaceOutputDir(filepath.Join(filepath.Dir(dir), "missing-staging"), dir); err == nil {
		t.Fatal("expected rename failure")
	}
	assertOldOutput(t, dir)
}
