// Package `quill` takes a Quill-based Delta (https://github.com/quilljs/delta) as a JSON array of `insert` operations
// and renders the defined HTML document.
//
// This library is designed to be easily extendable. Simply call RenderExtended with a function that may provide its
// own formats for certain kinds of ops and attributes.
package quill

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Render takes a Delta array of insert operations and returns the rendered HTML using the built-in settings.
// If an error occurs while rendering, any HTML already rendered is returned.
func Render(ops []byte) ([]byte, error) {
	return RenderExtended(ops, nil)
}

// RenderExtended takes a Delta array of insert operations and, optionally, a function that may provide a Formatter to
// customize the way certain kinds of inserts are rendered. If the given Formatter is nil, then the default one that is
// built in is used. If an error occurs while rendering, any HTML already rendered is returned.
func RenderExtended(ops []byte, customFormats func(string, *Op) Formatter) (html []byte, err error) {

	var raw []rawOp
	if err = json.Unmarshal(ops, &raw); err != nil {
		return nil, err
	}

	var (
		finalBuf = new(bytes.Buffer)       // the final output
		tempBuf  = new(bytes.Buffer)       // temporary buffer reused for each block element
		fs       = new(formatState)        // the tags currently open in the order in which they were opened
		o        = new(Op)                 // allocate memory for an Op to reuse for all iterations
		fms      = make([]Formatter, 0, 4) // the Formatter types defined for each Op
	)
	o.Attrs = make(map[string]string, 3) // initialize once here only

	for i := range raw {

		if err = raw[i].makeOp(o); err != nil {
			return finalBuf.Bytes(), err
		}

		fms = fms[:0] // Reset the slice for the current Op iteration.

		// To set up fms, first check the Op insert type.
		fmTer := o.getFormatter(o.Type, customFormats)
		if fmTer == nil {
			return finalBuf.Bytes(), fmt.Errorf("an op does not have a format defined for its type: %v", raw[i])
		} else if !fs.hasSet(fmTer.Fmt()) {
			fms = append(fms, fmTer)
		}

		// Get a Formatter out of each of the attributes.
		for attr := range o.Attrs {
			fmTer = o.getFormatter(attr, customFormats)
			if fmTer != nil && !fs.hasSet(fmTer.Fmt()) {
				fms = append(fms, fmTer)
			}
		}

		// Check if any of the formats is a FormatWriter. If any is, just write it out.
		for i := range fms {
			fm := fms[i].Fmt()
			if fm == nil {
				if wr, ok := fms[i].(FormatWriter); ok {
					wr.Write(tempBuf)
					o.Data = ""
				}
				// Delete this Formatter from fms (it does not do anything else).
				fms = append(fms[0:i], fms[i+1:]...)
			}
		}

		// Open the a block element, write its body, and close it to move on only when the ending "\n" of the block is reached.
		if strings.IndexByte(o.Data, '\n') != -1 {

			if o.Data == "\n" { // Write a block element and flush the temporary buffer.

				// Avoid empty paragraphs and "\n" in the output.
				if tempBuf.Len() == 0 {
					o.Data = "<br>"
				} else {
					o.Data = ""
				}

				o.writeBlock(fs, tempBuf, finalBuf, fms)

			} else { // Extract the block-terminating line feeds and write each part as its own Op.

				split := strings.Split(o.Data, "\n")

				for i := range split {

					o.Data = split[i]

					// If the current o.Data still has an "\n" following (its not the last in split), then it ends a block.
					if i < len(split)-1 {

						// Avoid having empty paragraphs.
						if tempBuf.Len() == 0 && o.Data == "" {
							o.Data = "<br>"
						}

						o.writeBlock(fs, tempBuf, finalBuf, fms)

					} else if o.Data != "" { // If the last element in split is just "" then the last character in the rawOp was a "\n".
						o.writeInline(fs, tempBuf, fms)
					}

				}

			}

		} else { // We are just adding stuff inline.
			o.writeInline(fs, tempBuf, fms)
		}

	}

	html = finalBuf.Bytes()
	return

}

// An Op is a Delta insert operations (https://github.com/quilljs/delta#insert) that has been converted into this format for
// usability with the type safety in Go.
type Op struct {
	Data  string            // the text to insert or the value of the embed object (http://quilljs.com/docs/delta/#embeds)
	Type  string            // the type of the op (typically "text", but any other type can be registered)
	Attrs map[string]string // key is attribute name; value is either the attribute value or "y" (meaning true)
}

// writeBlock writes a block element (which may be nested inside another block element if it is a FormatWrapper).
// The opening HTML tag of a block element is written to the main buffer only after the "\n" character terminating the
// block is reached (the Op with the "\n" character holds the information about the block element).
func (o *Op) writeBlock(fs *formatState, tempBuf *bytes.Buffer, finalBuf *bytes.Buffer, newFms []Formatter) {

	// Close the inline formats opened within the block.
	fs.closePrevious(tempBuf, o)

	var blockWrap struct {
		tagName string
		classes []string
		style   string
	}

	// At least a format from the Op.Type should be set.
	if len(newFms) == 0 {
		return
	}

	// Merge all formats into a single tag.
	for i := range newFms {
		fm := newFms[i].Fmt()
		if fm.Block {
			val := fm.Val
			switch fm.Place {
			case Tag:
				// If an opening tag is not specified by the Op insert type, it may be specified by an attribute.
				if fm.Block && val != "" {
					blockWrap.tagName = val // Override whatever value may be set.
				}
			case Class:
				blockWrap.classes = append(blockWrap.classes, val)
			case Style:
				blockWrap.style += val
			}
		}
		// Simply write out all of FormatWrapper opening text (if there is any).
		if fw, ok := newFms[i].(FormatWrapper); ok {
			finalBuf.WriteString(fw.PreWrap(fs.open))
		}
	}

	if blockWrap.tagName != "" {
		finalBuf.WriteByte('<')
		finalBuf.WriteString(blockWrap.tagName)
		finalBuf.WriteString(classesList(blockWrap.classes))
		if blockWrap.style != "" {
			finalBuf.WriteString(" style=")
			finalBuf.WriteString(strconv.Quote(blockWrap.style))
		}
		finalBuf.WriteByte('>')
	}

	finalBuf.Write(tempBuf.Bytes()) // Copy the temporary buffer to the final output.

	finalBuf.WriteString(o.Data) // Copy the data of the current Op (usually just "<br>" or blank).

	if blockWrap.tagName != "" {
		closeTag(finalBuf, blockWrap.tagName)
	}

	// Write out the closes by FormatWrapper formats, starting from the last written.
	for i := len(newFms) - 1; i >= 0; i-- {
		if fw, ok := newFms[i].(FormatWrapper); ok {
			finalBuf.WriteString(fw.PostWrap(fs.open, o))
		}
	}

	tempBuf.Reset()

}

func (o *Op) writeInline(fs *formatState, buf *bytes.Buffer, newFms []Formatter) {

	fs.closePrevious(buf, o)

	// Save the formats being written now separately from fs.
	addNow := &formatState{make([]*Format, 0, len(newFms))}

	for i := range newFms {
		f := newFms[i].Fmt()
		// Filter out Block-level formats.
		if !f.Block {
			f.fm = newFms[i]
			fs.add(f)
			addNow.open = append(addNow.open, f)
		}
	}

	addNow.writeFormats(buf)
	copy(fs.open, addNow.open) // Copy after the sorting.

	buf.WriteString(o.Data)

}

// HasAttr says if the Op is not nil and has the attribute set to a non-blank value.
func (o *Op) HasAttr(attr string) bool {
	return o != nil && o.Attrs[attr] != ""
}

// getFormatter returns a formatter based on the keyword (either "text" or "" or an attribute name) and the Op settings.
// For every Op, first its Type is passed through here as the keyword, and then its attributes.
func (o *Op) getFormatter(keyword string, customFormats func(string, *Op) Formatter) Formatter {

	if customFormats != nil {
		if custom := customFormats(keyword, o); custom != nil {
			return custom
		}
	}

	switch keyword { // This is the list of currently recognized "keywords".
	case "text":
		return new(textFormat)
	case "header":
		return &headerFormat{
			level: o.Attrs["header"],
		}
	case "list":
		lf := &listFormat{
			indent: indentDepths[o.Attrs["indent"]],
		}
		if o.Attrs["list"] == "bullet" {
			lf.lType = "ul"
		} else {
			lf.lType = "ol"
		}
		return lf
	case "blockquote":
		return new(blockQuoteFormat)
	case "align":
		return &alignFormat{
			val: o.Attrs["align"],
		}
	case "image":
		return &imageFormat{
			src: o.Data,
		}
	case "link":
		return &linkFormat{
			href: o.Attrs["link"],
		}
	case "bold":
		return new(boldFormat)
	case "italic":
		return new(italicFormat)
	case "underline":
		return new(underlineFormat)
	case "color":
		return &colorFormat{
			c: o.Attrs["color"],
		}
	}

	return nil

}

// Each handler should check the previous Op to see if it has attributes that are not set on the current Op and close the
// appropriate HTML tags before writing the current Op; also the handler should not needlessly open up a  tag for an
// attribute if it was already opened for the previous Op. This ensures that the rendered HTML is lean.

// A FormatPlace is either an HTML tag name, a CSS class, or a style attribute value.
type FormatPlace uint8

const (
	Tag FormatPlace = iota
	Class
	Style
)

type Formatter interface {
	Fmt() *Format       // Format gives the string to write and where to place it.
	HasFormat(*Op) bool // Say if the Op has the Format that Fmt returns.
}

// A Formatter may also be a FormatWriter if it wishes to write the body of the Op in some custom way (useful for embeds).
type FormatWriter interface {
	Formatter
	Write(io.Writer) // Write the entire body of the element.
}

// A FormatWrapper wraps text in additional HTML tags (such as "ul" for lists).
type FormatWrapper interface {
	Formatter
	PreWrap([]*Format) string       // Given the currently open formats, say what to write to open the wrap (complete HTML tag).
	PostWrap([]*Format, *Op) string // Given the currently open formats and the current Op, say what to write to close the wrap.
}

type Format struct {
	Val   string      // the value to print
	Place FormatPlace // where this format is placed in the text
	Block bool        // indicate whether this is a block-level format (not printed until a "\n" is reached)
	fm    Formatter   // where this instance of a Format came from
}

// If cl has something, then classesList returns the class attribute to add to an HTML element with a space before the
// "class" attribute and spaces between each class name.
func classesList(cl []string) (classAttr string) {
	if len(cl) > 0 {
		classAttr = " class=" + strconv.Quote(strings.Join(cl, " "))
	}
	return
}

// closeTag writes a complete closing tag to buf.
func closeTag(buf *bytes.Buffer, tagName string) {
	buf.WriteString("</")
	buf.WriteString(tagName)
	buf.WriteByte('>')
}
