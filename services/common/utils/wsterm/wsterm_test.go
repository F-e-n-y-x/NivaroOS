package wsterm

import (
	"testing"

	"github.com/gorilla/websocket"
)

func TestParseClientMessage(t *testing.T) {
	type res struct {
		input      string
		resize     bool
		cols, rows uint16
		ignored    bool
	}
	cases := []struct {
		name string
		mt   int
		data string
		want res
	}{
		{"binary is always input", websocket.BinaryMessage, `{"type":"resize","cols":80,"rows":24}`, res{input: `{"type":"resize","cols":80,"rows":24}`}},
		{"framed resize", websocket.TextMessage, "\x00" + `{"type":"resize","cols":100,"rows":40}`, res{resize: true, cols: 100, rows: 40}},
		{"framed garbage dropped", websocket.TextMessage, "\x00not json", res{ignored: true}},
		{"legacy exact resize", websocket.TextMessage, `{"type":"resize","cols":90,"rows":30}`, res{resize: true, cols: 90, rows: 30}},
		{"pasted json with extra keys is input", websocket.TextMessage, `{"type":"resize","cols":90,"rows":30,"x":1}`, res{input: `{"type":"resize","cols":90,"rows":30,"x":1}`}},
		{"pasted json other type is input", websocket.TextMessage, `{"type":"cmd","cmd":"rm -rf /"}`, res{input: `{"type":"cmd","cmd":"rm -rf /"}`}},
		{"pasted arbitrary json is input", websocket.TextMessage, `{"name":"x"}`, res{input: `{"name":"x"}`}},
		{"plain text input", websocket.TextMessage, "ls -la\r", res{input: "ls -la\r"}},
	}
	for _, c := range cases {
		input, ctrl := ParseClientMessage(c.mt, []byte(c.data))
		switch {
		case c.want.ignored:
			if input != nil || ctrl != nil {
				t.Errorf("%s: expected ignored, got input=%q ctrl=%v", c.name, input, ctrl)
			}
		case c.want.resize:
			cols, rows, ok := ctrl.Resize()
			if input != nil || !ok || cols != c.want.cols || rows != c.want.rows {
				t.Errorf("%s: got input=%q ctrl=%+v", c.name, input, ctrl)
			}
		default:
			if ctrl != nil || string(input) != c.want.input {
				t.Errorf("%s: got input=%q ctrl=%+v", c.name, input, ctrl)
			}
		}
	}
}

func TestParseSize(t *testing.T) {
	if c, r := ParseSize("", "", 120, 32); c != 120 || r != 32 {
		t.Fatal(c, r)
	}
	if c, r := ParseSize("200", "50", 120, 32); c != 200 || r != 50 {
		t.Fatal(c, r)
	}
	if c, r := ParseSize("-1", "abc", 120, 32); c != 120 || r != 32 {
		t.Fatal(c, r)
	}
	if c, r := ParseSize("99999", "0", 120, 32); c != 120 || r != 32 {
		t.Fatal(c, r)
	}
}
