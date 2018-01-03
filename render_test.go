package quill

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"testing"
)

func TestSimple(t *testing.T) {

	cases := []string{
		`[{"insert": "\n"}]`,
		`[{"insert":"line1\nline2\n"}]`,
		`[{"insert": "line1\n\nline3\n"}]`,
	}

	want := []string{
		"<p></p>",
		"<p>line1</p><p>line2</p>",
		"<p>line1</p><p><br></p><p>line3</p>",
	}

	for i := range cases {

		bts, err := Render([]byte(cases[i]))
		if err != nil {
			t.Errorf("%s", err)
			t.FailNow()
		}
		if string(bts) != want[i] {
			t.Errorf("bad rendering; got: %s", bts)
		}

	}

}

func TestOps1(t *testing.T) {
	if err := testPair("ops1.json", "ops1.html"); err != nil {
		t.Errorf("%s", err)
	}
}

func TestNested(t *testing.T) {
	if err := testPair("nested.json", "nested.html"); err != nil {
		t.Errorf("%s", err)
	}
}

func testPair(opsFile, htmlFile string) error {
	opsArr, err := ioutil.ReadFile("./tests/" + opsFile)
	if err != nil {
		return fmt.Errorf("could not read %s; %s\n", opsFile, err)
	}
	desired, err := ioutil.ReadFile("./tests/" + htmlFile)
	if err != nil {
		return fmt.Errorf("could not read %s; %s\n", htmlFile, err)
	}
	got, err := Render(opsArr)
	if err != nil {
		return fmt.Errorf("error rendering; %s\n", err)
	}
	if !bytes.Equal(desired, got) {
		return fmt.Errorf("bad rendering; \nwanted: \n%s\ngot: \n%s\n", desired, got)
	}
	return nil
}

func TestOp_ClosePrevAttrs(t *testing.T) {
	fts := formatState{
		open: []format{
			{"em", "italic", Tag},
			{"strong", "bold", Tag},
		},
	}
	o := &Op{
		Data: "stuff",
		Type: "text",
		// no attributes set
	}
	desired := "</strong></em>"
	buf := new(bytes.Buffer)
	o.closePrevFormats(buf, &fts, nil)
	got := buf.String()
	if got != desired {
		t.Errorf("closed attributes wrong; wanted %qgot %q\n", desired, got)
	}
}
