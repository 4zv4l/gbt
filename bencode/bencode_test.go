package bencode

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

var bencodeTests = []struct {
	name     string
	encoded  string
	expected any
}{
	{"zeroInteger", "i0e", 0},
	{"integer", "i64e", 64},
	{"negativeInteger", "i-5e", -5},
	{"str", "6:foobar", "foobar"},
	{"zeroStr", "0:", ""},

	{"list", "li-5e5:aaaaae", []any{-5, "aaaaa"}},
	{"zeroList", "le", []any(nil)},

	{"dict", "d3:foo3:bare", map[string]any{"foo": "bar"}},
	{"dictList", "d4:listli64eee", map[string]any{"list": []any{64}}},
	{"zeroDict", "de", map[string]any{}},
}

func TestDecode(t *testing.T) {
	for _, tt := range bencodeTests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.encoded)
			val, err := Decode(r)

			if err != nil {
				t.Fatalf("Decode() returned unexpected error: %v", err)
			}

			if !reflect.DeepEqual(val, tt.expected) {
				t.Errorf("Decode() expected %v (type %T), got %v (type %T)", tt.expected, tt.expected, val, val)
			}
		})
	}
}

func ExampleDecode() {
	bencodedData := "li64e3:fooe"
	bdecodedData, _ := Decode(strings.NewReader(bencodedData))
	fmt.Println(bdecodedData.([]any)[1])
	// Output:
	// foo
}

func TestEncode(t *testing.T) {
	for _, tt := range bencodeTests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			err := Encode(&buf, tt.expected)

			if err != nil {
				t.Fatalf("Encode() returned unexpected error: %v", err)
			}

			if buf.String() != tt.encoded {
				t.Errorf("Encode() expected %q, got %q", tt.encoded, buf.String())
			}
		})
	}
}

func ExampleEncode() {
	bdecodedData := []any{64, "foo"}
	bencodedData := bytes.NewBuffer(nil)
	_ = Encode(bencodedData, bdecodedData)
	fmt.Println(bencodedData)
	// Output:
	// li64e3:fooe
}
